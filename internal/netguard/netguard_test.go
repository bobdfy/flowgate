package netguard

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBlocked(t *testing.T) {
	tests := []struct {
		ip      string
		blocked bool
		why     string
	}{
		{"127.0.0.1", true, "回环"},
		{"::1", true, "IPv6 回环"},
		{"10.1.2.3", true, "私有网段"},
		{"172.16.0.1", true, "私有网段"},
		{"192.168.1.1", true, "私有网段"},
		{"169.254.169.254", true, "云元数据服务，最典型的 SSRF 目标"},
		{"0.0.0.0", true, "未指定地址"},
		{"0.1.2.3", true, "0.0.0.0/8 整段不可路由"},
		{"100.64.0.1", true, "CGNAT"},
		{"224.0.0.1", true, "组播"},
		{"fd00::1", true, "IPv6 ULA（IsPrivate 覆盖 fc00::/7）"},
		{"fe80::1", true, "IPv6 链路本地"},
		{"::ffff:127.0.0.1", true, "IPv4 映射的回环也必须认出来"},
		{"64:ff9b::127.0.0.1", true, "NAT64 映射到内网"},
		{"8.8.8.8", false, "公网"},
		{"1.1.1.1", false, "公网"},
		{"2001:4860:4860::8888", false, "公网 IPv6"},
	}

	for _, tt := range tests {
		if got := Blocked(net.ParseIP(tt.ip)); got != tt.blocked {
			t.Errorf("Blocked(%s)=%v, 期望 %v（%s）", tt.ip, got, tt.blocked, tt.why)
		}
	}

	if !Blocked(nil) {
		t.Error("nil IP 应当按拒绝处理")
	}
}

func TestNewPolicyParsesAllowlist(t *testing.T) {
	p, err := NewPolicy([]string{" 172.16.0.0/12 ", "", "127.0.0.1", "::1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(p.Allowed()); got != 3 {
		t.Fatalf("白名单条数=%d, 期望 3（空串应被跳过）: %v", got, p.Allowed())
	}

	// 单个 IP 自动补满掩码，不能变成放行整个网段。
	if p.Blocked(net.ParseIP("127.0.0.2")) != true {
		t.Error("127.0.0.1 的白名单不应连带放行 127.0.0.2")
	}

	if _, err := NewPolicy([]string{"10.0.0.0/33"}); err == nil {
		t.Error("非法 CIDR 应当报错")
	}
}

// 白名单优先于默认规则，这是本地联调/内网部署的唯一出口。
func TestPolicyAllowlistWins(t *testing.T) {
	p, err := NewPolicy([]string{"172.16.0.0/12", "127.0.0.1/32"})
	if err != nil {
		t.Fatal(err)
	}

	if p.Blocked(net.ParseIP("172.19.0.5")) {
		t.Error("白名单内的地址应当放行")
	}
	if !p.Blocked(net.ParseIP("10.0.0.5")) {
		t.Error("白名单外的私有地址仍应拒绝")
	}
	if !p.Blocked(net.ParseIP("169.254.169.254")) {
		t.Error("白名单不应影响其他内网地址")
	}

	// nil 策略等价于「什么都不放行」。
	var nilPolicy *Policy
	if !nilPolicy.Blocked(net.ParseIP("127.0.0.1")) {
		t.Error("nil Policy 也必须拒绝内网地址")
	}
}

func TestCheckHost(t *testing.T) {
	ctx := context.Background()
	p, _ := NewPolicy(nil)

	if err := p.CheckHost(ctx, "127.0.0.1"); err == nil {
		t.Error("字面量回环地址应被拒")
	}
	if err := p.CheckHost(ctx, "8.8.8.8"); err != nil {
		t.Errorf("公网字面量应放行: %v", err)
	}
	// localhost 走真实解析，双栈机器上会同时返回 ::1 和 127.0.0.1。
	if err := p.CheckHost(ctx, "localhost"); err == nil {
		t.Error("localhost 应被拒（解析到回环）")
	}
	if err := p.CheckHost(ctx, ""); err == nil {
		t.Error("空 host 应被拒")
	}

	allow, _ := NewPolicy([]string{"127.0.0.0/8", "::1/128"})
	if err := allow.CheckHost(ctx, "localhost"); err != nil {
		t.Errorf("加入白名单后 localhost 应放行: %v", err)
	}
}

// 关键用例：即使配置期检查被绕过（比如 DNS 事后改答案），
// 拨号层的 Control 也必须拦住。这里用真实 TCP 连接验证。
func TestDialerBlocksAtConnectTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	do := func(p *Policy) error {
		transport := &http.Transport{DialContext: p.Dialer(2 * time.Second).DialContext}
		defer transport.CloseIdleConnections()
		resp, err := transport.RoundTrip(httptest.NewRequest(http.MethodGet, srv.URL, nil))
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	}

	// httptest 监听在 127.0.0.1：默认策略下必须连不上。
	err := do(&Policy{})
	if err == nil {
		t.Fatal("回环地址应当被拨号层拒绝，但连接成功了")
	}
	if !strings.Contains(err.Error(), "拒绝连接内网地址") {
		t.Fatalf("错误信息里应能看出是 SSRF 拦截，got: %v", err)
	}

	// 显式放行后才允许连（本地 mock 上游就靠这个）。
	allow, _ := NewPolicy([]string{"127.0.0.1/32"})
	if err := do(allow); err != nil {
		t.Fatalf("白名单内的地址应当拨通: %v", err)
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("FLOWGATE_TEST_CIDRS", "10.0.0.0/8, 192.168.0.0/16")
	p, err := FromEnv("FLOWGATE_TEST_CIDRS")
	if err != nil {
		t.Fatal(err)
	}
	if p.Blocked(net.ParseIP("10.1.1.1")) {
		t.Error("环境变量里的网段应当放行")
	}

	t.Setenv("FLOWGATE_TEST_CIDRS", "")
	p, err = FromEnv("FLOWGATE_TEST_CIDRS")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Blocked(net.ParseIP("10.1.1.1")) {
		t.Error("变量为空时应回到默认拒绝")
	}
}
