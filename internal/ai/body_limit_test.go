package ai

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 超限要回 413 并说清楚原因。
// 改造前用 io.LimitReader 静默截断，客户端只会看到「请求体不合法」，无从判断是自己发太大了。
func TestHandleRejectsOversizedBody(t *testing.T) {
	h := NewHandler(nil, nil)

	body := bytes.Repeat([]byte("a"), maxRequestBody+1)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超大请求体应回 413，实际 %d（body=%q）", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "请求体超过上限") {
		t.Fatalf("错误信息应说明是体积超限, got %q", rec.Body.String())
	}
}
