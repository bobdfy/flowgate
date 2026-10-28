package middleware

import (
	"net/http"

	"github.com/bobdfy/flowgate/internal/errorlog"
	"github.com/bobdfy/flowgate/internal/observability"
	"github.com/bobdfy/flowgate/internal/overload"
)

// Inflight 限制整个进程的在途请求数。放鉴权前面
func Inflight(guard *overload.Guard, scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			release, err := guard.Acquire(r.Context())
			if err != nil {
				// 拒绝发生在鉴权和路由之前，没人会替它写标签：不写的话外层会
				// 兜底成 unmatched，让「过载拒绝」和「没有路由匹配」混成
				// 同一条时间序列。鉴权、限流也都是自己写标签的。
				SetRouteLabel(r, LabelOverload)
				// 归因同理：错误串里已经带上 reason 与拒绝瞬间的在途数
				// （如 "overload: rejects (reason = fail_fast, in_flight = 1000)"），
				// 直接进错误看板的 detail，不用再拼一遍。
				errorlog.Set(r.Context(), errorlog.KindOverload, err.Error())
				WriteOverload(w, err, scope, func(reason string) {
					observability.OverloadRejectedTotal.WithLabelValues(scope, reason).Inc()
				})
				return
			}
			observability.InstanceInflight.Inc()
			defer observability.InstanceInflight.Dec()

			defer release()
			next.ServeHTTP(w, r)
		})
	}
}
