package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bobdfy/flowgate/internal/overload"
)

// 被拒绝：503 + Retry-After，并且把「记哪个账本」交给调用方。
func TestWriteOverloadRejected(t *testing.T) {
	var gotReason string
	rec := httptest.NewRecorder()

	err := &overload.RejectedError{
		Reason:     overload.ReasonQueueTimeout,
		RetryAfter: 2 * time.Second,
		InFlight:   7,
	}
	if !WriteOverload(rec, err, "svc-a", func(reason string) { gotReason = reason }) {
		t.Fatal("已写过响应时必须返回 true")
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("期望 503，实际 %d", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After 期望 2，实际 %q", got)
	}
	if gotReason != string(overload.ReasonQueueTimeout) {
		t.Fatalf("回调应收到拒绝原因，实际 %q", gotReason)
	}
	if !strings.Contains(rec.Body.String(), "overload:"+string(overload.ReasonQueueTimeout)) {
		t.Fatalf("响应体应带上原因, got %q", rec.Body.String())
	}
}

// 排队等超时：不是「拒绝」，要回 504 让客户端知道是等超时了。
func TestWriteOverloadDeadlineIs504(t *testing.T) {
	rec := httptest.NewRecorder()
	if !WriteOverload(rec, context.DeadlineExceeded, "svc-a", nil) {
		t.Fatal("已写过响应时必须返回 true")
	}
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("期望 504，实际 %d", rec.Code)
	}
}

// 客户端断开：连接已经没了，不能回写，但必须告诉调用方「已处理」。
func TestWriteOverloadCanceledWritesNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	if !WriteOverload(rec, context.Canceled, "svc-a", nil) {
		t.Fatal("必须返回 true，让调用方直接 return")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("客户端已断开时不应写响应体, got %q", rec.Body.String())
	}
	if rec.Code != http.StatusOK { // httptest 的默认值，说明没调过 WriteHeader
		t.Fatalf("客户端已断开时不应写状态码，实际 %d", rec.Code)
	}
}

// inc 为 nil 不能 panic：路由级/服务级/实例级三处调用方都可能不关心指标。
func TestWriteOverloadNilInc(t *testing.T) {
	rec := httptest.NewRecorder()
	err := &overload.RejectedError{Reason: overload.ReasonFailFast}
	if !WriteOverload(rec, err, "x", nil) {
		t.Fatal("已写过响应时必须返回 true")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("期望 503，实际 %d", rec.Code)
	}
}
