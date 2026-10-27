package gateway

import (
	"net/url"
	"testing"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/netguard"
)

// 连接池上限必须跟着服务配置走；不同并发配置不能串用同一个 Transport。
func TestProxyCacheTransportLimits(t *testing.T) {
	policy, err := netguard.NewPolicy(nil)
	if err != nil {
		t.Fatalf("构造策略失败: %v", err)
	}
	cache := NewProxyCache(policy)

	up, err := url.Parse("http://upstream.example")
	if err != nil {
		t.Fatalf("解析上游地址失败: %v", err)
	}

	svc := model.Service{
		Name:                    "svc-a",
		MaxConcurrency:          8,
		ConnectTimeoutMs:        1000,
		ResponseHeaderTimeoutMs: 2000,
	}

	ta := cache.get(up, svc)
	if ta.MaxConnsPerHost != 8 {
		t.Fatalf("MaxConnsPerHost 应跟随 MaxConcurrency，实际 %d", ta.MaxConnsPerHost)
	}
	if ta.MaxIdleConnsPerHost != 8 {
		t.Fatalf("MaxIdleConnsPerHost 应跟随 MaxConcurrency，实际 %d", ta.MaxIdleConnsPerHost)
	}
	if ta.DialContext == nil {
		t.Fatal("Transport 必须带上 netguard 的拨号校验")
	}

	// 只改并发配置：必须拿到新的 Transport，否则改配置不生效。
	svcMore := svc
	svcMore.MaxConcurrency = 16
	if tb := cache.get(up, svcMore); tb == ta {
		t.Fatal("并发配置不同却复用了同一个 Transport，改配置不会生效")
	}

	// 不限流的服务也要有兜底上限，不能真的无限开连接。
	svcUnlimited := svc
	svcUnlimited.MaxConcurrency = 0
	if tc := cache.get(up, svcUnlimited); tc.MaxConnsPerHost != 512 {
		t.Fatalf("max_concurrency=0 时应有 512 兜底上限，实际 %d", tc.MaxConnsPerHost)
	}

	// 完全相同的配置才复用。
	if again := cache.get(up, svc); again != ta {
		t.Fatal("相同配置应复用同一个 Transport")
	}
}
