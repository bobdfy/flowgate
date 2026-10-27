package ai

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/bobdfy/flowgate/internal/identity"
	"github.com/bobdfy/flowgate/internal/observability"
	"github.com/bobdfy/flowgate/internal/overload"
)

// 名字 + 数量的组合构成两层名额：
//
//	模型层  —— 这个模型整体最多几个在途请求（ProviderEntry.MaxConcurrency）
//	租户层  —— 同一个模型下，单个租户最多占多少（总名额的 75%）
//
// 为什么需要租户层：只有模型层的话，一个租户开几十条长流就能把这个模型的
// 名额全占住，其他租户全部 429 —— 隔离做了等于没做，只是把"全站互抢"
// 缩小成了"单个模型内互抢"。
//
// 为什么名额必须覆盖整条 SSE：QPS 只限制新请求的速率，不限制仍在跑的请求数。
// 每秒 20 条、每条跑 60 秒 = 稳态 1200 个在途，QPS 限流完全不会触发。
// 所以名额的释放点在 serveStream 读完（或客户端断开）之后，不是拿到响应头之后。

// 单个租户最多占用模型总名额的 90%，永远给其他租户留 10% 的余量。
//
// 为什么不平均分（1/N）：部署初期往往只有一个租户，平均分会把可用容量
// 直接砍掉一大半，纯粹是无谓的变慢。90% 的语义是「你能用绝大部分，
// 但永远吃不干」—— 隔离的目的达到了，单租户的损失也几乎为零。
const tenantSharePercent = 90

// modelLimiter 一个模型的并发名额，含租户公平。
type modelLimiter struct {
	model string
	total *overload.Guard
	fair  int // 单租户名额

	mu sync.Mutex
	// tenants 按租户懒创建。租户数由 tenants 表决定，是有界集合；
	// 租户被删后这里会残留一个小对象，可以忽略。
	tenants map[int64]*overload.Guard
}

// newModelLimiter 构造一个模型的限额。maxConcurrency <= 0 表示不限，调用方不会构造。
func newModelLimiter(model string, maxConcurrency int) *modelLimiter {
	// 至少给 1：maxConcurrency 很小时（1~3）按比例算出来会是 0，
	// 那样这个模型对任何租户都会直接拒绝，等于把模型关了。
	fair := max(maxConcurrency*tenantSharePercent/100, 1)
	return &modelLimiter{
		model:   model,
		total:   overload.NewGuard(maxConcurrency, overload.StrategyFailFast, 0, 0),
		fair:    fair,
		tenants: make(map[int64]*overload.Guard),
	}
}

func (l *modelLimiter) tenantGuard(tenantID int64) *overload.Guard {
	l.mu.Lock()
	defer l.mu.Unlock()

	g, ok := l.tenants[tenantID]
	if !ok {
		g = overload.NewGuard(l.fair, overload.StrategyFailFast, 0, 0)
		l.tenants[tenantID] = g
	}
	return g
}

// acquire 依次占用租户名额和模型名额，返回幂等的 release 与被拒的层级。
//
// 顺序是先租户后模型：反过来的话，模型名额已经占了才发现租户超限，
// 这一刻模型名额被无谓地占住又立刻归还，高并发下会造成名额抖动。
func (l *modelLimiter) acquire(ctx context.Context, tenantID int64) (release func(), scope string, err error) {
	releaseTenant, err := l.tenantGuard(tenantID).Acquire(ctx)
	if err != nil {
		return nil, "tenant", err
	}

	releaseModel, err := l.total.Acquire(ctx)
	if err != nil {
		// 模型名额没拿到，租户名额必须还回去；
		// 漏了这一句，这个租户的名额会越用越少，最终自己被自己限死。
		releaseTenant()
		return nil, "model", err
	}

	return func() {
		releaseModel()
		releaseTenant()
	}, "", nil
}

// acquireModelSlot 为一次 AI 请求占用模型名额。返回的 release 必须 defer 调用，
// 位置要在 ServeHTTP 里 —— 那里直到整条流结束才会返回。
//
// 被拒绝时自己写好 429 响应，返回 ok=false。
func (h *Handler) acquireModelSlot(w http.ResponseWriter, r *http.Request, model string) (func(), bool) {
	lim := h.limits[model]
	if lim == nil {
		return func() {}, true
	}

	var tenantID int64
	if id, ok := identity.FromContext(r.Context()); ok {
		tenantID = id.TenantID
	}

	release, scope, err := lim.acquire(r.Context(), tenantID)
	if err != nil {
		observability.AIModelConcurrencyRejected.WithLabelValues(model, scope).Inc()
		slog.Warn("ai_concurrency_limited",
			"model", model, "scope", scope, "tenant_id", tenantID, "err", err)

		// 名额是瞬时的，重试几乎立刻就能成功，所以给 1 秒而不是长等待。
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, ErrTypeLimit,
			fmt.Sprintf("模型 %s 的在途请求已达上限（%s 级），请稍后重试", model, scope))
		return nil, false
	}

	observability.AIModelInflight.WithLabelValues(model).Inc()
	return func() {
		release()
		observability.AIModelInflight.WithLabelValues(model).Dec()
	}, true
}
