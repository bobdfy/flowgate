// Package middleware 提供网关通用的 HTTP 中间件：Request ID 与访问日志。
package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/bobdfy/flowgate/internal/errorlog"
	"github.com/bobdfy/flowgate/internal/observability"
)

// ErrorResponder 决定「网关自己拒绝请求」时回什么 body。
type ErrorResponder func(w http.ResponseWriter, r *http.Request, status int, msg string)

// RequestID 为每个请求生成或透传 Request ID。
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-ID")
		if rid == "" {
			rid = newID()
			r.Header.Set("X-Request-ID", rid)
		}
		w.Header().Set("X-Request-ID", rid)
		next.ServeHTTP(w, r)
	})
}

// newID 生成一个 16 位十六进制随机 ID。
func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// statusRecorder 包装 ResponseWriter，在写响应的同时记录状态码。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader 记录状态码后，再委托给原始 ResponseWriter。
func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush 透传 Flusher 接口，避免破坏流式响应（后续 SSE 会用到）。
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// knownMethods 是合法的 HTTP 方法集合，用于把 method 标签压成有界取值。
var knownMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true,
	http.MethodPut: true, http.MethodPatch: true, http.MethodDelete: true,
	http.MethodOptions: true, http.MethodConnect: true, http.MethodTrace: true,
}

// methodLabel 归一化 HTTP 方法：自定义动词（客户端随便发）统一记成 OTHER，
// 否则 method 标签同样可以被构造出无限序列。
func methodLabel(m string) string {
	if knownMethods[m] {
		return m
	}
	return "OTHER"
}

// Logging 记录每个请求的方法、路径、状态码和耗时（结构化，与网关其他日志统一）。
//
// 指标标签用「路由模板」而不是原始路径：路径由客户端控制，
// 直接当标签会让随机 URL 不断造出新的时间序列，把 Prometheus 打爆。
// 日志里保留原始 path（日志没有基数问题），另加 route 字段便于对照。
func Logging(next http.Handler) http.Handler {
	return LoggingWithErrors(nil, "", next)
}

// LoggingWithErrors 同 Logging，并把失败请求交给 sink（nil = 不记录错误看板）。
//
// instance 用于标识条目来自哪个 gateway 实例：多实例共用一个 Redis 列表，
// 没有这个字段就看不出是哪台出的问题。
func LoggingWithErrors(sink ErrorSink, instance string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// 内层（路由 / 鉴权 / 限流 / AI 分流）把命中的有界标签写进这个持有者
		r = r.WithContext(withRouteLabel(r.Context()))
		if sink != nil {
			// 内层可以把「为什么失败」写进这个持有者（熔断 / 流中断 / 超时），
			// 外层据此归类，而不是只看状态码。
			r = r.WithContext(errorlog.WithHolder(r.Context()))
			// 身份同理：鉴权在内层，外层要一个快照才能记下租户与 Key。
			r = r.WithContext(withIdentityHolder(r.Context()))
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		route := RouteLabel(r.Context())
		if route == "" {
			route = LabelUnmatched
		}

		duration := time.Since(start)

		slog.Info("access_log",
			"method", r.Method,
			"path", r.URL.Path,
			"route", route,
			"status", rec.status,
			"duration_ms", duration.Milliseconds(),
			"request_id", r.Header.Get("X-Request-ID"),
		)

		status := strconv.Itoa(rec.status)
		method := methodLabel(r.Method)
		observability.RequestDuration.WithLabelValues(method, route, status).Observe(duration.Seconds())
		observability.RequestTotal.WithLabelValues(method, route, status).Inc()

		if sink != nil {
			if entry, ok := buildErrorEntry(instance, r, rec.status, route, duration); ok {
				sink.Record(entry)
			}
		}
	})
}
