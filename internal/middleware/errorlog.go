package middleware

import (
	"net/http"
	"time"

	"github.com/bobdfy/flowgate/internal/errorlog"
)

// ErrorSink 接收失败请求（*errorlog.Recorder 实现）。
type ErrorSink interface {
	Record(errorlog.Entry)
}

// shouldRecord 判断这次请求算不算「失败请求」。
//
// 不记 404：扫描器每天能造出成千上万条 404，既没有排障价值，又会把
// 真正的问题挤出 500 条的窗口。4xx 里只记与「看门人」相关的三种：
// 鉴权失败（Key 配错 / 被吊销）、限流（配额打满）、429（上游限流）。
func shouldRecord(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		return true
	}
	return status >= 500
}

// kindForStatus 是内层没写归因时的兜底分类。
func kindForStatus(status int) errorlog.Kind {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return errorlog.KindAuth
	case status == http.StatusTooManyRequests:
		return errorlog.KindOverload
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		return errorlog.KindTimeout
	case status == http.StatusServiceUnavailable:
		// 网关自己的 503 基本都是过载拒绝；熔断由 AI 层显式写归因覆盖。
		return errorlog.KindOverload
	case status == http.StatusInternalServerError:
		return errorlog.KindInternal
	case status >= 500:
		return errorlog.KindUpstream
	default:
		return errorlog.KindInternal
	}
}

// buildErrorEntry 组装一条错误看板记录；ok=false 表示这条请求不该记。
func buildErrorEntry(instance string, r *http.Request, status int, route string, duration time.Duration) (errorlog.Entry, bool) {
	kind, detail, forced := errorlog.Get(r.Context())
	// forced = 内层明确说「这是一次失败」，即使响应头已经写出 200
	// （流式响应中途断开就是这种情况）。
	if !forced && !shouldRecord(status) {
		return errorlog.Entry{}, false
	}
	if kind == "" {
		kind = kindForStatus(status)
	}

	e := errorlog.Entry{
		TS:         time.Now(),
		Instance:   instance,
		RequestID:  r.Header.Get("X-Request-ID"),
		Method:     methodLabel(r.Method),
		Route:      route,
		Status:     status,
		DurationMS: duration.Milliseconds(),
		Kind:       kind,
		Detail:     errorlog.Truncate(detail),
	}
	if id, ok := IdentitySnapshot(r.Context()); ok {
		e.TenantID = id.TenantID
		e.KeyID = id.KeyID
	}
	return e, true
}
