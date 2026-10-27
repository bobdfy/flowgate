package quota

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// windowConfigKey 保存当前生效的 TPM 窗口长度（秒）。
//
// 由 gateway 启动时写入，管理面查询用量时读它来对齐窗口，
// 省得 gateway / admin 各配一遍、改一处忘一处。
const windowConfigKey = "tpm:window"

// WindowUsage 是某租户当前窗口的 TPM 用量。
type WindowUsage struct {
	TenantID    int64
	Window      time.Duration
	WindowStart time.Time
	WindowEnd   time.Time
	Used        int64
}

// PublishWindow 把本实例的窗口长度写进 Redis，供管理面查询时对齐。
// 多实例部署时窗口长度应保持一致，否则以最后写入的为准。
func (rt *RedisTPM) PublishWindow(ctx context.Context) error {
	if rt.rdb == nil {
		return nil
	}
	if err := rt.rdb.Set(ctx, windowConfigKey, int64(rt.Window()/time.Second), 0).Err(); err != nil {
		return fmt.Errorf("publish tpm window failed: %w", err)
	}
	return nil
}

// PublishedWindow 读取 gateway 最近发布的窗口长度。
// ok=false 表示 Redis 里还没有这个 key（gateway 尚未启动）。
func (rt *RedisTPM) PublishedWindow(ctx context.Context) (time.Duration, bool, error) {
	if rt.rdb == nil {
		return 0, false, fmt.Errorf("tpm window: redis client is nil")
	}
	secs, err := rt.rdb.Get(ctx, windowConfigKey).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read published tpm window failed: %w", err)
	}
	if secs <= 0 {
		return 0, false, nil
	}
	return time.Duration(secs) * time.Second, true, nil
}

// Usage 返回某租户当前窗口已用 token 和窗口边界。
//
// 窗口长度优先取 Redis 里 gateway 发布的值；没有（gateway 还没启动过）
// 时回退到本实例创建时的 window。
func (rt *RedisTPM) Usage(ctx context.Context, tenantID int64) (WindowUsage, error) {
	if rt.rdb == nil {
		return WindowUsage{}, fmt.Errorf("tpm usage: redis client is nil")
	}

	window := rt.Window()
	secs, err := rt.rdb.Get(ctx, windowConfigKey).Int64()
	switch {
	case err == nil && secs > 0:
		window = time.Duration(secs) * time.Second
	case err != nil && !errors.Is(err, redis.Nil):
		return WindowUsage{}, fmt.Errorf("read tpm window failed: %w", err)
	}

	now := rt.now()
	start := windowStart(window, now)
	used, err := rt.rdb.Get(ctx, windowKeyFor(tenantID, window, now)).Int64()
	if errors.Is(err, redis.Nil) {
		// 窗口内还没有请求，或 key 已过期 = 用量 0。
		used, err = 0, nil
	}
	if err != nil {
		return WindowUsage{}, fmt.Errorf("read tpm usage failed: %w", err)
	}

	return WindowUsage{
		TenantID:    tenantID,
		Window:      window,
		WindowStart: start,
		WindowEnd:   start.Add(window),
		Used:        used,
	}, nil
}

// windowKeyFor 固定窗口 key：tpm:{tenant}:{unixWindow}。
//
// unixWindow = unix秒 / 窗口秒数，窗口一变 key 空间也跟着变，
// 因此改窗口长度不会串用旧额度（旧 key 到期自动回收）。
func windowKeyFor(tenantID int64, window time.Duration, t time.Time) string {
	secs := int64(window / time.Second)
	return "tpm:" + strconv.FormatInt(tenantID, 10) + ":" + strconv.FormatInt(t.Unix()/secs, 10)
}

// windowStart 返回 t 所在窗口的开始时刻。
func windowStart(window time.Duration, t time.Time) time.Time {
	secs := int64(window / time.Second)
	return time.Unix(t.Unix()/secs*secs, 0)
}
