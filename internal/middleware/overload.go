package middleware

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/bobdfy/flowgate/internal/overload"
)

// WriteOverload 把 overload.Guard 返回的错误翻译成 HTTP 响应。
// 返回 true 表示已经写过响应了，调用方直接 return 即可。
func WriteOverload(w http.ResponseWriter, err error, scope string, inc func(reason string)) bool {
	var rej *overload.RejectedError
	if !errors.As(err, &rej) {
		if errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
			return true
		}
		return true // 客户端已断开，无需回写
	}

	if rej.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(rej.RetryAfter.Seconds()))))
	}
	if inc != nil {
		inc(string(rej.Reason))
	}
	slog.Warn("overloaded", "scope", scope, "reason", rej.Reason, "inflight", rej.InFlight)

	http.Error(w, "overload:"+string(rej.Reason), http.StatusServiceUnavailable)
	return true
}
