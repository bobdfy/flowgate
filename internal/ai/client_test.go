package ai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bobdfy/flowgate/internal/netguard"
)

// AI 链路和普通转发共用同一套拨号校验：内网上游必须被拦。
func TestNewClientBlocksPrivateUpstream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 不传白名单 → 127.0.0.1 属于回环，应当被拒。
	resp, err := NewClient(nil).Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("回环地址应当被拨号层拒绝，但请求成功了")
	}
	if !strings.Contains(err.Error(), "拒绝连接内网地址") {
		t.Fatalf("错误信息里应能看出是 SSRF 拦截, got: %v", err)
	}
}

// 302 不能被跟随：否则上游可以把网关引到内网地址去。
func TestNewClientDoesNotFollowRedirect(t *testing.T) {
	var internalHit bool
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		internalHit = true
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

	resp, err := NewClient(policy).Get(redirector.URL)
	if err != nil {
		t.Fatalf("请求本身不应失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("应当把 302 原样返回，得到 %d", resp.StatusCode)
	}
	if internalHit {
		t.Fatal("跟随了重定向：第二跳已被发出，上游可以把网关引到内网")
	}
}

// 兜底行为：不传 client 时不能退化成「不校验」。
func TestNewHandlerFallsBackToGuardedClient(t *testing.T) {
	h := NewHandler(nil, nil)
	if h.client == nil || h.client.Transport == nil {
		t.Fatal("nil client 应回落到带校验的默认客户端")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	if _, err := h.client.Get(srv.URL); err == nil {
		t.Fatal("兜底客户端也必须拒绝内网地址")
	}
}
