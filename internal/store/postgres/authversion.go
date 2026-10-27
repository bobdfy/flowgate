package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// execer 是 *pgxpool.Pool 和 pgx.Tx 共有的写接口。
// bumpAuthVersion 在事务里调用，保证「数据改动 + 版本号」一起提交。
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// bumpAuthVersion 递增鉴权配置版本号。
//
// gateway 每 3 秒轮询一次；版本号变化 → 清空进程内鉴权缓存。
// 因此 key 的白名单 / 限额 / 停用、租户状态改动都能在几秒内生效。
func bumpAuthVersion(ctx context.Context, db execer) error {
	if _, err := db.Exec(ctx,
		`UPDATE auth_version SET version = version + 1, updated_at = now() WHERE id = TRUE`); err != nil {
		return fmt.Errorf("bump auth version failed: %w", err)
	}
	return nil
}

// CurrentAuthVersion 读取当前鉴权配置版本号（gateway 轮询用）。
func CurrentAuthVersion(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var v int64
	if err := pool.QueryRow(ctx,
		`SELECT version FROM auth_version WHERE id = TRUE`).Scan(&v); err != nil {
		return 0, fmt.Errorf("read auth version failed: %w", err)
	}
	return v, nil
}
