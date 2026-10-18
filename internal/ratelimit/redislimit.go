package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const slidingWindowScript = `
local key    = KEYS[1]
local window = tonumber(ARGV[1])
local limit  = tonumber(ARGV[2])
local now    = tonumber(ARGV[3])
local member = ARGV[4]

redis.call('ZREMRANGEBYSCORE', key, "-inf", now - window)
local count = redis.call('ZCARD', key)
if count >= limit then
    return 0
end
redis.call('ZADD', key, now, member)
redis.call('PEXPIRE', key, window)
return 1
`

// slidingWindowPairScript 一次原子检查两个 key（租户级 + Key 级）。
// 返回 0/1 表示「哪个 key 超限」（0=都放行，1=第一个超限，2=第二个超限），
// 也顺带解决了一个语义问题：旧实现串行检查时，「租户过了、Key 被拒」
// 会白白消耗租户计数 —— 原子双 key 检查里，任一超限则两者都不写入。
const slidingWindowPairScript = `
local k1     = KEYS[1]
local k2     = KEYS[2]
local window = tonumber(ARGV[1])
local limit1 = tonumber(ARGV[2])
local limit2 = tonumber(ARGV[3])
local now    = tonumber(ARGV[4])
local member = ARGV[5]

redis.call('ZREMRANGEBYSCORE', k1, "-inf", now - window)
local c1 = redis.call('ZCARD', k1)
if c1 >= limit1 then
    return 1
end

redis.call('ZREMRANGEBYSCORE', k2, "-inf", now - window)
local c2 = redis.call('ZCARD', k2)
if c2 >= limit2 then
    return 2
end

redis.call('ZADD', k1, now, member)
redis.call('PEXPIRE', k1, window)
redis.call('ZADD', k2, now, member)
redis.call('PEXPIRE', k2, window)
return 0
`

type RedisLimiter struct {
	rdb      *redis.Client
	window   time.Duration // 窗口大小
	script   *redis.Script
	pairScript *redis.Script
}

func NewRedisLimiter(rdb *redis.Client, window time.Duration) *RedisLimiter {
	return &RedisLimiter{
		rdb:        rdb,
		window:     window,
		script:     redis.NewScript(slidingWindowScript),
		pairScript: redis.NewScript(slidingWindowPairScript),
	}
}

func (l *RedisLimiter) Allow(ctx context.Context, key string, limit int64) (bool, error) {
	now := time.Now().UnixMilli()
	// member 要唯一：时间戳 + 纳秒，避免同一毫秒内两个请求覆盖
	member := fmt.Sprintf("%d-%d", now, time.Now().UnixNano())

	result, err := l.script.Run(ctx, l.rdb, []string{"rl:" + key}, l.window.Milliseconds(), limit, now, member).Int()
	if err != nil {
		return false, fmt.Errorf("redis rate limit failed: %w", err)
	}
	return result == 1, nil
}

// AllowPair 一次检查两个 key（租户级 + Key 级），返回 (是否都放行, 哪个维度超限)。
// ok=true 表示两个 key 都没超限；ok=false 时 blocked 为 1 或 2，指出是哪个维度超限。
// 一次 Redis 往返完成两层限流，替代原来两次串行 Allow。
func (l *RedisLimiter) AllowPair(ctx context.Context, key1 string, limit1 int64, key2 string, limit2 int64) (ok bool, blocked int, err error) {
	now := time.Now().UnixMilli()
	member := fmt.Sprintf("%d-%d", now, time.Now().UnixNano())

	result, err := l.pairScript.Run(ctx, l.rdb,
		[]string{"rl:" + key1, "rl:" + key2},
		l.window.Milliseconds(), limit1, limit2, now, member).Int()
	if err != nil {
		return false, 0, fmt.Errorf("redis rate limit pair failed: %w", err)
	}
	if result == 0 {
		return true, 0, nil
	}
	return false, result, nil
}

// RetryAfter 滑动窗口不好精确算等待时长，返回保守值（窗口剩余时间）。
func (l *RedisLimiter) RetryAfter(key string, limit int64) time.Duration {
	// 简化：无法精确算，给窗口长度作为兜底
	return l.window
}
