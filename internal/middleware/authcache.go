package middleware

import (
	"container/list"
	"sync"
	"time"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/observability"
)

const (
	// defaultAuthCacheMax 条目上限
	defaultAuthCacheMax = 10000

	// negativeTTLDiv 负缓存 TTL = 正缓存 TTL / 5
	negativeTTLDiv = 5
)

// authCacheEntry 是鉴权缓存的一条记录。
// 负缓存（查无此 key）也缓存
type authCacheEntry struct {
	hash     string
	key      *model.APIKey // nil = 负缓存
	expireAt time.Time
}

// authCache 是 hash → APIKey 的进程内 TTL 缓存。
type authCache struct {
	mu       sync.RWMutex
	ttl      time.Duration
	negTTL   time.Duration
	maxItems int

	ll    *list.List
	items map[string]*list.Element
}

// newAuthCache 创建鉴权缓存。ttl <= 0 时缓存禁用（每请求都查库），
// 与未启用缓存的旧行为一致，便于 A/B 对照。
func newAuthCache(ttl time.Duration) *authCache {
	negTTL := ttl / negativeTTLDiv
	if negTTL <= 0 {
		negTTL = ttl
	}
	return &authCache{
		ttl:      ttl,
		negTTL:   negTTL,
		maxItems: defaultAuthCacheMax,
		ll:       list.New(),
		items:    make(map[string]*list.Element),
	}
}

func (c *authCache) disabled() bool {
	return c.ttl <= 0
}

// peek 只查缓存，不动指标。给 singleflight 内的二次检查用（外层已经记过了）。
func (c *authCache) peek(hash string) (*model.APIKey, bool) {
	if c.disabled() {
		return nil, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[hash]
	if !ok {
		return nil, false
	}

	e := el.Value.(*authCacheEntry)

	// 过期就地删除：留着会一直占容量名额，让有用的条目被挤掉。
	if time.Now().After(e.expireAt) {
		c.dropLocked(el)
		c.publishLenLocked()
		return nil, false
	}

	c.ll.MoveToFront(el)

	return e.key, true

}

// get 返回 hash 对应的缓存项。
// 返回 (key, hit)：key 为 nil 时可能是「负缓存命中」或「未命中」
func (c *authCache) get(hash string) (*model.APIKey, bool) {
	if c.disabled() {
		return nil, false // 缓存关闭时不算未命中，否则指标全是噪声
	}
	k, hit := c.peek(hash)
	if hit {
		observability.AuthCacheHitsTotal.Inc()
	} else {
		observability.AuthCacheMissesTotal.Inc()
	}
	return k, hit
}

// put 写入一条缓存（正缓存 key!=nil，负缓存 key=nil）。
func (c *authCache) put(hash string, key *model.APIKey) {
	if c.disabled() {
		return
	}

	ttl := c.ttl
	if key == nil {
		ttl = c.negTTL
	}
	e := &authCacheEntry{hash: hash, key: key, expireAt: time.Now().Add(ttl)}

	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[hash]; ok {
		el.Value = e
		c.ll.MoveToFront(el)
		return
	}

	// 满了：先清掉已经过期的（白扔的便宜不捡白不捡），还满就淘汰最久未使用的。
	if len(c.items) >= c.maxItems {
		c.purgeExpiredLocked()
	}
	for len(c.items) >= c.maxItems {
		back := c.ll.Back()
		if back == nil {
			break
		}
		c.dropLocked(back)
		observability.AuthCacheEvictionsTotal.Inc()
	}

	c.items[hash] = c.ll.PushFront(e)
	c.publishLenLocked()
}

// clear 清空全部缓存。鉴权配置版本号变化时整体失效，
// 代价是下一轮请求重新查库（key 数量通常很小，可接受）。
func (c *authCache) clear() {
	c.mu.Lock()
	c.items = make(map[string]*list.Element)
	c.ll.Init()
	c.publishLenLocked()
	c.mu.Unlock()
}

// len 返回当前条目数（测试用）。
func (c *authCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// dropLocked 删除一个元素。调用方必须已持有 c.mu。
func (c *authCache) dropLocked(el *list.Element) {
	e := el.Value.(*authCacheEntry)
	delete(c.items, e.hash)
	c.ll.Remove(el)
}

// purgeExpiredLocked 清掉所有已过期条目。调用方必须已持有 c.mu。
func (c *authCache) purgeExpiredLocked() {
	now := time.Now()
	for _, el := range c.items { // 边遍历边删除是 Go 明确允许的
		if now.After(el.Value.(*authCacheEntry).expireAt) {
			c.dropLocked(el)
		}
	}
}

// publishLenLocked 上报条目数。必须在锁内调用：len(c.items) 在锁外读是数据竞争。
func (c *authCache) publishLenLocked() {
	observability.AuthCacheEntries.Set(float64(len(c.items)))
}
