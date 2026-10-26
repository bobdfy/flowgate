package quota

// TPM 配额在 Redis 故障时的显式降级策略。
//
// 没有这一层时，预扣失败只打一行日志就继续转发，等于配额治理静默失效。
// 这里把「故障时怎么办」变成配置：closed 拒绝、open 放行、fallback 用本地近似。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bobdfy/flowgate/internal/observability"
)

// ErrUnavailable 表示配额服务不可用。
// handler 用 errors.Is 认出它并回 503，而不是 429「配额已用尽」——
// 故障不是用户用超了，不能把责任推给用户。
var ErrUnavailable = errors.New("tpm: 配额服务不可用")

// FailMode 是 Redis 故障时的处理方式，取值与 QPS 的 -fail-mode 保持一致。
type FailMode string

const (
	FailClosed   FailMode = "closed"   // 拒绝需要 TPM 治理的请求
	FailFallback FailMode = "fallback" // 用每实例本地近似配额
	FailOpen     FailMode = "open"     // 放行，但必须告警并计数
)

// defaultAlertGap 是同一条故障日志的最小间隔。
// Redis 挂 10 分钟、每秒 100 个请求，不加冷却就是 6 万行重复日志。
const defaultAlertGap = 30 * time.Second

// FallbackTPM 给 Limiter 加一层故障策略。
//
// primary 正常时行为完全不变；只有 primary 报错才会走 mode 分支。
type FallbackTPM struct {
	primary Limiter
	local   *LocalTPM
	mode    FailMode

	// 只保护 lastAlert，不能把锁包在 primary 的调用外面：
	// 那是网络 I/O，包住之后所有预扣会串行化。
	mu        sync.Mutex
	lastAlert time.Time
	alertGap  time.Duration
}

var _ Limiter = (*FallbackTPM)(nil)

// NewFallbackTPM 创建降级包装。mode 非法时回落到 closed 并告警：
// 配置写错时宁可拒绝，也不要静默变成 fail-open。
func NewFallbackTPM(primary Limiter, local *LocalTPM, mode FailMode) *FallbackTPM {
	switch mode {
	case FailClosed, FailFallback, FailOpen:
	default:
		slog.Error("tpm_fail_mode_invalid", "mode", string(mode), "fallback_to", string(FailClosed))
		mode = FailClosed
	}
	if local == nil {
		local = NewLocalTPM(0)
	}
	return &FallbackTPM{
		primary:  primary,
		local:    local,
		mode:     mode,
		alertGap: defaultAlertGap,
	}
}

// Mode 返回实际生效的策略，便于启动时打日志。
func (f *FallbackTPM) Mode() FailMode { return f.mode }

// Reserve 先问 primary；只有它报错时才按 mode 降级。
func (f *FallbackTPM) Reserve(ctx context.Context, tenantID, need, limit int64) (Reservation, Decision, error) {
	res, d, err := f.primary.Reserve(ctx, tenantID, need, limit)
	if err == nil {
		return res, d, nil
	}

	f.report(err)

	switch f.mode {
	case FailOpen:
		// 放行但不记账。标 Local 是为了让 Settle 不去碰已经挂掉的 Redis。
		return Reservation{Local: true, Limit: limit},
			Decision{Allowed: true, Limit: limit}, nil
	case FailFallback:
		lres, ld, lerr := f.local.Reserve(ctx, tenantID, need, limit)
		if lerr != nil {
			// 本地实现不会返回错误；真出错了只能按不可用处理，绝不能放行。
			return Reservation{}, Decision{}, fmt.Errorf("%w: %v", ErrUnavailable, lerr)
		}
		lres.Local = true
		return lres, ld, nil
	default:
		return Reservation{}, Decision{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
}

// Settle 按预扣的来源决定落哪个账本，不做跨后端回退。
//
// primary 正常时预扣的那笔账只在 Redis 里，本地没有对应记录；
// 结算时 Redis 如果挂了，退回本地等于凭空记一笔，所以只记日志和错误。
func (f *FallbackTPM) Settle(ctx context.Context, r Reservation, actual int64) error {
	if r.Local {
		return f.local.Settle(ctx, r, actual)
	}
	err := f.primary.Settle(ctx, r, actual)
	if err != nil {
		f.report(err)
	}
	return err
}

// report 记录一次降级：指标每次都加，日志按 alertGap 冷却。
func (f *FallbackTPM) report(err error) {
	observability.TPMDegradedTotal.WithLabelValues(string(f.mode)).Inc()

	now := time.Now()
	f.mu.Lock()
	quiet := now.Sub(f.lastAlert) < f.alertGap
	if !quiet {
		f.lastAlert = now
	}
	f.mu.Unlock()

	if quiet {
		return
	}
	slog.Warn("tpm_degraded", "mode", string(f.mode), "err", err)
}
