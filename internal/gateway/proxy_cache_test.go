package gateway

import (
	"net/url"
	"testing"
	"time"

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

// Sweep 只回收「超过阈值没被用到」的 Transport，最近用过的原样保留。
func TestProxyCacheSweep(t *testing.T) {
	policy, err := netguard.NewPolicy(nil)
	if err != nil {
		t.Fatalf("构造策略失败: %v", err)
	}
	cache := NewProxyCache(policy)

	up, err := url.Parse("http://upstream.example")
	if err != nil {
		t.Fatalf("解析上游地址失败: %v", err)
	}

	svcA := model.Service{Name: "a", MaxConcurrency: 8}
	svcB := model.Service{Name: "b", MaxConcurrency: 16}

	stale := cache.get(up, svcA)
	cache.get(up, svcB) // 这个刚被用过，不该被回收

	// 把 A 的最后使用时间回拨一小时，模拟「配置早已删掉、再没人取过」。
	cache.mu.Lock()
	cache.m[transportKey(up, svcA)].lastUsed = time.Now().Add(-time.Hour)
	cache.mu.Unlock()

	if n := cache.Sweep(10 * time.Minute); n != 1 {
		t.Fatalf("期望回收 1 个，实际回收 %d 个", n)
	}

	cache.mu.Lock()
	_, hasA := cache.m[transportKey(up, svcA)]
	_, hasB := cache.m[transportKey(up, svcB)]
	cache.mu.Unlock()

	if hasA {
		t.Fatal("超时未使用的 Transport 应被回收")
	}
	if !hasB {
		t.Fatal("刚用过的 Transport 不能被回收")
	}

	// 回收后再要同一个配置，必须新建，而不是拿到已经退役的那个。
	if again := cache.get(up, svcA); again == stale {
		t.Fatal("回收后应重新构造 Transport")
	}
}

// 时间没到的条目一律不动：Sweep 每 5 秒跑一次，不能误伤正在用的池子。
func TestProxyCacheSweepKeepsFreshEntries(t *testing.T) {
	policy, err := netguard.NewPolicy(nil)
	if err != nil {
		t.Fatalf("构造策略失败: %v", err)
	}
	cache := NewProxyCache(policy)

	up, _ := url.Parse("http://upstream.example")
	cache.get(up, model.Service{Name: "a", MaxConcurrency: 8})

	if n := cache.Sweep(10 * time.Minute); n != 0 {
		t.Fatalf("刚创建的 Transport 不应被回收，实际回收 %d 个", n)
	}
	cache.mu.Lock()
	got := len(cache.m)
	cache.mu.Unlock()
	if got != 1 {
		t.Fatalf("缓存条目数应仍为 1，实际 %d", got)
	}
}
