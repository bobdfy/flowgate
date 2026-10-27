package quota

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// 集成测试：需要真实 Redis。
//
//	REDIS_ADDR=127.0.0.1:6380 REDIS_PASSWORD=flowgate go test ./internal/quota -run Integration -v
func TestRedisTPMIntegration(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR 未设置，跳过 Redis 集成测试")
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: os.Getenv("REDIS_PASSWORD"),
	})
	defer rdb.Close()

	ctx := context.Background()
	tenantID := time.Now().UnixNano() // 每次用独立“租户”，不污染已有数据
	rt := NewRedisTPM(rdb)

	// 1. 预扣 60，limit=100 → 放行，已用 60
	res, d, err := rt.Reserve(ctx, tenantID, 60, 100)
	if err != nil {
		t.Fatalf("reserve 1: %v", err)
	}
	if !d.Allowed {
		t.Fatalf("第一次预扣应放行, used=%d", d.Used)
	}
	if res.Reserved != 60 {
		t.Fatalf("第一次应预扣 60, got=%d", res.Reserved)
	}
	defer rdb.Del(ctx, res.Key)

	// 2. 再预扣 60，但只剩 40：额度没用完就放行，按剩余额度截断预扣
	res2, d2, err := rt.Reserve(ctx, tenantID, 60, 100)
	if err != nil {
		t.Fatalf("reserve 2: %v", err)
	}
	if !d2.Allowed {
		t.Fatal("窗口还有额度时不应拒绝")
	}
	if res2.Reserved != 40 || d2.Used != 100 {
		t.Fatalf("应按剩余 40 预扣并把窗口占满, reserved=%d used=%d", res2.Reserved, d2.Used)
	}

	// 3. 已用满 → 拒绝，并给 RetryAfter
	_, d3, err := rt.Reserve(ctx, tenantID, 1, 100)
	if err != nil {
		t.Fatalf("reserve 3: %v", err)
	}
	if d3.Allowed {
		t.Fatal("窗口用满后应被拒")
	}
	if d3.RetryAfter <= 0 {
		t.Fatal("拒绝时必须给 RetryAfter")
	}

	// 4. 第一笔实际只用了 20，退还 40 → 已用 60
	if err := rt.Settle(ctx, res, 20); err != nil {
		t.Fatalf("settle: %v", err)
	}

	// 5. 退款后还剩 40，再预扣 40 → 100，放行
	_, d5, err := rt.Reserve(ctx, tenantID, 40, 100)
	if err != nil {
		t.Fatalf("reserve 5: %v", err)
	}
	if !d5.Allowed {
		t.Fatalf("退款后应放行, used=%d", d5.Used)
	}
}

// TestRedisTPMUsageIntegration 验证用量查询与限流器读到的是同一个窗口 / 同一个计数。
func TestRedisTPMUsageIntegration(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR 未设置，跳过 Redis 集成测试")
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: os.Getenv("REDIS_PASSWORD"),
	})
	defer rdb.Close()

	ctx := context.Background()
	tenantID := time.Now().UnixNano()
	rt := NewRedisTPMWithWindow(rdb, 5*time.Second)

	// 测试会临时改写全局窗口配置，结束后恢复原值（本地开发时可能正跑着 gateway）。
	old, oldErr := rdb.Get(ctx, windowConfigKey).Result()
	if oldErr != nil && oldErr != redis.Nil {
		t.Fatalf("read old window config: %v", oldErr)
	}
	defer func() {
		if oldErr == redis.Nil || old == "" {
			rdb.Del(context.Background(), windowConfigKey)
			return
		}
		rdb.Set(context.Background(), windowConfigKey, old, 0)
	}()

	if err := rt.PublishWindow(ctx); err != nil {
		t.Fatalf("publish window: %v", err)
	}

	res, d, err := rt.Reserve(ctx, tenantID, 40, 100)
	if err != nil || !d.Allowed {
		t.Fatalf("reserve: err=%v decision=%+v", err, d)
	}
	defer rdb.Del(ctx, res.Key)

	if err := rt.Settle(ctx, res, 25); err != nil {
		t.Fatalf("settle: %v", err)
	}

	u, err := rt.Usage(ctx, tenantID)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if u.Used != 25 {
		t.Fatalf("usage 应读到结算后的 25, got=%d", u.Used)
	}
	if u.Window != 5*time.Second {
		t.Fatalf("usage 应用 Redis 发布的窗口 5s, got=%s", u.Window)
	}
	if !u.WindowStart.Before(u.WindowEnd) {
		t.Fatalf("窗口边界不合法: %s..%s", u.WindowStart, u.WindowEnd)
	}
}
