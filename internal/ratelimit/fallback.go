package ratelimit

import (
	"context"
	"sync"
	"time"
)

type FailMode string

type FallbackLimiter struct {
	primary  Limiter // Redis限流:滑动窗口
	fallback Limiter // 本地令牌桶
	mode     FailMode

	// 告警冷却：避免 Redis 挂时每条请求都刷 Error 日志
	mu        sync.Mutex
	lastAlert time.Time
	alertGap  time.Duration
}

const (
	FailClosed   FailMode = "closed"
	FailFallback FailMode = "fallback"
	FailOpen     FailMode = "open"
)

func NewFallbackLimiter(primary, fallback Limiter, mode FailMode) *FallbackLimiter {
	return &FallbackLimiter{primary: primary, fallback: fallback, mode: mode}
}

func (f *FallbackLimiter) Allow(ctx context.Context, key string, limit int64) (bool, error) {
	allowed, err := f.primary.Allow(ctx, key, limit)
	if err == nil {
		return allowed, nil
	}

	switch f.mode {
	case FailClosed:
		return false, nil
	case FailFallback:
		return f.fallback.Allow(ctx, key, limit)
	case FailOpen:
		return true, nil
	}
	return false, nil
}

// AllowPair 一次检查两个 key（租户级 + Key 级），返回 (是否都放行, 哪个维度超限)。
// 语义与 RedisLimiter.AllowPair 对齐：blocked 为 1/2 指出超限维度，0 表示都放行。
// Redis 故障时按 FailMode 降级：fallback 用本地令牌桶逐 key 检查，closed 直接拒绝，open 放行。
func (f *FallbackLimiter) AllowPair(ctx context.Context, key1 string, limit1 int64, key2 string, limit2 int64) (bool, int, error) {
	type pairLimiter interface {
		AllowPair(ctx context.Context, key1 string, limit1 int64, key2 string, limit2 int64) (bool, int, error)
	}

	if p, ok := f.primary.(pairLimiter); ok {
		allowed, blocked, err := p.AllowPair(ctx, key1, limit1, key2, limit2)
		if err == nil {
			return allowed, blocked, nil
		}
	}

	// primary 不支持 AllowPair 或出错 → 降级
	switch f.mode {
	case FailClosed:
		return false, 0, nil
	case FailFallback:
		// 本地令牌桶不支持 pair，逐 key 检查（租户优先，与旧串行顺序一致）
		if ok, _ := f.fallback.Allow(ctx, key1, limit1); !ok {
			return false, 1, nil
		}
		if ok, _ := f.fallback.Allow(ctx, key2, limit2); !ok {
			return false, 2, nil
		}
		return true, 0, nil
	case FailOpen:
		return true, 0, nil
	}
	return false, 0, nil
}

// FailMode 返回当前失败模式，供限流中间件在 limiter 报错时决定放行还是拒绝。
func (f *FallbackLimiter) FailMode() FailMode { return f.mode }

func (f *FallbackLimiter) RetryAfter(key string, limit int64) time.Duration {
	if p, ok := f.primary.(RetryAfterProvider); ok {
		return p.RetryAfter(key, limit)
	}
	if p, ok := f.fallback.(RetryAfterProvider); ok {
		return p.RetryAfter(key, limit)
	}
	return time.Second
}
