package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SettingsStore 负责 gateway_settings 表（运行时可配置参数）。
type SettingsStore struct {
	pool *pgxpool.Pool
}

// NewSettingsStore 创建 SettingsStore。
func NewSettingsStore(pool *pgxpool.Pool) *SettingsStore {
	return &SettingsStore{pool: pool}
}

// TPMWindow 返回已保存的 TPM 窗口长度与更新时间。
// 还没配置过时返回 (0, zero time, nil)。
func (s *SettingsStore) TPMWindow(ctx context.Context) (int, time.Time, error) {
	var seconds int
	var updatedAt time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT tpm_window_seconds, updated_at FROM gateway_settings WHERE id = TRUE`,
	).Scan(&seconds, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, time.Time{}, nil
		}
		return 0, time.Time{}, fmt.Errorf("get tpm window setting failed: %w", err)
	}
	return seconds, updatedAt, nil
}

// SetTPMWindow 写入 / 更新窗口长度（秒），返回更新时间。
func (s *SettingsStore) SetTPMWindow(ctx context.Context, seconds int) (time.Time, error) {
	var updatedAt time.Time
	err := s.pool.QueryRow(ctx,
		`INSERT INTO gateway_settings (id, tpm_window_seconds, updated_at)
		 VALUES (TRUE, $1, now())
		 ON CONFLICT (id) DO UPDATE
		 SET tpm_window_seconds = EXCLUDED.tpm_window_seconds,
		     updated_at = now()
		 RETURNING updated_at`,
		seconds,
	).Scan(&updatedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("set tpm window setting failed: %w", err)
	}
	return updatedAt, nil
}
