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

type RedisLimiter struct {
	rdb    *redis.Client
	window time.Duration // 窗口大小
	script *redis.Script
}

func NewRedisLimiter(rdb *redis.Client, window time.Duration) *RedisLimiter {
	return &RedisLimiter{rdb: rdb, window: window, script: redis.NewScript(slidingWindowScript)}
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

// RetryAfter 滑动窗口不好精确算等待时长，返回保守值（窗口剩余时间）。
func (l *RedisLimiter) RetryAfter(key string, limit int64) time.Duration {
	// 简化：无法精确算，给窗口长度作为兜底
	return l.window
}
