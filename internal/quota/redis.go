package quota

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const reserveScript = `
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local need = tonumber(ARGV[2])
local ttl = tonumber(ARGV[3])

local used = tonumber(redis.call('GET', key) or '0')
if used + need  > limit then 
	local left = redis.call('TTL', key)
	if left < 0 then left = ttl end
	return {0, used, left}
end

used = redis.call('INCRBY', key, need)
if redis.call('TTL', key) < 0 then
	redis.call('EXPIRE', key, ttl)
end
return {1, used, ttl}
`

const settleScript = `
local key = KEYS[1]
local delta = tonumber(ARGV[1])
local ttl = tonumber(ARGV[2])

if delta == 0 then return 0 end
if delta > 0 then
	local v = redis.call('INCRBY', key, delta)
	if redis.call('TTL', key) < 0 then
		redis.call('EXPIRE', key, ttl) 
	end
	return v
end

local cur = tonumber(redis.call('GET', key) or '0')
if cur <= 0 then return 0 end
local dec = math.min(-delta, cur)
return redis.call('DECRBY', key, dec)
`

var _ Limiter = (*RedisTPM)(nil)

// minKeyTTL 是配额 key 的最小存活时间。
// 只用于回收旧窗口，真正的额度窗口由 window 决定；
// 取 10 分钟是为了让超过窗口长度的大请求仍能正常结算。
const minKeyTTL = 10 * time.Minute

type RedisTPM struct {
	rdb     *redis.Client
	now     func() time.Time
	window  time.Duration
	reserve *redis.Script
	settle  *redis.Script
}

// NewRedisTPM 创建默认 1 分钟窗口的限流器。
func NewRedisTPM(rdb *redis.Client) *RedisTPM {
	return NewRedisTPMWithWindow(rdb, time.Minute)
}

// NewRedisTPMWithWindow 创建指定窗口长度的限流器。
// window <= 0 时回落到 1 分钟；小于 1 秒按 1 秒处理。
func NewRedisTPMWithWindow(rdb *redis.Client, window time.Duration) *RedisTPM {
	if window <= 0 {
		window = time.Minute
	}
	if window < time.Second {
		window = time.Second
	}
	return &RedisTPM{
		rdb:     rdb,
		now:     time.Now,
		window:  window,
		reserve: redis.NewScript(reserveScript),
		settle:  redis.NewScript(settleScript),
	}
}

func (rt *RedisTPM) Reserve(ctx context.Context, tenantID, need, limit int64) (Reservation, Decision, error) {
	now := rt.now()

	key := rt.windowKey(tenantID, now)

	if limit <= 0 {
		return Reservation{Key: key, Reserved: need, Limit: limit},
			Decision{Allowed: true, Limit: limit}, nil
	}
	if need < 1 {
		need = 1
	}

	res, err := rt.reserve.Run(ctx, rt.rdb, []string{key}, limit, need, rt.ttlSeconds()).Int64Slice()
	if err != nil {
		return Reservation{}, Decision{}, fmt.Errorf("tpm reserve failed: %w", err)
	}
	if len(res) != 3 {
		return Reservation{}, Decision{}, fmt.Errorf("tpm reserve: unexpected reply len=%d", len(res))
	}

	d := Decision{Allowed: res[0] == 1, Used: res[1], Limit: limit}
	if !d.Allowed {
		d.RetryAfter = rt.nextWindow(now).Sub(now)
		if d.RetryAfter <= 0 {
			d.RetryAfter = time.Second
		}
	}
	return Reservation{Key: key, Reserved: need, Limit: limit}, d, nil
}

func (rt *RedisTPM) Settle(ctx context.Context, r Reservation, actual int64) error {
	if r.Limit <= 0 {
		return nil
	}
	if actual < 0 {
		actual = 0
	}
	delta := actual - r.Reserved
	if delta == 0 {
		return nil
	}
	if err := rt.settle.Run(ctx, rt.rdb, []string{r.Key}, delta, rt.ttlSeconds()).Err(); err != nil {
		return fmt.Errorf("tpm settle failed: %w", err)
	}
	return nil
}

// windowKey 固定窗口：tpm:{tenant}:{unixWindow}
//
// unixWindow = unix秒 / 窗口秒数，窗口一变 key 空间也跟着变，
// 因此改窗口长度不会串用旧额度（旧 key 到期自动回收）。
func (rt *RedisTPM) windowKey(tenantID int64, t time.Time) string {
	secs := int64(rt.window / time.Second)
	return "tpm:" + strconv.FormatInt(tenantID, 10) + ":" + strconv.FormatInt(t.Unix()/secs, 10)
}

// nextWindow 返回当前窗口结束时刻，用于计算 Retry-After。
func (rt *RedisTPM) nextWindow(now time.Time) time.Time {
	secs := int64(rt.window / time.Second)
	return time.Unix((now.Unix()/secs+1)*secs, 0)
}

// ttlSeconds 决定配额 key 的存活时间：至少 10 分钟，
// 窗口更长时按窗口 + 1 分钟，避免窗口没过完 key 先过期。
func (rt *RedisTPM) ttlSeconds() int {
	ttl := rt.window + time.Minute
	if ttl < minKeyTTL {
		ttl = minKeyTTL
	}
	return int(ttl / time.Second)
}
