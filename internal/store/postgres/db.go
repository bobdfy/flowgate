// Package postgres 提供 PostgreSQL 数据访问层。
// 每个文件对应一张表，封装该表的增删改查。
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool 创建 PostgreSQL 连接池。
// dsn 是连接串，形如 postgres://flowgate:flowgate@localhost:5432/flowgate
// 调用方在程序退出时需 pool.Close() 释放连接。
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn failed: %w", err)
	}

	// 连接池上限：pgxpool 默认 max(4, NumCPU)，这里显式收窄，避免打满 Postgres
	cfg.MaxConns = 50

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("initialize pgxpool failed: %w", err)
	}
	// 验证连接是否可用
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database failed: %w", err)
	}
	return pool, nil
}
