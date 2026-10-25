package gatewayapp

// 运行时设置的轮询与热切换（目前只有 TPM 窗口）

import (
	"context"
	"log/slog"
	"time"

	"github.com/bobdfy/flowgate/internal/quota"
	"github.com/bobdfy/flowgate/internal/store/postgres"
)

// tpmWindowPollInterval 与管理面改配置后的生效延迟一致（与 AI 配置相同）。
const tpmWindowPollInterval = 3 * time.Second

// ResolveTPMWindow 决定启动时用哪个窗口：
//  1. 数据库里有配置 → 用它（事实来源）；
//  2. 没有配置 → 用 -tpm-window 兜底，并写回数据库作为初始配置。
func ResolveTPMWindow(ctx context.Context, store *postgres.SettingsStore, fallback time.Duration) (time.Duration, string) {
	secs, _, err := store.TPMWindow(ctx)
	if err != nil {
		slog.Warn("tpm_window_setting_unavailable", "err", err)
		return fallback, "flag"
	}
	if secs > 0 {
		return time.Duration(secs) * time.Second, "database"
	}

	seed := int(fallback / time.Second)
	if seed < 1 {
		seed = 1
	}
	if _, err := store.SetTPMWindow(ctx, seed); err != nil {
		slog.Warn("tpm_window_seed_failed", "err", err)
		return fallback, "flag"
	}
	slog.Info("tpm_window_seeded", "window", fallback.String())
	return fallback, "flag-seeded"
}

// WatchTPMWindow 每 3 秒读一次数据库，窗口变化时热切换并重新发布到 Redis。
//
// 切换对在途请求安全：Reserve 返回的 Reservation 里带 key，
// Settle 用的还是旧窗口的 key，不会把旧请求算进新窗口。
func WatchTPMWindow(ctx context.Context, store *postgres.SettingsStore, tpm *quota.RedisTPM) {
	ticker := time.NewTicker(tpmWindowPollInterval)
	defer ticker.Stop()

	current := tpm.Window()
	// 0 保证第一轮必发布一次，顺带补上启动时发布失败的情况。
	var published time.Duration

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			secs, _, err := store.TPMWindow(ctx)
			if err != nil {
				slog.Warn("tpm_window_poll_failed", "err", err)
				continue
			}
			if secs <= 0 {
				continue
			}

			next := time.Duration(secs) * time.Second
			if next != current {
				tpm.SetWindow(next)
				slog.Info("tpm_window_changed",
					"old", current.String(),
					"new", next.String(),
					"source", "database")
				current = next
			}

			// 发布失败时下一轮继续重试，避免管理面用量查询一直用旧窗口。
			if published != current {
				if err := tpm.PublishWindow(ctx); err != nil {
					slog.Warn("tpm_window_publish_failed", "err", err)
					continue
				}
				published = current
			}
		}
	}
}
