package overload

import (
	"context"
	"testing"
	"time"
)

func TestGuard(t *testing.T) {
	g := NewGuard(2, StrategyFailFast, 0, 0)
	ctx := context.Background()

	r1, e1 := g.Acquire(ctx)
	r2, e2 := g.Acquire(ctx)
	r3, e3 := g.Acquire(ctx)
	t.Logf("拿3个: e1=%v e2=%v e3=%v", e1, e2, e3)

	// ★ 还回去 1 个,应该能再拿 1 个
	r1()
	r4, e4 := g.Acquire(ctx)
	t.Logf("还1个后再拿: e4=%v (期望 nil)", e4)

	// 现在盒子里应该有 2 个:r2 和 r4。第 3 个还是应该被拒
	_, e5 := g.Acquire(ctx)
	t.Logf("再拿第3个: e5=%v (期望 fail_fast)", e5)

	s := g.Stats()
	t.Logf("Stats = %+v", s)

	if s.Rejected != 2 {
		t.Errorf("Rejected 期望 2,实际 %d", s.Rejected)
	}
	if s.InFlight != 2 {
		t.Errorf("InFlight 期望 2,实际 %d", s.InFlight)
	}

	// ★ 同一个 release 调两次,不能卡死
	r4()
	r4()
	t.Logf("release 调两次: 没卡死 ✓")

	r2()
	_ = r3

}

func TestGuardTimeout(t *testing.T) {
	g := NewGuard(1, StrategyWait, 100*time.Millisecond, 0)
	ctx := context.Background()

	r1, _ := g.Acquire(ctx)

	start := time.Now()
	_, err := g.Acquire(ctx)
	elapsed := time.Since(start)

	t.Logf("等待耗时 = %v", elapsed)
	t.Logf("err = %v", err)

	if elapsed < 90*time.Millisecond {
		t.Errorf("★ 没等就拒了 —— 说明 select 里还有 default")
	}
	r1()
}

func TestGuardDrop(t *testing.T) {
	// ★ 5 秒:远远大于这个测试本身的耗时(几百微秒)
	//   这样 r2 在整个测试期间一定还在等,等候区一定占着
	g := NewGuard(1, StrategyDrop, 5*time.Second, 1)
	ctx := context.Background()

	r1, _ := g.Acquire(ctx) // 占掉唯一的干活位子

	// 这个 Acquire 会一直阻塞(要等 5 秒),所以放进 goroutine
	go g.Acquire(ctx) // ← r2:抢到等候区唯一的座位,一直占着

	// ★ 等一小会儿,确保上面那个 goroutine 真的跑起来了、抢到座位了
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	_, err := g.Acquire(ctx) // ← r3:等候区满了,应该立刻拒
	elapsed := time.Since(start)

	t.Logf("耗时 = %v (期望接近 0)", elapsed)
	t.Logf("err = %v (期望 queue_full)", err)

	if elapsed > 100*time.Millisecond {
		t.Errorf("★ 它等了 —— 说明抢座位那个 select 少了 default")
	}
	r1()
}
