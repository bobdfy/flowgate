package ai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bobdfy/flowgate/internal/netguard"
)

// AI 链路和普通转发共用同一套拨号校验：内网上游必须被拦。
func TestNewClientsBlocksPrivateUpstream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 不传白名单 → 127.0.0.1 属于回环，应当被拒。
	resp, err := NewClients(nil).Stream.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("回环地址应当被拨号层拒绝，但请求成功了")
	}
	if !strings.Contains(err.Error(), "拒绝连接内网地址") {
		t.Fatalf("错误信息里应能看出是 SSRF 拦截, got: %v", err)
	}
}

// 302 不能被跟随：否则上游可以把网关引到内网地址去。
func TestNewClientsDoesNotFollowRedirect(t *testing.T) {
	var internalHit atomic.Bool
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		internalHit.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer internal.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL, http.StatusFound)
	}))
	defer redirector.Close()

	// 放行 127.0.0.1，让请求能真正打到 redirector —— 这样测的是「不跟随」这个行为本身，
	// 而不是被拨号校验顺手拦下。
	policy, err := netguard.NewPolicy([]string{"127.0.0.1/32"})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := NewClients(policy).Stream.Get(redirector.URL)
	if err != nil {
		t.Fatalf("请求本身不应失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("应当把 302 原样返回，得到 %d", resp.StatusCode)
	}
	if internalHit.Load() {
		t.Fatal("跟随了重定向：第二跳已被发出，上游可以把网关引到内网")
	}
}

// 兜底行为：不传 clients 时不能退化成「不校验」。
func TestNewHandlerFallsBackToGuardedClients(t *testing.T) {
	h := NewHandler(nil, nil)
	if h.clients == nil || h.clients.Stream.Transport == nil {
		t.Fatal("nil clients 应回落到带校验的默认客户端")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	if _, err := h.clients.Stream.Get(srv.URL); err == nil {
		t.Fatal("兜底客户端也必须拒绝内网地址")
	}
}

// 流式 client 不能有总超时。
//
// http.Client.Timeout 覆盖的是「建连 + 重定向 + 读完响应体」的全部时间，
// 一旦设置，超过该时长的 SSE 流会在中途被硬掐断——客户端只看到半句话，不会报错。
// 非流式则相反：必须有时长上界，否则上游慢滴流可以永久占住 goroutine。
func TestStreamClientHasNoTotalTimeout(t *testing.T) {
	c := NewClients(nil)

	if c.Stream.Timeout != 0 {
		t.Fatalf("流式 client 不能设置 Timeout（会掐断长流），当前 %s", c.Stream.Timeout)
	}
	if c.Buffered.Timeout <= 0 {
		t.Fatalf("非流式 client 必须有总超时，当前 %s", c.Buffered.Timeout)
	}
	if c.Stream.Transport != c.Buffered.Transport {
		t.Fatal("两个 client 必须共用同一个 Transport，否则连接池会变成两份")
	}
}

// 一次请求只能落到一个 client 上：streaming 走 Stream，其余走 Buffered。
type stubTransport struct {
	hits atomic.Int32
}

func (s *stubTransport) RoundTrip(*http.Request) (*http.Response, error) {
	s.hits.Add(1)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

func TestForwardPicksClientByStreaming(t *testing.T) {
	cases := []struct {
		name      string
		streaming bool
	}{
		{"流式", true},
		{"非流式", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stream, buffered := &stubTransport{}, &stubTransport{}
			h := NewHandler(nil, &Clients{
				Stream:   &http.Client{Transport: stream},
				Buffered: &http.Client{Transport: buffered},
			})
			entry := &ProviderEntry{Name: "p", BaseURL: "http://upstream.example"}

			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			resp, err := h.forward(req, entry, ChatCompletions, nil, []byte(`{}`), tc.streaming)
			if err != nil {
				t.Fatalf("forward 失败: %v", err)
			}
			resp.Body.Close()

			wantStream, wantBuffered := int32(0), int32(0)
			if tc.streaming {
				wantStream = 1
			} else {
				wantBuffered = 1
			}
			if got := stream.hits.Load(); got != wantStream {
				t.Fatalf("Stream 命中 %d 次，期望 %d", got, wantStream)
			}
			if got := buffered.hits.Load(); got != wantBuffered {
				t.Fatalf("Buffered 命中 %d 次，期望 %d", got, wantBuffered)
			}
		})
	}
}

// 非流式 client 的超时必须是可观测的硬上界，不能只写在注释里。
// 这里用 50ms 的假 client 证明：慢响应会在超时后失败，而不是无限等待。
func TestBufferedClientTimeoutIsEnforced(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer slow.Close()

	policy, err := netguard.NewPolicy([]string{"127.0.0.1/32"})
	if err != nil {
		t.Fatal(err)
	}
	c := NewClients(policy)
	c.Buffered.Timeout = 50 * time.Millisecond // 仅本测试：缩短以便快速验证语义

	if _, err := c.Buffered.Get(slow.URL); err == nil {
		t.Fatal("超过总时长的请求应当失败")
	}

	// 同样的慢响应，流式 client 没有总超时，应当正常完成。
	c.Stream.Transport = c.Buffered.Transport // 复用同一个已放行的 Transport
	resp, err := c.Stream.Get(slow.URL)
	if err != nil {
		t.Fatalf("流式 client 不应有总超时: %v", err)
	}
	resp.Body.Close()
}
