package ai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// P1-6 的边界：AI 链路**不做透明重试**，也不做跨 Provider 重放。
//
// 这两条不是「暂时没写」，而是刻意不做：
//   - Chat POST 不幂等，重试会产生重复计费；
//   - 流已经开始输出后换一个上游重放，用户会看到两段拼接的内容。
//
// 所以这里的测试断言的是「上游只被打了一次」，而不是「重试后成功」。

func TestServeHTTPDoesNotRetryOnUpstream5xx(t *testing.T) {
	var hits atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer up.Close()

	h := NewHandler(map[string]*ProviderEntry{
		"m": {Name: "p", BaseURL: up.URL},
	}, NewClients(allowLoopback(t), DefaultTimeouts()))

	body := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("上游 500 应原样透传，实际 %d", rec.Code)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("上游应只被打 1 次，实际 %d 次 —— 出现了透明重试", got)
	}
}

// 流式：已经吐出一段内容后上游断开，不能换上游重放。
func TestServeHTTPDoesNotReplayStreamAfterPartialOutput(t *testing.T) {
	var hits atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)

		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("测试上游需要 Hijacker 才能模拟中途断流")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()

		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\n\r\n")
		_, _ = buf.WriteString("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		_ = buf.Flush()
		// 不发 [DONE]，直接断：模拟上游中途死亡
	}))
	defer up.Close()

	h := NewHandler(map[string]*ProviderEntry{
		"m": {Name: "p", BaseURL: up.URL},
	}, NewClients(allowLoopback(t), DefaultTimeouts()))
	h.SetStreamingForwarder(streamProxy)

	body := `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	if got := hits.Load(); got != 1 {
		t.Fatalf("流式中途失败后不应重新请求上游，实际打了 %d 次", got)
	}
	// 客户端看到的内容必须只有一份，不能因为重放出现两段。
	if got := strings.Count(rec.Body.String(), "partial"); got != 1 {
		t.Fatalf("首段内容应只出现一次，实际 %d 次", got)
	}
}

// 上游连不上（连接层失败）同样不重试。
func TestServeHTTPDoesNotRetryOnDialFailure(t *testing.T) {
	var reqs atomic.Int64

	// 一个立即关闭的监听：能建立 TCP 连接但不回任何数据。
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.Add(1)
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hj.Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer up.Close()

	h := NewHandler(map[string]*ProviderEntry{
		"m": {Name: "p", BaseURL: up.URL},
	}, NewClients(allowLoopback(t), DefaultTimeouts()))

	body := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	if rec.Code != http.StatusBadGateway && rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("连接失败应回 502/503，实际 %d", rec.Code)
	}
	if got := reqs.Load(); got != 1 {
		t.Fatalf("连接失败后不应重试，实际打了 %d 次", got)
	}
}

// 确保 writeError 的 OpenAI 结构在 AI 路径上是统一的（P1-6「统一错误语义」）。
func TestUpstreamErrorsUseOpenAIEnvelope(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":"bad gateway"}`)
	}))
	defer up.Close()

	h := NewHandler(map[string]*ProviderEntry{
		"m": {Name: "p", BaseURL: up.URL},
	}, NewClients(allowLoopback(t), DefaultTimeouts()))

	body := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("错误体应为 OpenAI 结构，实际 %s", rec.Body.String())
	}
}
