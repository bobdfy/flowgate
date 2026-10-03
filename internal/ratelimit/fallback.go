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
