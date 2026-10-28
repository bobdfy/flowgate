package middleware

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 谎报 Content-Length 的客户端（或者用 chunked 慢慢塞）不能绕过限制：
// 上限必须落在读取路径上，而不是只看请求头。
func TestLimitBodyRejectsHonestOversize(t *testing.T) {
	called := false
	h := LimitBody(16, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("a", 64)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超限应回 413，实际 %d", rec.Code)
	}
	if called {
		t.Fatal("超限请求不该进入 handler")
	}
	if got := rec.Header().Get("Connection"); got != "close" {
		t.Fatalf("超限响应应要求关闭连接，实际 Connection=%q", got)
	}
}

func TestLimitBodyAllowsUnderLimit(t *testing.T) {
	var got string
	h := LimitBody(16, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || got != "hello" {
		t.Fatalf("正常请求应原样通过: status=%d body=%q", rec.Code, got)
	}
}

// Content-Length 未知（chunked，ContentLength=-1）时只有 MaxBytesReader 能拦。
func TestLimitBodyRejectsUnknownLength(t *testing.T) {
	var readErr error
	h := LimitBody(16, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		readErr = json.NewDecoder(r.Body).Decode(&v)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"a":"`+strings.Repeat("x", 64)+`"}`))
	req.ContentLength = -1 // 模拟 chunked
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var maxErr *http.MaxBytesError
	if !errors.As(readErr, &maxErr) {
		t.Fatalf("读取方应拿到 *http.MaxBytesError，实际 %v", readErr)
	}
	if maxErr.Limit != 16 {
		t.Fatalf("Limit=%d want 16", maxErr.Limit)
	}
}

// maxBytes<=0 表示不限制，用于内部调用方。
func TestLimitBodyDisabled(t *testing.T) {
	h := LimitBody(0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("a", 1024)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("不限制时应直接透传，实际 %d", rec.Code)
	}
}
