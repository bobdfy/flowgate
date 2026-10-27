package quota

import (
	"context"
	"testing"
	"time"
)

// TestWindowStartAligns 验证 windowStart 与 windowKeyFor 使用同一个窗口边界，
// 否则用量查询会读到上一个窗口的 key。
func TestWindowStartAligns(t *testing.T) {
	base := time.Unix(1_700_000_000/3600*3600, 0)

	for _, window := range []time.Duration{time.Minute, 5 * time.Minute, time.Hour} {
		start := windowStart(window, base.Add(window-time.Second))
		if !start.Equal(base) {
			t.Fatalf("window=%s: 窗口内任意时刻应回到同一开始点, got=%s want=%s",
				window, start, base)
		}
		next := windowStart(window, base.Add(window))
		if !next.Equal(base.Add(window)) {
			t.Fatalf("window=%s: 跨窗后应进入下一窗口, got=%s", window, next)
		}
	}
}

// TestWindowKeyForMatchesReserve 验证 Usage 用的 key 与 Reserve 完全一致。
func TestWindowKeyForMatchesReserve(t *testing.T) {
	base := time.Unix(1_700_000_000/3600*3600, 0)
	rt := NewRedisTPMWithWindow(nil, 5*time.Minute)

	if got, want := windowKeyFor(42, 5*time.Minute, base), rt.windowKey(42, base); got != want {
		t.Fatalf("用量 key 与预扣 key 不一致: got=%s want=%s", got, want)
	}
}

// TestUsageWithoutRedis Usage 在没注入 Redis 客户端时应返回错误而不是 panic。
func TestUsageWithoutRedis(t *testing.T) {
	rt := NewRedisTPMWithWindow(nil, time.Minute)
	if _, err := rt.Usage(context.Background(), 1); err == nil {
		t.Fatal("rdb 为 nil 时应返回错误")
	}
}
