package middleware

import (
	"context"
	"net/http"
)

// 指标标签的固定取值。除「路由模板」外，任何可能由客户端输入决定的字符串
// 都不能进指标标签 —— 一次随机路径请求就能造出一个新时间序列。
const (
	LabelUnmatched    = "unmatched"     // 没有任何路由匹配
	LabelAuthRejected = "auth_rejected" // 鉴权失败，请求根本没进路由
	LabelRateLimited  = "rate_limited"  // 限流拒绝，同样没进路由
	LabelAI           = "ai"            // AI 路径，后面拼 API 名
)

// routeLabelKey 是 ctx 的键，值为 *routeLabel。
type routeLabelKey struct{}

// routeLabel 保存一次请求最终命中的有界标签。
// 只在单个请求 goroutine 里读写，不需要锁。
type routeLabel struct{ value string }

// RouteLabel 返回本次请求的路由标签；没被任何层写过时返回空串。
func RouteLabel(ctx context.Context) string {
	if h, ok := ctx.Value(routeLabelKey{}).(*routeLabel); ok {
		return h.value
	}
	return ""
}

// SetRouteLabel 由内层（路由匹配 / 鉴权 / 限流 / AI 分流）写入标签，供 Logging 打点。
//
// 为什么是「写 ctx 里的可变持有者」而不是直接 Values：
// Logging 在外层，只有内层知道这次请求命中了哪条路由，
// context 的值只能向下传，所以需要一个内层能写、外层能读的容器。
func SetRouteLabel(r *http.Request, label string) {
	if h, ok := r.Context().Value(routeLabelKey{}).(*routeLabel); ok {
		h.value = label
	}
}

// withRouteLabel 把标签持有者放进 ctx（Logging 调用）。
func withRouteLabel(ctx context.Context) context.Context {
	return context.WithValue(ctx, routeLabelKey{}, &routeLabel{})
}
