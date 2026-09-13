package ratelimit

import (
	"context"
	"sync"
	"time"
)

// 限流器接口: 判断某个key的请求是否放行
// 返回 error 是给分布器实现(redis)
type Limiter interface {
	Allow(ctx context.Context, key string, limit int64) (bool, error)
}

// RetryAfterProvider 可选接口: 算出「还要等多久才有额度」
type RetryAfterProvider interface {
	RetryAfter(key string, limit int64) time.Duration
}

// 令牌桶, 以固定速率来补充令牌，桶容量限制最大突发
type TokenBucket struct {
	mu     sync.Mutex
	rate   float64   //每秒补充令牌数
	burst  float64   //容量
	tokens float64   //当前令牌数
	last   time.Time //上次补充的时间
	now    func() time.Time
}

// NewTokenBUcket() 创建令牌桶, 初始为满
func NewTokenBucket(rate, burst float64, now func() time.Time) *TokenBucket {
	if now == nil {
		now = time.Now
	}
	return &TokenBucket{
		rate:   rate,
		burst:  burst,
		tokens: burst,
		last:   now(),
		now:    now,
	}
}

// Allow 尝试取走一个令牌：够则扣 1 返回 true，不够返回 false。“
func (b *TokenBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refillLocked()
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// RetryAfter 返回还需多久才能攒够 1 个令牌。
func (b *TokenBucket) RetryAfter() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.refillLocked()
	if b.tokens >= 1 {
		return 0
	}
	if b.rate <= 0 {
		return time.Second
	}

	secs := (1 - b.tokens) / b.rate
	return time.Duration(secs * float64(time.Second))

}

// refillLocked 按距上次的经过时间补充令牌，封顶 burst（调用方持锁）。
func (b *TokenBucket) refillLocked() {
	now := b.now()
	elapsed := now.Sub(b.last).Seconds()
	if elapsed <= 0 {
		return
	}

	b.tokens = min(b.burst, b.tokens+elapsed*b.rate)
	b.last = now
}

// LocalLimiter 单实例本地限流：按 key 各维护一个令牌桶。
type LocalLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucketEntry
	//rate    float64 删除, rate由 Allow 传入, 每个桶不同
	burst  float64
	now    func() time.Time
	idle   time.Duration // 空闲多久的桶会被清理
	lastGC time.Time
}

type bucketEntry struct {
	bucket   *TokenBucket
	lastSeen time.Time // 这个 key 最后一次被访问的时间
}

// NewLocalLimiter 创建本地限流器：rate 每秒放行量，burst 突发容量。
func NewLocalLimiter(burst float64) *LocalLimiter {
	now := time.Now
	return &LocalLimiter{
		buckets: make(map[string]*bucketEntry),
		burst:   burst,
		now:     now,
		idle:    10 * time.Minute,
		lastGC:  now(),
	}
}

// Allow 实现 Limiter：拿 key 对应的桶判定，本地实现不返回 error。
func (l *LocalLimiter) Allow(ctx context.Context, key string, limit int64) (bool, error) {
	return l.bucketFor(key, limit).Allow(), nil
}

// RetryAfter 实现 RetryAfterProvider。
func (l *LocalLimiter) RetryAfter(key string, limit int64) time.Duration {
	return l.bucketFor(key, limit).RetryAfter()
}

// bucketFor 取 key 对应的桶，没有则创建；顺带清理长期空闲的桶（防内存泄漏）。
func (l *LocalLimiter) bucketFor(key string, limit int64) *TokenBucket {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()

	if e, ok := l.buckets[key]; ok {
		e.lastSeen = now
		if e.bucket.rate != float64(limit) {
			e.bucket = NewTokenBucket(float64(limit), l.burst, l.now)
		}
		return e.bucket
	}

	if now.Sub(l.lastGC) >= l.idle {
		for k, e := range l.buckets {
			if now.Sub(e.lastSeen) >= l.idle {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}

	b := NewTokenBucket(float64(limit), l.burst, l.now)
	l.buckets[key] = &bucketEntry{bucket: b, lastSeen: now}
	return b
}
