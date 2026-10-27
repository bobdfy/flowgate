package middleware

import (
	"testing"
	"time"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/observability"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// 容量上限必须真的生效：超过上限时淘汰最久未使用的（LRU），而不是让 map 无界增长。
// 无界缓存 = 慢速内存泄漏，攻击者用海量随机 key 就能把进程撑爆。
func TestAuthCacheEvictsLRUAtCapacity(t *testing.T) {
	c := newAuthCache(time.Minute)
	c.maxItems = 2 // 默认 10000 太大，测的时候缩小

	c.put("a", &model.APIKey{ID: 1})
	c.put("b", &model.APIKey{ID: 2})
	c.get("a") // 触碰 a：现在 b 才是最久未使用的

	evictions := testutil.ToFloat64(observability.AuthCacheEvictionsTotal)

	c.put("c", &model.APIKey{ID: 3})

	if _, hit := c.get("b"); hit {
		t.Fatal("最久未使用的 b 应被淘汰")
	}
	if _, hit := c.get("a"); !hit {
		t.Fatal("刚被访问过的 a 不该被淘汰")
	}
	if _, hit := c.get("c"); !hit {
		t.Fatal("新写入的 c 应命中")
	}
	if n := c.len(); n != 2 {
		t.Fatalf("容量上限为 2，实际 %d 条", n)
	}
	if d := testutil.ToFloat64(observability.AuthCacheEvictionsTotal) - evictions; d != 1 {
		t.Fatalf("应淘汰 1 条，实际淘汰计数增加 %v", d)
	}
}

// 满额时先清已过期的条目。过期数据本来就该消失，能腾出位置就不用牺牲未过期的数据。
func TestAuthCachePurgesExpiredBeforeEvicting(t *testing.T) {
	c := newAuthCache(10 * time.Millisecond)
	c.maxItems = 3
	c.put("a", &model.APIKey{ID: 1})
	c.put("b", &model.APIKey{ID: 2})
	c.put("c", &model.APIKey{ID: 3})

	time.Sleep(15 * time.Millisecond) // 让三条都过期

	evictions := testutil.ToFloat64(observability.AuthCacheEvictionsTotal)
	c.put("d", &model.APIKey{ID: 4})

	if d := testutil.ToFloat64(observability.AuthCacheEvictionsTotal) - evictions; d != 0 {
		t.Fatalf("清掉过期条目就够腾位置了，不该再淘汰未过期条目，实际淘汰 %v 条", d)
	}
	if n := c.len(); n != 1 {
		t.Fatalf("只剩新写入的 d，实际 %d 条", n)
	}
}

// 负缓存的 TTL 是正缓存的 1/5：「查无此 key」这个结论更容易变化
// （管理员刚新建的 key 必须尽快能用），所以它不该和正常 key 活得一样久。
func TestAuthCacheNegativeTTLShorterThanPositive(t *testing.T) {
	c := newAuthCache(time.Second) // negTTL = 1s / 5 = 200ms
	c.put("positive", &model.APIKey{ID: 1})
	c.put("negative", nil)

	time.Sleep(400 * time.Millisecond) // 越过负缓存 TTL，但远没到正缓存 TTL

	if _, hit := c.peek("negative"); hit {
		t.Fatal("负缓存应已过期（TTL 200ms）")
	}
	if _, hit := c.peek("positive"); !hit {
		t.Fatal("正缓存不该过期（TTL 1s）")
	}
}

// 过期条目必须就地删掉。留着不动的话它会一直占着容量名额，
// 把还有用的条目挤出去 —— 缓存越跑越差却不报错。
func TestAuthCacheExpiredEntryDeletedInPlace(t *testing.T) {
	c := newAuthCache(10 * time.Millisecond)
	c.put("abc", &model.APIKey{ID: 1})

	time.Sleep(15 * time.Millisecond)

	if _, hit := c.peek("abc"); hit {
		t.Fatal("过期后应未命中")
	}
	if n := c.len(); n != 0 {
		t.Fatalf("过期条目应被就地删除，实际还占着 %d 个名额", n)
	}
}

// 计数器口径：
//   - peek 不记任何指标（它是 singleflight 里的二次检查，外层 get 已经记过了）
//   - get 一次命中 / 未命中各只记一次
//   - 缓存关闭时不记未命中，否则关缓存压测会让看板误以为「缓存全线失效」
func TestAuthCacheMetricsCounting(t *testing.T) {
	c := newAuthCache(time.Minute)
	c.put("a", &model.APIKey{ID: 1})

	hits := testutil.ToFloat64(observability.AuthCacheHitsTotal)
	misses := testutil.ToFloat64(observability.AuthCacheMissesTotal)

	c.peek("a")
	c.peek("absent")
	if d := testutil.ToFloat64(observability.AuthCacheHitsTotal) - hits; d != 0 {
		t.Fatalf("peek 不该记命中，实际增加 %v", d)
	}
	if d := testutil.ToFloat64(observability.AuthCacheMissesTotal) - misses; d != 0 {
		t.Fatalf("peek 不该记未命中，实际增加 %v", d)
	}

	c.get("a")
	c.get("absent")
	if d := testutil.ToFloat64(observability.AuthCacheHitsTotal) - hits; d != 1 {
		t.Fatalf("一次命中应只记 1，实际 %v", d)
	}
	if d := testutil.ToFloat64(observability.AuthCacheMissesTotal) - misses; d != 1 {
		t.Fatalf("一次未命中应只记 1，实际 %v", d)
	}

	disabled := newAuthCache(0)
	misses = testutil.ToFloat64(observability.AuthCacheMissesTotal)
	disabled.get("x")
	if d := testutil.ToFloat64(observability.AuthCacheMissesTotal) - misses; d != 0 {
		t.Fatalf("缓存关闭时不该记未命中，实际增加 %v", d)
	}
}
