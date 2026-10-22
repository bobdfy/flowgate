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

	// 1. 预扣 60，limit=100 → 放行
	res, d, err := rt.Reserve(ctx, tenantID, 60, 100)
	if err != nil {
		t.Fatalf("reserve 1: %v", err)
	}
	if !d.Allowed {
		t.Fatalf("第一次预扣应放行, used=%d", d.Used)
	}
	defer rdb.Del(ctx, res.Key)

	// 2. 再预扣 60 → 60+60 > 100，拒绝
	_, d2, err := rt.Reserve(ctx, tenantID, 60, 100)
	if err != nil {
		t.Fatalf("reserve 2: %v", err)
	}
	if d2.Allowed {
		t.Fatal("第二次预扣应被拒")
	}
	if d2.RetryAfter <= 0 {
		t.Fatal("拒绝时必须给 RetryAfter")
	}

	// 3. 实际只用了 20，退还 40
	if err := rt.Settle(ctx, res, 20); err != nil {
		t.Fatalf("settle: %v", err)
	}

	// 4. 退款后窗口内已用 20，再预扣 60 → 80 <= 100，放行
	_, d3, err := rt.Reserve(ctx, tenantID, 60, 100)
	if err != nil {
		t.Fatalf("reserve 3: %v", err)
	}
	if !d3.Allowed {
		t.Fatalf("退款后应放行, used=%d", d3.Used)
	}
}
