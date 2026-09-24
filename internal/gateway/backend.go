package gateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bobdfy/flowgate/internal/loadbalance"
	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/observability"
	"github.com/bobdfy/flowgate/internal/overload"
)

// Backend 是一个上游服务的转发入口。
// 它内部持有一个负载均衡器，负责从该服务的多个实例里挑一个健康实例并转发。
type Backend struct {
	// 这个后端是哪个服务(含超时配置)
	svc model.Service

	// 负载均衡器：负责「这次轮到哪个实例」
	lb loadbalance.LoadBalancer

	// 挑中实例后，去哪拿转发用的 proxy (转发器缓存)
	proxyCache *ProxyCache

	pool *loadbalance.NodePool

	guard *overload.Guard
}

const (
	// backoffMax 退避封顶，重试间隔最长不超过这个值。
	backoffMax = time.Second
)

// ServeHTTP 实现 http.Handler：挑一个健康实例转发，失败按配置重试（换节点 + 退避），最后把「最终那次」的响应（或错误）一次性 flush 给客户端。
func (b *Backend) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	start := time.Now()

	// 整体超时：整个「重试过程」共享同一个 deadline(RoundTrip 和退避都受它约束)
	if timeout := time.Duration(b.svc.RequestTimeoutMs) * time.Millisecond; timeout > 0 {
		ctx, cancel := context.WithTimeout(req.Context(), timeout)
		defer cancel()
		req = req.WithContext(ctx)
	}

	var (
		resp *http.Response
		err  error
		node *loadbalance.NodeState
	)

	// 服务级并发护栏（过载保护），位置在超时(之后)。
	//
	// 为什么不能放在超时之前：那样"排队等待"的时间不计入 request_timeout。
	// 举例：request_timeout = 10s、排队等了 20s —— 位置在超时之前的话，
	// 出队后才开始算那 10s，客户端实际等了 30s，跟承诺的 10s 对不上。
	// 放在之后，排队 + 重试 + 转发 全程共享同一个 deadline，
	// 这也正是 wait 策略能成立的前提。
	release, err := b.guard.Acquire(req.Context())
	if err != nil {
		if rejectOverload(w, err, "service", b.svc.Name, func(reason string) {
			observability.OverloadRejectedTotal.WithLabelValues(b.svc.Name, reason).Inc()
		}) {
			return
		}
	}

	defer release()

	maxRetries := b.svc.MaxRetries
	retryOn := parseRetryStatus(b.svc.RetryOnStatus)
	retryable := isIdempotent(req.Method)

	// 缓存请求体：RoundTrip 会把 req.Body 读空，重试第二次 clone 出来就是空的，
	// 所以先整读进内存，每轮重试前重新包 reader 发出去。
	var reqBody []byte
	if req.Body != nil {
		reqBody, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
	}

	attempt := 0
	for ; ; attempt++ {
		node = b.lb.Pick()
		if node == nil {
			break
		}

		u, perr := parseUpstream(node.Address)
		if perr != nil {
			err = perr
			break
		}

		req.Body = io.NopCloser(bytes.NewReader(reqBody))
		req.ContentLength = int64(len(reqBody))

		transport := b.proxyCache.get(u, b.svc)
		resp, err = forwardOnce(transport, req, u)

		shouldRetry := attempt < maxRetries && retryable
		if err == nil && !retryOn[resp.StatusCode] {
			shouldRetry = false // 有响应但状态码不在重试名单 → 不重试
		}
		if !shouldRetry {
			break
		}

		// 丢弃这次响应：排干 body + 关闭，连接才能还给连接池复用
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			resp = nil
		}

		// 记一次失败给被动健康 + 熔断器
		b.pool.RecordResult(node, false)

		slept, delay := sleepBackoff(req.Context(), b.svc.RetryBackoffMs, attempt)
		slog.Warn("retry",
			"service", b.svc.Name,
			"attempt", attempt+1,
			"max_retries", maxRetries,
			"addr", node.Address,
			"err", err,
			"status", statusCode,
			"backoff_ms", delay.Milliseconds(),
		)
		if !slept {
			err = req.Context().Err()
			break
		}
	}

	if node == nil {
		http.Error(w, "no healthy upstream", http.StatusServiceUnavailable)
		slog.Error("no_available_upstream",
			"service", b.svc.Name,
			"attempts", attempt,
			"duration_ms", time.Since(start).Milliseconds())
		return
	}

	if err != nil {
		b.pool.RecordResult(node, false)
		if errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
		} else {
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
		}
		slog.Warn("request_failed",
			"service", b.svc.Name,
			"addr", node.Address,
			"err", err.Error(),
			"attempts", attempt+1,
			"duration_ms", time.Since(start).Milliseconds())
		return
	}

	// 成功路径：最终状态码记被动健康（成功只记成功，不误记失败）
	b.pool.RecordResult(node, resp.StatusCode < 500)
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
	resp.Body.Close()

	slog.Info("request_finished",
		"service", b.svc.Name,
		"addr", node.Address,
		"status", resp.StatusCode,
		"attempts", attempt+1,
		"duration_ms", time.Since(start).Milliseconds())
}

// parseRetryStatus 把 "502,503" 这类逗号分隔的状态码串解析成状态码集合，用于判断哪些响应码值得重试。
func parseRetryStatus(s string) map[int]bool {
	if s == "" {
		return make(map[int]bool)
	}

	parts := strings.Split(s, ",")
	statusSet := make(map[int]bool)

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		code, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		statusSet[code] = true
	}
	return statusSet
}

// isSafeMethod 判断 HTTP 方法是否幂等，只有安全方法才允许在失败后自动重试。
func isIdempotent(method string) bool {
	if method == "GET" || method == "HEAD" || method == "PUT" || method == "DELETE" {
		return true
	} else {
		return false
	}
}

// sleepBackoff 指数退避 + 抖动：第 attempt 次重试的基准延迟 = baseMs × 2^attempt，封顶 backoffMax，
// 再在 [delay/4, delay) 内随机，避免多个客户端同时重试把上游打爆。
// 返回 (是否睡满一轮, 实际延迟)：false 表示期间 ctx 已取消，调用方应停止重试。
// 为什么是「保底 25%」而不是「全抖动 [0, delay)」（这段是新增的）
//
//	—— 全抖动的打散效果最好，但可能抽出接近 0 的间隔。
//	   多个客户端同时重试时，等于没有退避：重试会在同一瞬间叠加，
//	   上游刚恢复就被第二波打挂。
//	   保底 25% 牺牲一部分打散效果，换取「任何一次重试都至少等了 delay 的 1/4」。
//	   在「重试风暴」这个具体场景里，这个保底比极致的随机更重要。
func sleepBackoff(ctx context.Context, baseMs, attempt int) (bool, time.Duration) {
	delay := time.Duration(baseMs) * time.Millisecond

	for range attempt {
		delay *= 2
		if delay >= backoffMax {
			delay = backoffMax
			break
		}
	}

	if delay > 0 {
		minDelay := delay / 4          // 保底值 = delay 的 25%
		delayRange := delay - minDelay // 剩余范围 = delay 的 75%
		if delayRange > 0 {
			delay = minDelay + time.Duration(rand.Int63n(int64(delayRange)))
		} else {
			delay = minDelay
		}
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false, delay
	case <-timer.C:
		return true, delay
	}
}

// ReportMetrics 把当前并发水位刷进Prometheus
func (b *Backend) ReportMetrics() {
	st := b.guard.Stats()
	observability.InflightRequests.WithLabelValues(b.svc.Name).Set(float64(st.InFlight))
	observability.QueueDepth.WithLabelValues(b.svc.Name).Set(float64(st.Queued))
}

// rejectOverload 把 overload.RejectedError 翻译为HTTP响应
func rejectOverload(w http.ResponseWriter, err error, role, name string, inc func(reason string)) bool {
	var rej *overload.RejectedError
	if !errors.As(err, &rej) {
		slog.Debug("guard_ctx_done", "role", role, "name", name, "err", err)
		return true
	}

	if rej.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(rej.RetryAfter.Seconds()))))
	}

	if inc != nil {
		inc(string(rej.Reason))
	}
	slog.Warn("overloaded",
		"role", role,
		"name", name,
		"reason", rej.Reason,
		"inflight", rej.InFlight)

	http.Error(w, "overload:"+string(rej.Reason), http.StatusServiceUnavailable)
	return true
}
