package quota

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeLimiter 用来模拟 Redis 正常 / 故障两种状态，并记录调用次数。
type fakeLimiter struct {
	res Reservation
	dec Decision

	reserveErr error
	settleErr  error

	reserveCalls int
	settleCalls  int
	lastActual   int64
}

func (f *fakeLimiter) Reserve(ctx context.Context, tenantID, need, limit int64) (Reservation, Decision, error) {
	f.reserveCalls++
	if f.reserveErr != nil {
		return Reservation{}, Decision{}, f.reserveErr
	}
	return f.res, f.dec, nil
}

func (f *fakeLimiter) Settle(ctx context.Context, r Reservation, actual int64) error {
	f.settleCalls++
	f.lastActual = actual
	return f.settleErr
}

func TestFallbackTPMHealthyPathIsUnchanged(t *testing.T) {
	primary := &fakeLimiter{
		res: Reservation{Key: "k", Reserved: 10, Limit: 100},
		dec: Decision{Allowed: true, Used: 10, Limit: 100},
	}
	f := NewFallbackTPM(primary, NewLocalTPM(time.Minute), FailClosed)

	res, d, err := f.Reserve(context.Background(), 1, 10, 100)
	if err != nil || !d.Allowed || res.Key != "k" || res.Local {
		t.Fatalf("正常路径应原样返回: err=%v allowed=%v key=%q local=%v", err, d.Allowed, res.Key, res.Local)
	}
	if primary.reserveCalls != 1 {
		t.Fatalf("primary 调用次数=%d", primary.reserveCalls)
	}
}

// closed：Redis 故障时拒绝，并让 handler 能通过 errors.Is 认出来。
func TestFallbackTPMClosedReturnsUnavailable(t *testing.T) {
	primary := &fakeLimiter{reserveErr: errors.New("connection refused")}
	f := NewFallbackTPM(primary, NewLocalTPM(time.Minute), FailClosed)

	_, _, err := f.Reserve(context.Background(), 1, 10, 100)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("应返回 ErrUnavailable, got %v", err)
	}
}

// open：放行，但结算不能去碰已经挂掉的 primary。
func TestFallbackTPMOpenAllowsWithoutTouchingPrimary(t *testing.T) {
	primary := &fakeLimiter{reserveErr: errors.New("connection refused")}
	f := NewFallbackTPM(primary, NewLocalTPM(time.Minute), FailOpen)

	res, d, err := f.Reserve(context.Background(), 1, 10, 100)
	if err != nil || !d.Allowed {
		t.Fatalf("open 模式应放行: err=%v allowed=%v", err, d.Allowed)
	}
	if !res.Local {
		t.Fatal("open 模式的预扣必须标记 Local")
	}

	if err := f.Settle(context.Background(), res, 100); err != nil {
		t.Fatalf("结算不应报错: %v", err)
	}
	if primary.settleCalls != 0 {
		t.Fatalf("primary 已故障，不应调用 Settle, calls=%d", primary.settleCalls)
	}
}

// fallback：走本地配额，并且真的按本地账本限额。
func TestFallbackTPMFallbackEnforcesLocalLimit(t *testing.T) {
	primary := &fakeLimiter{reserveErr: errors.New("connection refused")}
	local := NewLocalTPM(time.Minute)
	f := NewFallbackTPM(primary, local, FailFallback)

	res, d, err := f.Reserve(context.Background(), 1, 10, 10)
	if err != nil || !d.Allowed || !res.Local {
		t.Fatalf("首次预扣应放行并标记 Local: err=%v allowed=%v local=%v", err, d.Allowed, res.Local)
	}

	// 本地额度已满，第二次必须拒绝。
	_, d2, err := f.Reserve(context.Background(), 1, 1, 10)
	if err != nil || d2.Allowed {
		t.Fatalf("本地额度已满应拒绝: err=%v allowed=%v", err, d2.Allowed)
	}

	if err := f.Settle(context.Background(), res, 4); err != nil {
		t.Fatalf("本地结算失败: %v", err)
	}
	if primary.settleCalls != 0 {
		t.Fatalf("降级预扣不应结算到 primary, calls=%d", primary.settleCalls)
	}

	// 结算退了 6 个，本地又能用 6 个。
	if _, d3, _ := f.Reserve(context.Background(), 1, 6, 10); !d3.Allowed {
		t.Fatal("退还的额度应可再次使用")
	}
}

// 结算按 Reservation 的来源分流，不做跨后端回退。
func TestFallbackTPMSettleRoutesBySource(t *testing.T) {
	primary := &fakeLimiter{}
	f := NewFallbackTPM(primary, NewLocalTPM(time.Minute), FailFallback)
	ctx := context.Background()

	if err := f.Settle(ctx, Reservation{Key: "k", Reserved: 10, Limit: 100}, 20); err != nil {
		t.Fatalf("primary 结算失败: %v", err)
	}
	if primary.settleCalls != 1 || primary.lastActual != 20 {
		t.Fatalf("primary 结算次数=%d actual=%d", primary.settleCalls, primary.lastActual)
	}

	if err := f.Settle(ctx, Reservation{Key: "k", Reserved: 10, Limit: 100, Local: true}, 20); err != nil {
		t.Fatalf("本地结算失败: %v", err)
	}
	if primary.settleCalls != 1 {
		t.Fatalf("本地预扣不应触发 primary 结算, calls=%d", primary.settleCalls)
	}
}

// 配置写错（未知 mode）时宁可拒绝，也不能静默 fail-open。
func TestFallbackTPMInvalidModeFallsBackToClosed(t *testing.T) {
	primary := &fakeLimiter{reserveErr: errors.New("connection refused")}
	f := NewFallbackTPM(primary, nil, FailMode("bogus"))

	if f.Mode() != FailClosed {
		t.Fatalf("非法 mode 应回落到 closed, got %q", f.Mode())
	}
	if _, _, err := f.Reserve(context.Background(), 1, 1, 100); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("应返回 ErrUnavailable, got %v", err)
	}
}

// 结算阶段 Redis 出错时返回错误，但不会把账记到本地。
func TestFallbackTPMSettleErrorPropagates(t *testing.T) {
	primary := &fakeLimiter{settleErr: errors.New("redis down")}
	f := NewFallbackTPM(primary, NewLocalTPM(time.Minute), FailFallback)

	err := f.Settle(context.Background(), Reservation{Key: "k", Reserved: 10, Limit: 100}, 20)
	if err == nil {
		t.Fatal("primary 结算失败应把错误返回给调用方")
	}
}
