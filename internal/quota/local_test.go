package quota

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeClock 让本地配额的窗口判断可以手动推进，避免测试真的等窗口过去。
type fakeClock struct{ t time.Time }

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func newLocalTPMForTest(window time.Duration) (*LocalTPM, *fakeClock) {
	clk := newFakeClock()
	l := NewLocalTPM(window)
	l.now = clk.Now
	return l, clk
}

// 剩余额度不够 need 时只扣剩余：预扣是估算，不该因为估多了就整单拒绝。
func TestLocalTPMReserveTruncatesToRemaining(t *testing.T) {
	l, _ := newLocalTPMForTest(time.Minute)
	ctx := context.Background()

	r1, d1, err := l.Reserve(ctx, 1, 30, 100)
	if err != nil || !d1.Allowed || d1.Used != 30 || r1.Reserved != 30 {
		t.Fatalf("第一次预扣: err=%v allowed=%v used=%d reserved=%d", err, d1.Allowed, d1.Used, r1.Reserved)
	}

	_, d2, _ := l.Reserve(ctx, 1, 30, 100)
	if !d2.Allowed || d2.Used != 60 {
		t.Fatalf("第二次预扣: allowed=%v used=%d", d2.Allowed, d2.Used)
	}

	// 只剩 40，need=60 → 扣 40，窗口刚好占满。
	r3, d3, _ := l.Reserve(ctx, 1, 60, 100)
	if !d3.Allowed || d3.Used != 100 || r3.Reserved != 40 {
		t.Fatalf("截断预扣: allowed=%v used=%d reserved=%d", d3.Allowed, d3.Used, r3.Reserved)
	}

	// 额度已满 → 拒绝。
	_, d4, _ := l.Reserve(ctx, 1, 1, 100)
	if d4.Allowed {
		t.Fatalf("额度已满却放行, used=%d", d4.Used)
	}
}

// 拒绝时必须给出可用的 Retry-After，否则调用方没法回 429 的 Retry-After 头。
func TestLocalTPMDenySetsRetryAfter(t *testing.T) {
	l, clk := newLocalTPMForTest(time.Minute)
	ctx := context.Background()

	l.Reserve(ctx, 1, 100, 100)
	clk.Advance(15 * time.Second)

	_, d, _ := l.Reserve(ctx, 1, 1, 100)
	if d.Allowed {
		t.Fatal("额度已满却放行")
	}
	if d.RetryAfter <= 0 || d.RetryAfter > time.Minute {
		t.Fatalf("RetryAfter 越界: %v", d.RetryAfter)
	}
	if want := 45 * time.Second; d.RetryAfter != want {
		t.Fatalf("RetryAfter = %v, want %v", d.RetryAfter, want)
	}
}

// 窗口切换后用量归零。
func TestLocalTPMWindowReset(t *testing.T) {
	l, clk := newLocalTPMForTest(time.Minute)
	ctx := context.Background()

	l.Reserve(ctx, 1, 100, 100)
	if _, d, _ := l.Reserve(ctx, 1, 1, 100); d.Allowed {
		t.Fatal("本窗口额度应已用尽")
	}

	clk.Advance(61 * time.Second)

	_, d, _ := l.Reserve(ctx, 1, 100, 100)
	if !d.Allowed || d.Used != 100 {
		t.Fatalf("新窗口应重新计数: allowed=%v used=%d", d.Allowed, d.Used)
	}
}

// 不同租户互不影响。
func TestLocalTPMIsolatesTenants(t *testing.T) {
	l, _ := newLocalTPMForTest(time.Minute)
	ctx := context.Background()

	l.Reserve(ctx, 1, 100, 100)
	if _, d, _ := l.Reserve(ctx, 2, 100, 100); !d.Allowed {
		t.Fatal("租户 2 不应受租户 1 影响")
	}
}

// 结算按真实用量多退少补。
func TestLocalTPMSettleAdjustsUsage(t *testing.T) {
	l, _ := newLocalTPMForTest(time.Minute)
	ctx := context.Background()

	r, d, _ := l.Reserve(ctx, 1, 100, 1000)
	if !d.Allowed || d.Used != 100 {
		t.Fatalf("预扣: allowed=%v used=%d", d.Allowed, d.Used)
	}

	// 实际只用了 40，应退 60。
	if err := l.Settle(ctx, r, 40); err != nil {
		t.Fatalf("结算失败: %v", err)
	}

	_, d2, _ := l.Reserve(ctx, 1, 60, 1000)
	if d2.Used != 100 {
		t.Fatalf("退款后用量应为 40+60=100, got %d", d2.Used)
	}
}

// 实际用量超过预扣时补扣；退还超过已用时不把用量减成负数。
func TestLocalTPMSettleFloorsAtZero(t *testing.T) {
	l, _ := newLocalTPMForTest(time.Minute)
	ctx := context.Background()

	r, _, _ := l.Reserve(ctx, 1, 10, 100)
	if err := l.Settle(ctx, r, 0); err != nil { // 退 10
		t.Fatalf("结算失败: %v", err)
	}

	// 再退一次不应把用量减成负数；用量已被前面清空，这里 key 仍存在。
	if err := l.Settle(ctx, r, 0); err != nil {
		t.Fatalf("重复结算不应报错: %v", err)
	}

	_, d, _ := l.Reserve(ctx, 1, 1, 100)
	if !d.Allowed || d.Used != 1 {
		t.Fatalf("用量应回到 0 再扣 1, allowed=%v used=%d", d.Allowed, d.Used)
	}
}

// limit <= 0 表示不限额，永远放行且不记账。
func TestLocalTPMLimitZeroAlwaysAllowed(t *testing.T) {
	l, _ := newLocalTPMForTest(time.Minute)
	ctx := context.Background()

	for i := 0; i < 1000; i++ {
		_, d, _ := l.Reserve(ctx, 1, 1_000_000, 0)
		if !d.Allowed {
			t.Fatalf("未设限额却拒绝, 第 %d 次", i)
		}
	}
}

// 结算的窗口已被清理（或进程重启）时静默放弃，不能 panic 也不能记到别的窗口。
func TestLocalTPMSettleUnknownKeyIsNoop(t *testing.T) {
	l, _ := newLocalTPMForTest(time.Minute)
	ctx := context.Background()

	r := Reservation{Key: "tpm:1:999", Reserved: 10, Limit: 100}
	if err := l.Settle(ctx, r, 5); err != nil {
		t.Fatalf("未知窗口的结算不应报错: %v", err)
	}
}

// 清理只丢掉两个窗口以前的记录。
func TestLocalTPMSweepDropsStaleWindows(t *testing.T) {
	l, clk := newLocalTPMForTest(time.Minute)
	ctx := context.Background()

	l.Reserve(ctx, 1, 1, 100)
	oldKey := windowKeyFor(1, time.Minute, clk.Now())

	clk.Advance(3 * time.Minute)
	l.ops = localSweepEvery - 1 // 让下一次 Reserve 触发清理
	l.Reserve(ctx, 2, 1, 100)

	if _, ok := l.cur[oldKey]; ok {
		t.Fatal("两个窗口以前的记录应被清理")
	}
	if len(l.cur) != 1 {
		t.Fatalf("应只剩当前窗口的记录, got %d", len(l.cur))
	}
}

// 并发预扣不能超发：limit 就是硬上限。
func TestLocalTPMConcurrentReserveNeverExceedsLimit(t *testing.T) {
	l, _ := newLocalTPMForTest(time.Minute)
	ctx := context.Background()

	const (
		limit      = 50
		goroutines = 32
		perG       = 100
	)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allowed int
	)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				_, d, _ := l.Reserve(ctx, 1, 1, limit)
				if d.Allowed {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	if allowed != limit {
		t.Fatalf("放行次数应为 %d, got %d", limit, allowed)
	}
}
