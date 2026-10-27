package gatewayapp

import (
	"context"
	"log/slog"
	"time"

	"github.com/bobdfy/flowgate/internal/middleware"
	"github.com/bobdfy/flowgate/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// authCachePollInterval 是鉴权配置版本号的轮询间隔。
// 与 AI 配置热更新保持一致：管理面改完，最坏多等一个周期。
const authCachePollInterval = 3 * time.Second

// WatchAuthVersion 轮询 auth_version，发现变化就清空鉴权缓存。
//
// 鉴权缓存存的是整个 model.APIKey（含 allowed_models 和租户限额），
// 不失效的话，改白名单要等 TTL 过期才生效。版本号由管理面写 API Key /
// 租户时在同一个事务里 +1。
func WatchAuthVersion(ctx context.Context, db *pgxpool.Pool, a *middleware.Authenticator) {
	last := int64(-1)
	ticker := time.NewTicker(authCachePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			v, err := postgres.CurrentAuthVersion(ctx, db)
			if err != nil {
				slog.Warn("auth_version_read_failed", "err", err)
				continue
			}
			if last < 0 {
				// 首次读到值只记录：启动时缓存本来就是空的。
				last = v
				continue
			}
			if v == last {
				continue
			}
			a.InvalidateCache()
			last = v
			slog.Info("auth_cache_invalidated", "version", v)
		}
	}
}
