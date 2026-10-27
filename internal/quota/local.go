package quota

import (
	"context"
	"sync"
	"time"
)

// localSweepEvery 每多少次预扣清理一次过期窗口。
const localSweepEvery = 256

// LocalTPM 把 TPM 用量记在进程内存里。
//
//	这是近似值，不是全局配额：多实例部署时每个实例各记各的，
//
// 全局最多能放过 实例数 × limit 个 token。Redis 挂掉时在「完全不管」和
// 「全部拒绝」之间取中间态，代价要在日志/指标里说清楚。
type LocalTPM struct {
	mu     sync.Mutex
	window time.Duration
	cur    map[string]*localWindow // 窗口 key → 当窗用量
	ops    int                     // 清理计数器
	now    func() time.Time
}

type localWindow struct {
	start time.Time
	used  int64
}

var _ Limiter = (*LocalTPM)(nil)

// NewLocalTPM 创建本地配额。window <= 0 回落到 1 分钟，小于 1 秒按 1 秒处理，
func NewLocalTPM(window time.Duration) *LocalTPM {
	return &LocalTPM{
		window: normalizeWindow(window),
		cur:    make(map[string]*localWindow),
		now:    time.Now,
	}
}

// Window 返回当前窗口长度，与 RedisTPM.Window 对齐，便于降级时打日志
func (l *LocalTPM) Window() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.window
}

// Reserve 在转发前预扣 need 个 token，语义与 RedisTPM.Reserve 逐条对齐
func (l *LocalTPM) Reserve(ctx context.Context, tenantID, need, limit int64) (Reservation, Decision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	start := windowStart(l.window, now)
	key := windowKeyFor(tenantID, l.window, now)

	// 未设限额：不记账，直接放行（与 Redis 版一致）
	if limit <= 0 {
		return Reservation{Key: key, Reserved: need, Limit: limit},
			Decision{Allowed: true, Limit: limit}, nil
	}
	if need < 1 {
		need = 1
	}

	l.sweep(start)

	w := l.cur[key]
	if w == nil {
		w = &localWindow{start: start}
		l.cur[key] = w
	}

	if w.used >= limit {
		retry := start.Add(l.window).Sub(now)
		if retry <= 0 {
			retry = time.Second
		}
		// 被拒绝的预扣不会被结算，Reserved 保持 0
		return Reservation{Key: key, Limit: limit},
			Decision{Allowed: false, Used: w.used, Limit: limit, RetryAfter: retry}, nil
	}

	// 剩余额度不够 need 时只扣剩余，把窗口占满即可，
	// 不要因为「额度没用完却报已用尽」而整单拒绝。
	charge := need
	if remaining := limit - w.used; charge > remaining {
		charge = remaining
	}
	w.used += charge

	return Reservation{Key: key, Reserved: charge, Limit: limit},
		Decision{Allowed: true, Used: w.used, Limit: limit}, nil
}

// Settle 用真实用量多退少补。key 里带着窗口序号，
// 跨窗结算会落回预扣时的那个窗口，不会污染新窗口。
func (l *LocalTPM) Settle(ctx context.Context, r Reservation, actual int64) error {
	if r.Limit <= 0 {
		return nil
	}
	if actual < 0 {
		actual = 0
	}
	delta := actual - r.Reserved
	if delta == 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	w := l.cur[r.Key]
	if w == nil {
		// 窗口已被清理（请求跨了好几个窗口才结算，或进程重启过）。
		// 没有可退的本地账，放弃这次结算。
		return nil
	}
	w.used += delta
	if w.used < 0 {
		w.used = 0
	}
	return nil
}

// sweep 周期性丢掉两个窗口以前的记录，避免 map 随租户数和时间无限增长。
func (l *LocalTPM) sweep(now time.Time) {
	l.ops++
	if l.ops%localSweepEvery != 0 {
		return
	}
	stale := 2 * l.window
	for k, w := range l.cur {
		if now.Sub(w.start) >= stale {
			delete(l.cur, k)
		}
	}
}

// normalizeWindow 统一窗口取值规则，RedisTPM.SetWindow 用的是同一套。
func normalizeWindow(window time.Duration) time.Duration {
	if window <= 0 {
		return time.Minute
	}
	if window < time.Second {
		return time.Second
	}
	return window
}
