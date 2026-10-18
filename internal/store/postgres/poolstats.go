package postgres

import (
	"context"
	"log/slog"
	"time"

	"github.com/bobdfy/flowgate/internal/observability"
	"github.com/jackc/pgx/v5/pgxpool"
)

// poolSnapshot 是 pgxpool.Stat 里「累计量」的一份快照。
//
// 为什么只挑这几项：pgxpool.Stat 同时提供瞬时量（当前连接数）和累计量
// （次数 / 耗时）。瞬时量每次直接 Set 到 Gauge 即可；累计量则必须与上一次
// 采样做差，才能用 Counter.Add 上报（Counter 只接受增量，且不允许负值）。
type poolSnapshot struct {
	acquireCount         int64
	emptyAcquireCount    int64
	canceledAcquireCount int64
	newConnsCount        int64
	lifetimeDestroyCount int64
	idleDestroyCount     int64

	acquireDuration      time.Duration
	emptyAcquireWaitTime time.Duration
}

// snapshotOf 从 pgxpool.Stat 里取出关心的累计量。
func snapshotOf(s *pgxpool.Stat) poolSnapshot {
	return poolSnapshot{
		acquireCount:         s.AcquireCount(),
		emptyAcquireCount:    s.EmptyAcquireCount(),
		canceledAcquireCount: s.CanceledAcquireCount(),
		newConnsCount:        s.NewConnsCount(),
		lifetimeDestroyCount: s.MaxLifetimeDestroyCount(),
		idleDestroyCount:     s.MaxIdleDestroyCount(),
		acquireDuration:      s.AcquireDuration(),
		emptyAcquireWaitTime: s.EmptyAcquireWaitTime(),
	}
}

// deltaSince 计算 cur 相对 base 的增量。
//
// 所有字段都做了负数钳制（clamp）。理论上 pgxpool 的这些计数单调递增，
// 差值为负只可能发生在连接池被重建等异常情况下；而 prometheus 的
// Counter.Add 遇到负数会直接 panic —— 在采样 goroutine 里 panic 会带崩整个进程
// 宁可丢掉一次增量，也不能让观测代码把数据面搞挂。
func (base poolSnapshot) deltaSince(cur poolSnapshot) poolSnapshot {
	return poolSnapshot{
		acquireCount:         clampDelta(base.acquireCount, cur.acquireCount),
		emptyAcquireCount:    clampDelta(base.emptyAcquireCount, cur.emptyAcquireCount),
		canceledAcquireCount: clampDelta(base.canceledAcquireCount, cur.canceledAcquireCount),
		newConnsCount:        clampDelta(base.newConnsCount, cur.newConnsCount),
		lifetimeDestroyCount: clampDelta(base.lifetimeDestroyCount, cur.lifetimeDestroyCount),
		idleDestroyCount:     clampDelta(base.idleDestroyCount, cur.idleDestroyCount),

		acquireDuration:      time.Duration(clampDelta(int64(base.acquireDuration), int64(cur.acquireDuration))),
		emptyAcquireWaitTime: time.Duration(clampDelta(int64(base.emptyAcquireWaitTime), int64(cur.emptyAcquireWaitTime))),
	}
}

func clampDelta(base, cur int64) int64 {
	if cur <= base {
		return 0
	}
	return cur - base
}

// reportGauges 上报瞬时量（当前连接数）。
func reportGauges(s *pgxpool.Stat) {
	observability.PGPoolAcquiredConns.Set(float64(s.AcquiredConns()))
	observability.PGPoolIdleConns.Set(float64(s.IdleConns()))
	observability.PGPoolTotalConns.Set(float64(s.TotalConns()))
	observability.PGPoolConstructingConns.Set(float64(s.ConstructingConns()))
	observability.PGPoolMaxConns.Set(float64(s.MaxConns()))
}

// reportCounters 上报累计量的增量。
func reportCounters(d poolSnapshot) {
	if d.acquireCount > 0 {
		observability.PGPoolAcquiresTotal.Add(float64(d.acquireCount))
	}
	if d.emptyAcquireCount > 0 {
		observability.PGPoolEmptyAcquiresTotal.Add(float64(d.emptyAcquireCount))
	}
	if d.canceledAcquireCount > 0 {
		observability.PGPoolCanceledAcquiresTotal.Add(float64(d.canceledAcquireCount))
	}
	if d.newConnsCount > 0 {
		observability.PGPoolNewConnsTotal.Add(float64(d.newConnsCount))
	}
	if d.lifetimeDestroyCount > 0 {
		observability.PGPoolDestroyedLifetimeTotal.Add(float64(d.lifetimeDestroyCount))
	}
	if d.idleDestroyCount > 0 {
		observability.PGPoolDestroyedIdleTotal.Add(float64(d.idleDestroyCount))
	}
	if d.acquireDuration > 0 {
		observability.PGPoolAcquireSecondsTotal.Add(d.acquireDuration.Seconds())
	}
	if d.emptyAcquireWaitTime > 0 {
		observability.PGPoolEmptyAcquireWaitSecondsTotal.Add(d.emptyAcquireWaitTime.Seconds())
	}
}

// StartPoolStats 启动连接池采样：每 interval 读一次 pool.Stat() 并上报到 Prometheus。
//
// 设计要点：
//   - 只在 ctx 取消时退出（跟随进程生命周期），不对外暴露 stop 函数，避免调用方忘记关；
//   - 采样间隔默认 1 秒：劣化发生在秒级，5 秒会漏掉尖峰；而 pool.Stat() 成本极低
//     （一次加锁读 puddle 的计数），1 秒一次可以忽略不计；
//   - 池饱和时额外打一条 WARN 日志（带冷却），因为压测时人看的是日志，
//     等想到去查指标往往已经错过了现场。
//
// 用法（在 main 里）：
//
//	go postgres.StartPoolStats(ctx, db, time.Second)
func StartPoolStats(ctx context.Context, pool *pgxpool.Pool, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 首帧：建立增量基线。此时不做差（否则会把进程启动以来的历史全算进第一轮），
	// 只上报瞬时量。
	stat := pool.Stat()
	reportGauges(stat)
	prev := snapshotOf(stat)

	// 饱和告警冷却：避免池一满就每秒刷日志
	var lastWarn time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stat := pool.Stat()
			reportGauges(stat)

			cur := snapshotOf(stat)
			d := prev.deltaSince(cur)
			prev = cur
			reportCounters(d)

			// ★ 关键判据：池空等待一出现就说明连接数不够（或连接被长时间占着不放）。
			if d.emptyAcquireCount > 0 && time.Since(lastWarn) > 10*time.Second {
				lastWarn = time.Now()
				slog.Warn("pgxpool_saturated",
					"empty_acquires", d.emptyAcquireCount,
					"empty_wait_ms", d.emptyAcquireWaitTime.Milliseconds(),
					"acquired", stat.AcquiredConns(),
					"idle", stat.IdleConns(),
					"max", stat.MaxConns(),
					"new_conns", d.newConnsCount,
				)
			}
		}
	}
}
