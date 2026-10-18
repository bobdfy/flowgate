package ratelimit

import (
	"context"
	"testing"
	"time"
)

// fakePairLimiter 模拟一个支持 AllowPair 的 primary，用于测 FallbackLimiter 的降级分支。
type fakePairLimiter struct {
	ok      bool
	blocked int
	err     error
}

func (f *fakePairLimiter) Allow(ctx context.Context, key string, limit int64) (bool, error) {
	return f.ok, f.err
}

func (f *fakePairLimiter) AllowPair(ctx context.Context, k1 string, l1 int64, k2 string, l2 int64) (bool, int, error) {
	return f.ok, f.blocked, f.err
}

func (f *fakePairLimiter) RetryAfter(key string, limit int64) time.Duration {
	return time.Second
}

// TestFallbackAllowPairPrimaryOK 验证 primary 正常时结果透传。
func TestFallbackAllowPairPrimaryOK(t *testing.T) {
	p := &fakePairLimiter{ok: true, blocked: 0}
	f := NewFallbackLimiter(p, NewLocalLimiter(100), FailFallback)

	ok, blocked, err := f.AllowPair(context.Background(), "k1", 10, "k2", 20)
	if err != nil {
		t.Fatalf("不应有 error: %v", err)
	}
	if !ok || blocked != 0 {
		t.Fatalf("期望 (true, 0)，实际 (%v, %d)", ok, blocked)
	}
}

// TestFallbackAllowPairPrimaryBlocked 验证 primary 返回超限维度时透传 blocked。
func TestFallbackAllowPairPrimaryBlocked(t *testing.T) {
	p := &fakePairLimiter{ok: false, blocked: 2}
	f := NewFallbackLimiter(p, NewLocalLimiter(100), FailFallback)

	ok, blocked, err := f.AllowPair(context.Background(), "k1", 10, "k2", 20)
	if err != nil {
		t.Fatalf("不应有 error: %v", err)
	}
	if ok || blocked != 2 {
		t.Fatalf("期望 (false, 2)，实际 (%v, %d)", ok, blocked)
	}
}

// TestFallbackAllowPairFailClosed 验证 Redis 故障 + closed 模式 → 直接拒绝。
func TestFallbackAllowPairFailClosed(t *testing.T) {
	p := &fakePairLimiter{err: context.DeadlineExceeded}
	f := NewFallbackLimiter(p, NewLocalLimiter(100), FailClosed)

	ok, _, err := f.AllowPair(context.Background(), "k1", 10, "k2", 20)
	if err != nil {
		t.Fatalf("closed 模式不应返回 error: %v", err)
	}
	if ok {
		t.Fatal("closed 模式应拒绝")
	}
}

// TestFallbackAllowPairFailOpen 验证 Redis 故障 + open 模式 → 放行。
func TestFallbackAllowPairFailOpen(t *testing.T) {
	p := &fakePairLimiter{err: context.DeadlineExceeded}
	f := NewFallbackLimiter(p, NewLocalLimiter(100), FailOpen)

	ok, _, err := f.AllowPair(context.Background(), "k1", 10, "k2", 20)
	if err != nil {
		t.Fatalf("open 模式不应返回 error: %v", err)
	}
	if !ok {
		t.Fatal("open 模式应放行")
	}
}

// TestFallbackAllowPairFailFallbackLocal 验证 Redis 故障 + fallback 模式 → 本地令牌桶逐 key 检查。
// burst=1 使桶容量为 1：同一 key 第一次放行后，第二次应超限（blocked=1，租户维度先判）。
func TestFallbackAllowPairFailFallbackLocal(t *testing.T) {
	p := &fakePairLimiter{err: context.DeadlineExceeded}
	f := NewFallbackLimiter(p, NewLocalLimiter(1), FailFallback)

	// 第一次：本地桶有令牌，放行
	ok, _, err := f.AllowPair(context.Background(), "k1", 1, "k2", 100)
	if err != nil || !ok {
		t.Fatalf("第一次应放行: ok=%v err=%v", ok, err)
	}
	// 第二次：k1 已耗尽，blocked 应为 1
	ok, blocked, err := f.AllowPair(context.Background(), "k1", 1, "k2", 100)
	if err != nil {
		t.Fatalf("不应有 error: %v", err)
	}
	if ok || blocked != 1 {
		t.Fatalf("期望 (false, 1)，实际 (%v, %d)", ok, blocked)
	}
}
