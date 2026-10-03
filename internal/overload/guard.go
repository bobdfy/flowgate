// Package overload 提供并发保护（过载保护 / Bulkhead）的通用机制。
package overload

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// Strategy 过载时的处置策略
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
	// Reason 说明被拒的原因。 必须是枚举值，会被当作 Prometheus 的 label ——
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

	// rejected 是累计被拒数,只增不减。用 atomic 因为多 goroutine 会同时加。
	rejected atomic.Int64
}

// 并发护栏
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
	return g
}

// Acquire 尝试获取一个并发名额：成功返回幂等的 release(用完必须调用)，失败返回带拒绝原因的 RejectedError。
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
	}

	select {
	case g.boxes <- struct{}{}:
		g.inFlight.Add(1)
		return releaseOnce, nil
	default:
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

func (g *Guard) Stats() Stats {
	return Stats{
		InFlight: int(g.inFlight.Load()),
		Queued:   len(g.queue),
		Rejected: g.rejected.Load(),
	}
}
