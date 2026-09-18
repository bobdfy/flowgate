// Package overload 提供并发保护（过载保护 / Bulkhead）的通用机制。
package overload

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Strategy 过载是的处置策略
type Strategy string

const (
	// 并发满了直接拒绝
	StrategyFailFast = "fail-fast"

	// 并发满时先排队等待， 超时queueTimeout 才拒绝
	StrategyWait = "wait"

	// 并发满且排队队列也满时才立即拒绝
	StrategyDrop = "drop"
)

type RejectReason string

const (
	ReasonFailFast     RejectReason = "fail_fast"
	ReasonQueueFull    RejectReason = "queue_full"
	ReasonQueueTimeout RejectReason = "queue_timeout"
	ReasonDropTimeout  RejectReason = "queue_timeout_drop"
)

type RejectedError struct {
	// Reason 说明被拒的原因。★ 必须是枚举值，会被当作 Prometheus 的 label ——
	// 绝不能塞 service 名 / path / key 之类的动态值进去。
	Reason RejectReason

	// 重试的等待时间
	RetryAfter time.Duration

	// InFlight 是拒绝瞬间的在途请求数，排障用（写日志时带上）。
	InFlight int
}

func (e *RejectedError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("overload: rejects (reason = %s, in_flight = %d, retry_after = %s)", e.Reason, e.InFlight, e.RetryAfter)
	} else {
		return fmt.Sprintf("overload: rejects (reason = %s, inflight = %d)", e.Reason, e.InFlight)
	}
}

// 护栏的运行时的观测值
type Stats struct {
	InFlight int   // 当前在途请求数
	Queued   int   // 当前排队的请求数
	Rejected int64 // 累计被拒绝数
}

type Guard struct {
	maxConcurrency int
	strategy       Strategy
	queueTimeout   time.Duration
	queueCapacity  int

	// 并发名额的容器, 容量为 maxConcurrency
	boxes chan struct{}

	inFlight atomic.Int64

	// queue 是 drop 策略的等候区, 等着干活的位子
	queue chan struct{}

	// rejected 是累计被拒数,只增不减。★ 用 atomic 因为多 goroutine 会同时加。
	rejected atomic.Int64

	// 待生效的上限
	incoming atomic.Int64

	swapMu sync.Mutex
}

func NewGuard(maxConcurrency int, strategy Strategy, queueTimeout time.Duration, queueCapacity int) *Guard {
	if queueCapacity <= 0 {
		queueCapacity = DefaultQueueCapacity(maxConcurrency)
	}
	g := &Guard{
		maxConcurrency: maxConcurrency,
		strategy:       strategy,
		queueTimeout:   queueTimeout,
		queueCapacity:  queueCapacity,
		boxes:          make(chan struct{}, maxConcurrency),
		queue:          make(chan struct{}, queueCapacity),
	}
	g.incoming.Store(int64(maxConcurrency))
	return g
}

func (g *Guard) Acquire(ctx context.Context) (release func(), err error) {
	if g.maxConcurrency <= 0 {
		return func() {}, nil
	}

	var released bool

	releaseOnce := func() {
		if released {
			return
		}
		released = true
		<-g.boxes
		g.inFlight.Add(-1)
		g.applyIncomingLimit()
	}

	select {
	case g.boxes <- struct{}{}:
		g.inFlight.Add(1)
		return releaseOnce, nil
	default:
		// ★ 新增:盒子满了,但可能"配置刚缩小、新盒子还没换上去"。
		// 这时旧盒子还有空位,不能只看它 —— 还要看 incoming。
		// 这条判断和策略无关,所以放在 switch 之前,只写一遍。
		if g.incoming.Load() < int64(cap(g.boxes)) && g.inFlight.Load() >= g.incoming.Load() {
			g.rejected.Add(1)
			return nil, &RejectedError{
				Reason:   ReasonFailFast,
				InFlight: int(g.inFlight.Load()),
			}
		}

		switch g.strategy {
		case StrategyWait:

			return g.waitForSlot(ctx, releaseOnce, func() error {
				g.rejected.Add(1)
				return &RejectedError{
					Reason:     ReasonQueueTimeout,
					RetryAfter: g.queueTimeout,
					InFlight:   int(g.inFlight.Load()),
				}
			})
		case StrategyDrop:
			select {
			case g.queue <- struct{}{}:
			default:
				g.rejected.Add(1)
				return nil, &RejectedError{
					Reason:   ReasonQueueFull,
					InFlight: int(g.inFlight.Load()),
				}
			}
			defer func() {
				<-g.queue
			}()

			return g.waitForSlot(ctx, releaseOnce, func() error {
				g.rejected.Add(1)
				return &RejectedError{
					Reason:     ReasonDropTimeout,
					RetryAfter: g.queueTimeout,
					InFlight:   int(g.inFlight.Load()),
				}
			})
		default:
			g.rejected.Add(1)
			return nil, &RejectedError{Reason: ReasonFailFast, InFlight: int(g.inFlight.Load())}
		}
	}
}

func (g *Guard) waitForSlot(ctx context.Context, releaseOnce func(), onTimeout func() error) (func(), error) {
	timer := time.NewTimer(g.queueTimeout)
	defer timer.Stop()

	select {
	case g.boxes <- struct{}{}:
		g.inFlight.Add(1)
		return releaseOnce, nil
	case <-timer.C:
		return nil, onTimeout()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// DefaultQueueCapacity 是 drop 策略下 queueCapacity 传 0 时使用的默认值。
func DefaultQueueCapacity(maxConcurrency int) int {
	// 0 表示全部拒绝"——
	if maxConcurrency <= 0 {
		return 0
	}
	return maxConcurrency * 2
}

// SetMaxConcurrency 热更新并发上限（配置刷新时调用）。
//
// ★ 缩小上限时不会中断在途请求 —— 让它们跑完,只是不再放新的进来。
//
// ★ 因此换盒子有个延迟生效的规则:
//
// 等 InFlight 归零时再换
func (g *Guard) SetMaxConcurrency(n int) {
	// ★ n = 0 的语义是"拒绝所有请求"(不是"不限流")。
	//   boxes 容量为 0 时发送永远阻塞,所以所有请求都会走 fast-fail 拒绝。
	//   "不限流"只能用 NewGuard(0, ...) 表示,不能靠热更新得到。
	g.incoming.Store(int64(n))
	g.applyIncomingLimit()
}

// applyIncomingLimit 在安全的时候把 incoming 应用到 boxes 上。
func (g *Guard) applyIncomingLimit() {
	want := int(g.incoming.Load())

	g.swapMu.Lock()
	defer g.swapMu.Unlock()

	if want == cap(g.boxes) {
		return
	}

	if g.inFlight.Load() != 0 {
		return
	}
	g.boxes = make(chan struct{}, want)
}

func (g *Guard) Stats() Stats {
	return Stats{
		InFlight: int(g.inFlight.Load()),
		Queued:   len(g.queue),
		Rejected: g.rejected.Load(),
	}
}
