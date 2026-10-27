package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/observability"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// fakeKeyStore 是 keyLookup 的假实现：记录回源次数，可注入延迟和错误。
// 有了 keyLookup 接口才能这么测 —— 否则每条用例都得连真库。
type fakeKeyStore struct {
	calls atomic.Int64
	key   *model.APIKey
	err   error
	delay time.Duration
}

func (f *fakeKeyStore) GetByHash(context.Context, string) (*model.APIKey, error) {
	f.calls.Add(1)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	return f.key, f.err
}

// 缓存击穿（cache stampede）：热点 key 的缓存刚过期时，几百个并发请求同时未命中。
// singleflight 必须把它们合并成一次回源，其余等第一个的结果。
func TestLoadKeyCollapsesConcurrentMisses(t *testing.T) {
	store := &fakeKeyStore{key: &model.APIKey{ID: 7}, delay: 20 * time.Millisecond}
	a := NewAuthenticatorWithCache(store, time.Minute)

	const n = 100
	start := make(chan struct{})
	var wg sync.WaitGroup
	var bad atomic.Int64

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			k, err := a.loadKey(context.Background(), "hash-1")
			if err != nil || k == nil || k.ID != 7 {
				bad.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := bad.Load(); got != 0 {
		t.Fatalf("%d 个并发请求没拿到正确结果", got)
	}
	if got := store.calls.Load(); got != 1 {
		t.Fatalf("100 个同 key 并发请求应只回源 1 次，实际 %d 次", got)
	}
}

// 查无此 key 也要缓存（负缓存）。否则攻击者用随机 key 乱扫，
// 每个请求都是一个缓存未命中 + 一次数据库查询，一条命令就能把库打穿。
func TestLoadKeyCachesNotFound(t *testing.T) {
	store := &fakeKeyStore{}
	a := NewAuthenticatorWithCache(store, time.Minute)

	before := testutil.ToFloat64(observability.AuthDBSearchesTotal.WithLabelValues("not_found"))

	for i := 0; i < 3; i++ {
		k, err := a.loadKey(context.Background(), "nope")
		if err != nil || k != nil {
			t.Fatalf("查无此 key 应返回 (nil, nil)，实际 (%v, %v)", k, err)
		}
	}

	if got := store.calls.Load(); got != 1 {
		t.Fatalf("负缓存生效后应只回源 1 次，实际 %d 次", got)
	}
	if d := testutil.ToFloat64(observability.AuthDBSearchesTotal.WithLabelValues("not_found")) - before; d != 1 {
		t.Fatalf("not_found 计数应为 1，实际增加 %v", d)
	}
}

// 回源失败绝不能写缓存：否则数据库抖一下，「失败」会被钉死一整个 TTL，
// 期间所有持合法 key 的请求都被判成无效。
func TestLoadKeyDoesNotCacheErrors(t *testing.T) {
	store := &fakeKeyStore{err: errors.New("db down")}
	a := NewAuthenticatorWithCache(store, time.Minute)

	if _, err := a.loadKey(context.Background(), "hash-x"); err == nil {
		t.Fatal("回源错误应向上返回")
	}
	if n := a.cache.len(); n != 0 {
		t.Fatalf("错误结果不该写进缓存，实际 %d 条", n)
	}

	// 数据库恢复后必须能重新查出来，而不是一直吃缓存里的「失败」
	store.err = nil
	store.key = &model.APIKey{ID: 9}
	k, err := a.loadKey(context.Background(), "hash-x")
	if err != nil || k == nil || k.ID != 9 {
		t.Fatalf("恢复后应查到 key，实际 (%v, %v)", k, err)
	}
	if got := store.calls.Load(); got != 2 {
		t.Fatalf("应回源 2 次，实际 %d 次", got)
	}
}

// 缓存关闭时也要记回源次数 —— 这个指标的全部意义就是对比「开缓存前 / 后」的数据库压力。
func TestDBSeachesCountedWhenCacheDisabled(t *testing.T) {
	store := &fakeKeyStore{key: &model.APIKey{ID: 1}}
	a := NewAuthenticator(store) // 缓存关闭

	before := testutil.ToFloat64(observability.AuthDBSearchesTotal.WithLabelValues("found"))
	for i := 0; i < 3; i++ {
		if _, err := a.loadKey(context.Background(), "h"); err != nil {
			t.Fatal(err)
		}
	}

	if got := store.calls.Load(); got != 3 {
		t.Fatalf("缓存关闭时每次都要回源，应 3 次，实际 %d 次", got)
	}
	if d := testutil.ToFloat64(observability.AuthDBSearchesTotal.WithLabelValues("found")) - before; d != 3 {
		t.Fatalf("回源计数应为 3，实际增加 %v", d)
	}
}

// 一次未命中只记一次：RequireAuth 外层已经记过，
// singleflight 里的二次检查（peek）不能再记一遍，否则命中率被系统性低估一半。
func TestRequireAuthCountsOneMissPerMiss(t *testing.T) {
	store := &fakeKeyStore{key: &model.APIKey{ID: 1, TenantID: 2}}
	a := NewAuthenticatorWithCache(store, time.Minute)

	handler := a.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := FromContext(r.Context())
		if !ok || id.KeyID != 1 || id.TenantID != 2 {
			t.Errorf("身份没注入到 context: %+v ok=%v", id, ok)
		}
		w.WriteHeader(http.StatusOK)
	}), nil)

	do := func() int {
		req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		req.Header.Set("X-API-Key", "fg-test")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	misses := testutil.ToFloat64(observability.AuthCacheMissesTotal)
	if code := do(); code != http.StatusOK {
		t.Fatalf("首次请求应 200，实际 %d", code)
	}
	if d := testutil.ToFloat64(observability.AuthCacheMissesTotal) - misses; d != 1 {
		t.Fatalf("一次未命中应只记 1 次，实际 %v（内层重复记账？）", d)
	}

	hits := testutil.ToFloat64(observability.AuthCacheHitsTotal)
	if code := do(); code != http.StatusOK {
		t.Fatalf("第二次请求应 200，实际 %d", code)
	}
	if d := testutil.ToFloat64(observability.AuthCacheHitsTotal) - hits; d != 1 {
		t.Fatalf("第二次应记 1 次命中，实际 %v", d)
	}
	if got := store.calls.Load(); got != 1 {
		t.Fatalf("第二次应命中缓存、不再回源，实际回源 %d 次", got)
	}
}
