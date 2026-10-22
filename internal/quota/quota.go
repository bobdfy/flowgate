package quota

import (
	"context"
	"time"
)

// Reservation 是一次 Token预扣
type Reservation struct {
	Key      string
	Reserved int64
	Limit    int64
}

// Decision 预扣结果
type Decision struct {
	Allowed    bool
	Used       int64
	Limit      int64
	RetryAfter time.Duration
}

// Limiter 是Token配额限流器
type Limiter interface {
	// Reserve 在转发前原子预扣 need 个 token。
	Reserve(ctx context.Context, tenantID, need, limit int64) (Reservation, Decision, error)

	// Settle 在拿到真实用量后结算
	Settle(ctx context.Context, r Reservation, actual int64) error
}
