package middleware

import (
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/bobdfy/flowgate/internal/observability"
	"github.com/bobdfy/flowgate/internal/ratelimit"
)

// KeyFunc 从请求里限流key
// 任何接收一个 *http.Request 参数、返回一个 string 的函数，都可以当作 KeyFunc 使用。
type KeyFunc func(*http.Request) (string, int64)

// ClientKey 默认 key 提取：优先 X-API-Key，其次客户端 IP（带来源前缀避免命名空间撞车）。
func ClientKey(r *http.Request) (string, int64) {
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		return "key:" + apiKey, 100
	}

	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = host
	}
	return "ip:" + ip, 100

}

// RateLimit 限流中间件：key 超限返回 429（带 Retry-After）并且不调 next；
// 限流器自身出错（分布式实现才可能）先记日志放行，阶段 4 再接 fail_mode。
// 位置：包在 Router 外层，但在 RequestID / Logging 内层。
func RateLimit(limiter ratelimit.Limiter, keyFn KeyFunc, next http.Handler) http.Handler {
	if keyFn == nil {
		keyFn = ClientKey
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, limit := keyFn(r)

		start := time.Now()
		allowed, err := limiter.Allow(r.Context(), key, limit)

		observability.RateLimitDuration.Observe(time.Since(start).Seconds()) // ← 决策耗时

		if err != nil {
			slog.Error("rate_limit_error", // 记录 error 级别日志
				"err", err, // 记录错误内容
				"key", key, // 记录限流 key
				"method", r.Method, // 记录请求方法
				"path", r.URL.Path, // 记录请求路径
			)
			next.ServeHTTP(w, r)
			return
		}
		if !allowed {
			//limiter.(ratelimit.RetryAfterProvider) 是类型断言，判断 limiter 是否实现了 RetryAfterProvider 接口。
			if p, ok := limiter.(ratelimit.RetryAfterProvider); ok {
				if d := p.RetryAfter(key, limit); d > 0 {
					secs := int(math.Ceil(d.Seconds()))
					w.Header().Set("Retry-After", strconv.Itoa(secs))
				}
			}

			slog.Warn("rate_limited",
				"key", key, // 记录限流 key
				"method", r.Method, // 记录请求方法
				"path", r.URL.Path, // 记录请求路径
				// 从 context 取 request id（外层中间件注入）
				"request_id", r.Header.Get("X-Request-ID"),
			)
			// 返回 429 状态码和消息体
			observability.RateLimitedTotal.WithLabelValues(key).Inc()
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
