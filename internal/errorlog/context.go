package errorlog

import "context"

// reasonKey 是 ctx 的键，值为 *reason。
type reasonKey struct{}

// reason 保存内层写入的错误归因。
//
// 为什么要「ctx 里的可变持有者」：Logging 在最外层，只有内层知道这次失败
// 的真实原因（熔断、流式中途断开、上游超时），而 context 的值只能向下传。
// 所以需要一个内层能写、外层能读的容器 —— 和 middleware 的 routeLabel
// 是同一个模式。
//
// 只在单个请求 goroutine 里读写（流式归因也在同一个 goroutine），无需加锁。
type reason struct {
	kind   Kind
	detail string
	set    bool
}

// WithHolder 把归因持有者放进 ctx（middleware.Logging 调用）。
func WithHolder(ctx context.Context) context.Context {
	return context.WithValue(ctx, reasonKey{}, &reason{})
}

// Set 由内层记录失败归因；没有持有者（未启用错误看板）时是空操作。
//
// 调用 Set 意味着「这条请求一定要进错误看板」，即使状态码是 200
// —— 流式响应在已经写出 200 之后才断开，正是这种情况。
func Set(ctx context.Context, kind Kind, detail string) {
	if h, ok := ctx.Value(reasonKey{}).(*reason); ok {
		h.kind = kind
		h.detail = Truncate(detail)
		h.set = true
	}
}

// Get 返回内层写入的归因；ok=false 表示内层没写过。
func Get(ctx context.Context) (Kind, string, bool) {
	if h, ok := ctx.Value(reasonKey{}).(*reason); ok {
		return h.kind, h.detail, h.set
	}
	return "", "", false
}
