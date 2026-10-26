package gateway

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// forwardTo 起一个回显上游，用给定的请求头走一次 forwardOnce，
// 返回上游实际收到的头。
func forwardTo(t *testing.T, setHeaders func(h http.Header)) http.Header {
	t.Helper()

	got := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("解析上游地址失败: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/echo", nil)
	req.RemoteAddr = "203.0.113.9:54321" // 固定来源，便于断言 X-Forwarded-For
	setHeaders(req.Header)

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	resp, err := forwardOnce(transport, req, target)
	if err != nil {
		t.Fatalf("forwardOnce 失败: %v", err)
	}
	resp.Body.Close()

	return <-got
}

// 网关自己的凭据不能流到上游：泄漏了就等于把访问网关的钥匙交给上游。
func TestForwardOnceStripsGatewayCredentials(t *testing.T) {
	got := forwardTo(t, func(h http.Header) {
		h.Set("Authorization", "Bearer fg-secret")
		h.Set("X-API-Key", "fg-secret")
		h.Set("X-Custom", "hello")
		h.Set("Content-Type", "application/json")
	})

	for _, name := range []string{"Authorization", "X-API-Key"} {
		if v := got.Get(name); v != "" {
			t.Fatalf("%s 不应透传给上游, got %q", name, v)
		}
	}

	// 业务头必须原样保留，剥离不能扩大打击面。
	if v := got.Get("X-Custom"); v != "hello" {
		t.Fatalf("业务头被误删: X-Custom=%q", v)
	}
	if v := got.Get("Content-Type"); v != "application/json" {
		t.Fatalf("Content-Type 被误删: %q", v)
	}
}

// 客户端伪造的 X-Forwarded-For / X-Real-IP 必须被网关的真实来源覆盖。
func TestForwardOnceOverwritesForwardedFor(t *testing.T) {
	got := forwardTo(t, func(h http.Header) {
		h.Set("X-Forwarded-For", "1.2.3.4")
		h.Set("X-Real-IP", "1.2.3.4")
	})

	if v := got.Get("X-Forwarded-For"); v != "203.0.113.9" {
		t.Fatalf("X-Forwarded-For 应被覆盖为真实来源, got %q", v)
	}
	if v := got.Get("X-Real-IP"); v != "" {
		t.Fatalf("X-Real-IP 不应透传, got %q", v)
	}
}

// Connection 头里点名的字段属于逐跳头，转发时一并删除。
func TestForwardOnceRemovesHopByHopHeaders(t *testing.T) {
	got := forwardTo(t, func(h http.Header) {
		h.Set("Connection", "X-Hop")
		h.Set("X-Hop", "should-be-removed")
		h.Set("Keep-Alive", "timeout=5")
		h.Set("X-Custom", "hello")
	})

	if v := got.Get("X-Hop"); v != "" {
		t.Fatalf("Connection 点名的头应被删除, got %q", v)
	}
	if v := got.Get("Keep-Alive"); v != "" {
		t.Fatalf("Keep-Alive 是逐跳头, got %q", v)
	}
	if v := got.Get("X-Custom"); v != "hello" {
		t.Fatalf("业务头被误删: X-Custom=%q", v)
	}
}
