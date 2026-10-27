package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/bobdfy/flowgate/internal/loadbalance"
	"github.com/bobdfy/flowgate/internal/observability"
)

// Provider 级熔断。
//
// 与普通链路复用同一套状态语义（loadbalance.CircuitState / Outcome），
// 但不再复用 NodePool：AI 的关键维度是 provider，没有地址列表和权重。
//
// 只把「上游不可用」计入失败，业务错误一律中性：
//
//	连接失败 / 超时 / 5xx / 429 / 408   → 失败（上游扛不住了）
//	4xx（参数错、密钥错、模型不存在）      → 中性（是我们的错，不是上游挂了）
//	客户端主动断开                      → 中性（P0-12：用户走了不代表上游不可用）
//
// 为什么必须区分：如果 401 也算失败，一个配错的 key 会在 5 个请求后
// 把整个 provider 熔断 10 秒，管理面看到的却是「DeepSeek 挂了」。
const (
	breakerFailureThreshold = 5
	breakerCooldown         = 10 * time.Second
	breakerHalfOpenLimit    = 1
)

// breaker 一个 provider 的熔断器。并发安全。
type breaker struct {
	provider string

	mu               sync.Mutex
	state            loadbalance.CircuitState
	fails            int
	openUntil        time.Time
	halfOpenInFlight int // HalfOpen 态正在飞行的试探请求数
}

func newBreaker(provider string) *breaker {
	return &breaker{provider: provider, state: loadbalance.StateClosed}
}

// breakerLease 一次请求占用的熔断名额。
//
// 只有 HalfOpen 态的试探请求才真正占名额（probe=true）；
// Closed 态不计数，只是一个「把结果送回熔断器」的凭证。
type breakerLease struct {
	b     *breaker
	probe bool
	once  sync.Once
}

// Done 结算本次请求的结果。重复调用只生效一次 ——
// 这样「显式结算」和 defer 兜底可以同时存在。
func (l *breakerLease) Done(o loadbalance.Outcome) {
	if l == nil {
		return
	}
	l.once.Do(func() { l.b.record(o, l.probe) })
}

// acquire 判断能否放行。返回 false 表示熔断中，调用方应快速拒绝。
func (b *breaker) acquire() (*breakerLease, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case loadbalance.StateOpen:
		if time.Now().Before(b.openUntil) {
			return nil, false
		}
		// 冷却到期：转半开，放一条试探。
		b.state = loadbalance.StateHalfOpen
		b.fails = 0
		b.halfOpenInFlight = 0
		b.setMetric(loadbalance.StateHalfOpen)
		slog.Info("ai_circuit_half_open", "provider", b.provider, "cooldown", breakerCooldown)
		fallthrough

	case loadbalance.StateHalfOpen:
		// 整条链路同时只放 breakerHalfOpenLimit 条试探，
		// 否则「试探」会变成「全量打回来」，恢复瞬间再次压垮上游。
		if b.halfOpenInFlight >= breakerHalfOpenLimit {
			return nil, false
		}
		b.halfOpenInFlight++
		return &breakerLease{b: b, probe: true}, true

	default:
		return &breakerLease{b: b}, true
	}
}

// record 更新计数并决定是否翻转状态。probe 表示这条请求是不是半开试探。
func (b *breaker) record(o loadbalance.Outcome, probe bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	from := b.state
	switch b.state {
	case loadbalance.StateClosed:
		if o == loadbalance.OutcomeNeutral {
			return
		}
		if o == loadbalance.OutcomeSuccess {
			b.fails = 0
			return
		}
		b.fails++
		if b.fails >= breakerFailureThreshold {
			b.tripLocked(from, "failure_threshold")
		}

	case loadbalance.StateHalfOpen:
		// 熔断前就发出去的旧请求，此时才回来：结果不能作为恢复依据。
		// 否则一条迟到的成功会把刚开始的试探直接判成「已恢复」。
		if !probe {
			return
		}
		if b.halfOpenInFlight > 0 {
			b.halfOpenInFlight--
		}
		if o == loadbalance.OutcomeNeutral {
			return
		}
		if o == loadbalance.OutcomeSuccess {
			b.state = loadbalance.StateClosed
			b.fails = 0
			b.halfOpenInFlight = 0
			b.setMetric(loadbalance.StateClosed)
			slog.Info("ai_circuit_closed", "provider", b.provider, "from", from, "reason", "probe_success")
			return
		}
		b.tripLocked(from, "probe_failed")

	case loadbalance.StateOpen:
		// 已经熔断，期间回来的结果没有意义。
	}
}

// tripLocked 打开熔断。调用方必须持锁。
func (b *breaker) tripLocked(from loadbalance.CircuitState, reason string) {
	b.state = loadbalance.StateOpen
	b.openUntil = time.Now().Add(breakerCooldown)
	b.fails = 0
	b.halfOpenInFlight = 0
	b.setMetric(loadbalance.StateOpen)
	slog.Warn("ai_circuit_open",
		"provider", b.provider,
		"from", from,
		"reason", reason,
		"open_until", b.openUntil.Format(time.RFC3339),
		"cooldown", breakerCooldown)
}

func (b *breaker) setMetric(s loadbalance.CircuitState) {
	var v float64
	switch s {
	case loadbalance.StateOpen:
		v = 1
	case loadbalance.StateHalfOpen:
		v = 2
	}
	observability.AICircuitState.WithLabelValues(b.provider).Set(v)
}

// classifyForwardError 把发送阶段的错误归入熔断结果。
func classifyForwardError(err error) loadbalance.Outcome {
	switch {
	case err == nil:
		return loadbalance.OutcomeSuccess
	case errors.Is(err, context.Canceled):
		// 客户端走了，上游是死是活我们并不知道。
		return loadbalance.OutcomeNeutral
	default:
		// 其余一律是连接/超时/协议层故障，算上游不可用。
		return loadbalance.OutcomeFailure
	}
}

// classifyStatus 按上游 HTTP 状态码判定可用性。
func classifyStatus(code int) loadbalance.Outcome {
	switch {
	case code >= 200 && code < 300:
		return loadbalance.OutcomeSuccess
	case code == http.StatusTooManyRequests, code == http.StatusRequestTimeout:
		return loadbalance.OutcomeFailure
	case code >= 500:
		return loadbalance.OutcomeFailure
	default:
		// 4xx：请求本身的问题，换一个时间点重试结果一样。
		return loadbalance.OutcomeNeutral
	}
}

// breakerFor 返回该模型所属 provider 的熔断器；没有对应 breaker 时返回 nil（不熔断）。
func (h *Handler) breakerFor(entry *ProviderEntry) *breaker {
	if entry == nil {
		return nil
	}
	name := entry.Name
	if name == "" {
		return nil
	}
	return h.breakers[name]
}

// acquireBreakerSlot 在发往上游之前申请放行。被熔断时自己写好 503，返回 ok=false。
//
// 放在模型并发名额之前：上游已经确定挂了，没必要再占一个名额再退还。
func (h *Handler) acquireBreakerSlot(w http.ResponseWriter, entry *ProviderEntry) (*breakerLease, bool) {
	b := h.breakerFor(entry)
	if b == nil {
		return nil, true
	}

	lease, ok := b.acquire()
	if !ok {
		observability.AIBreakerRejected.WithLabelValues(b.provider).Inc()
		slog.Warn("ai_breaker_rejected", "provider", b.provider)
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, ErrTypeUpstream,
			fmt.Sprintf("上游 %s 暂时不可用（熔断中），请稍后重试", b.provider))
		return nil, false
	}
	return lease, true
}
