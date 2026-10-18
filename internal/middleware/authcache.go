package middleware

import (
	"sync"
	"time"

	"github.com/bobdfy/flowgate/internal/model"
)

// authCacheEntry 是鉴权缓存的一条记录。
// 负缓存（查无此 key）也缓存
type authCacheEntry struct {
	key      *model.APIKey // nil = 负缓存（该 hash 查无有效 key）
	expireAt time.Time
}

// authCache 是 hash → APIKey 的进程内 TTL 缓存。
type authCache struct {
	mu    sync.RWMutex
	ttl   time.Duration
	items map[string]authCacheEntry
}

// newAuthCache 创建鉴权缓存。ttl <= 0 时缓存禁用（每请求都查库），
// 与未启用缓存的旧行为一致，便于 A/B 对照。
func newAuthCache(ttl time.Duration) *authCache {
	return &authCache{
		ttl:   ttl,
		items: make(map[string]authCacheEntry),
	}
}

// get 返回 hash 对应的缓存项。
// 返回 (key, hit)：key 为 nil 时可能是「负缓存命中」或「未命中」
func (c *authCache) get(hash string) (*model.APIKey, bool) {
	if c.ttl <= 0 {
		return nil, false
	}
	now := time.Now()
	c.mu.RLock()
	e, ok := c.items[hash]
	c.mu.RUnlock()
	if !ok || now.After(e.expireAt) {
		return nil, false
	}
	return e.key, true
}

// put 写入一条缓存（正缓存 key!=nil，负缓存 key=nil）。
func (c *authCache) put(hash string, key *model.APIKey) {
	if c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	c.items[hash] = authCacheEntry{key: key, expireAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}
