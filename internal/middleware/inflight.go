package middleware

import (
	"net/http"

	"github.com/bobdfy/flowgate/internal/observability"
	"github.com/bobdfy/flowgate/internal/overload"
)

// Inflight 限制整个进程的在途请求数。放鉴权前面
func Inflight(guard *overload.Guard, scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			release, err := guard.Acquire(r.Context())
			if err != nil {
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
