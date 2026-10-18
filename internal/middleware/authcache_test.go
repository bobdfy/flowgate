package middleware

import (
	"testing"
	"time"

	"github.com/bobdfy/flowgate/internal/model"
)

// TestAuthCacheHit 验证正缓存命中：put 后 get 返回同一份 key。
func TestAuthCacheHit(t *testing.T) {
	c := newAuthCache(time.Minute)
	want := &model.APIKey{ID: 1, TenantID: 2, QPSLimit: 100, TenantQPSLimit: 1000}
	c.put("abc", want)

	got, hit := c.get("abc")
	if !hit {
		t.Fatal("期望命中，实际未命中")
	}
	if got == nil || got.ID != 1 || got.TenantID != 2 {
		t.Fatalf("缓存返回的 key 不对: %+v", got)
	}
}

// TestAuthCacheNegativeHit 验证负缓存：put(hash, nil) 后 get 返回 hit=true 且 key=nil。
// 这是「无效 key 不打穿数据库」的关键 —— 必须区分「负缓存命中」和「未命中」。
func TestAuthCacheNegativeHit(t *testing.T) {
	c := newAuthCache(time.Minute)
	c.put("nonexistent", nil)

	got, hit := c.get("nonexistent")
	if !hit {
		t.Fatal("负缓存应命中（hit=true），实际未命中")
	}
	if got != nil {
		t.Fatalf("负缓存应返回 nil key，实际 %+v", got)
	}
}

// TestAuthCacheExpire 验证 TTL 过期：过期后 get 返回未命中。
func TestAuthCacheExpire(t *testing.T) {
	c := newAuthCache(10 * time.Millisecond)
	c.put("abc", &model.APIKey{ID: 1})

	if _, hit := c.get("abc"); !hit {
		t.Fatal("未过期时应命中")
	}

	time.Sleep(15 * time.Millisecond)

	if _, hit := c.get("abc"); hit {
		t.Fatal("过期后应未命中")
	}
}

// TestAuthCacheDisabled 验证 ttl<=0 时缓存禁用：put 不生效，get 恒未命中。
func TestAuthCacheDisabled(t *testing.T) {
	c := newAuthCache(0)
	c.put("abc", &model.APIKey{ID: 1})

	if _, hit := c.get("abc"); hit {
		t.Fatal("禁用缓存时不应命中")
	}
}

// TestAuthCacheOverwrite 验证同一 hash 重复 put 会覆盖旧值。
func TestAuthCacheOverwrite(t *testing.T) {
	c := newAuthCache(time.Minute)
	c.put("abc", &model.APIKey{ID: 1})
	c.put("abc", &model.APIKey{ID: 2})

	got, _ := c.get("abc")
	if got == nil || got.ID != 2 {
		t.Fatalf("应返回最后一次写入的值，实际 %+v", got)
	}
}
