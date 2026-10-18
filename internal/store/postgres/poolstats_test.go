package postgres

import (
	"testing"
	"time"
)

// 本文件覆盖 StartPoolStats 里唯一的「逻辑」部分：累计量的增量计算与上报。
//
// 为什么只测这些：pgxpool.Stat 是真实连接池的快照，没有 DB 就构造不出来，
// 所以采样主循环（需要真池）不进单元测试；而"两次快照做差"是纯函数，
// 且它承载着一条硬性约束 —— 传给 prometheus Counter 的值不能为负，
// 否则 Counter.Add 会 panic，而 panic 发生在采样 goroutine 里会带崩整个数据面。
// 这条约束必须有测试兜住。

func TestSnapshotDelta(t *testing.T) {
	base := poolSnapshot{
		acquireCount:         100,
		emptyAcquireCount:    3,
		canceledAcquireCount: 1,
		newConnsCount:        50,
		lifetimeDestroyCount: 2,
		idleDestroyCount:     4,
		acquireDuration:      2 * time.Second,
		emptyAcquireWaitTime: 500 * time.Millisecond,
	}
	cur := poolSnapshot{
		acquireCount:         150,                    // +50
		emptyAcquireCount:    3,                      // +0
		canceledAcquireCount: 5,                      // +4
		newConnsCount:        51,                     // +1
		lifetimeDestroyCount: 2,                      // +0
		idleDestroyCount:     9,                      // +5
		acquireDuration:      3 * time.Second,        // +1s
		emptyAcquireWaitTime: 500 * time.Millisecond, // +0
	}

	d := base.deltaSince(cur)

	if d.acquireCount != 50 {
		t.Errorf("acquireCount 增量 = %d，期望 50", d.acquireCount)
	}
	if d.emptyAcquireCount != 0 {
		t.Errorf("emptyAcquireCount 增量 = %d，期望 0", d.emptyAcquireCount)
	}
	if d.canceledAcquireCount != 4 {
		t.Errorf("canceledAcquireCount 增量 = %d，期望 4", d.canceledAcquireCount)
	}
	if d.newConnsCount != 1 {
		t.Errorf("newConnsCount 增量 = %d，期望 1", d.newConnsCount)
	}
	if d.idleDestroyCount != 5 {
		t.Errorf("idleDestroyCount 增量 = %d，期望 5", d.idleDestroyCount)
	}
	if d.acquireDuration != time.Second {
		t.Errorf("acquireDuration 增量 = %v，期望 1s", d.acquireDuration)
	}
	if d.emptyAcquireWaitTime != 0 {
		t.Errorf("emptyAcquireWaitTime 增量 = %v，期望 0", d.emptyAcquireWaitTime)
	}
}

// TestSnapshotDelta_ClampNegative 是这条代码里最重要的一条测试。
//
// 场景：理论上 pgxpool 的计数器单调递增，但连接池被重建、或 Stat 内部
// 计数回绕时，差值可能为负。prometheus 的 Counter.Add 收到负数会直接 panic ——
// 而这段代码跑在后台采样 goroutine 里，panic 会拖垮整个网关进程。
// 所以宁可丢弃一次增量（钳成 0），也不能让观测代码成为新的故障源。
func TestSnapshotDelta_ClampNegative(t *testing.T) {
	base := poolSnapshot{acquireCount: 100, newConnsCount: 50, acquireDuration: 5 * time.Second}
	cur := poolSnapshot{acquireCount: 80, newConnsCount: 10, acquireDuration: time.Second}

	d := base.deltaSince(cur)
	if d.acquireCount != 0 || d.newConnsCount != 0 || d.acquireDuration != 0 {
		t.Fatalf("负增量没有被钳成 0：acquire=%d new=%d dur=%v",
			d.acquireCount, d.newConnsCount, d.acquireDuration)
	}
}

func TestSnapshotDelta_FirstSample(t *testing.T) {
	// 首帧基线：全 0 的 base 与真实快照做差，应当等于快照本身
	var zero poolSnapshot
	cur := poolSnapshot{acquireCount: 7, newConnsCount: 2, acquireDuration: 100 * time.Millisecond}
	d := zero.deltaSince(cur)
	if d.acquireCount != 7 || d.newConnsCount != 2 || d.acquireDuration != 100*time.Millisecond {
		t.Fatalf("首帧增量应为快照本身，得到 %+v", d)
	}
}

func TestClampDelta(t *testing.T) {
	cases := []struct {
		base, cur, want int64
	}{
		{0, 0, 0},
		{0, 5, 5},
		{5, 5, 0},
		{5, 3, 0}, // 回退 → 0
		{5, 100, 95},
	}
	for _, c := range cases {
		if got := clampDelta(c.base, c.cur); got != c.want {
			t.Errorf("clampDelta(%d, %d) = %d，期望 %d", c.base, c.cur, got, c.want)
		}
	}
}

// TestReportCounters_NoPanic 用真实（全局）的 prometheus Counter 跑一遍上报，
// 确保"增量永不为负"这条契约在完整链路上成立。
// Counter.Add 遇负数会 panic，所以这条用例只要不崩就是过。
func TestReportCounters_NoPanic(t *testing.T) {
	// 全 0：什么都不该加
	reportCounters(poolSnapshot{})

	// 正常增量
	reportCounters(poolSnapshot{
		acquireCount:         10,
		emptyAcquireCount:    2,
		canceledAcquireCount: 1,
		newConnsCount:        3,
		lifetimeDestroyCount: 1,
		idleDestroyCount:     1,
		acquireDuration:      200 * time.Millisecond,
		emptyAcquireWaitTime: 50 * time.Millisecond,
	})

	// 极端情况：即使某处漏了钳制逻辑，这里也必须不崩（负增量应已被上游丢弃）
	reportCounters(poolSnapshot{}.deltaSince(poolSnapshot{acquireCount: 1}))
}
