package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrAINotFound 表示要改 / 删的 AI 配置记录不存在。
var ErrAINotFound = errors.New("ai config record not found")

// AIConfigStore 负责 AI provider / model 配置的读写。
//
// 每次写操作都会在同一事务里把 ai_config_version +1，
// gateway 侧靠轮询这个版本号决定要不要重建 AI handler。
type AIConfigStore struct {
	pool *pgxpool.Pool
}

// NewAIConfigStore 创建一个 AIConfigStore。
func NewAIConfigStore(pool *pgxpool.Pool) *AIConfigStore {
	return &AIConfigStore{pool: pool}
}

const aiProviderCols = `id, name, type, endpoint, upstream_path, api_key_env, api_key,
	stream_usage, enabled, created_at, updated_at`

// aiModelCols 的顺序必须和三处 rows.Scan 的接收顺序完全一致，改一处就得全改。
const aiModelCols = `name, provider_name, upstream_model, max_concurrency,
	enabled, created_at, updated_at`

// Version 返回当前配置版本号。
func (s *AIConfigStore) Version(ctx context.Context) (int64, error) {
	var v int64
	err := s.pool.QueryRow(ctx, `SELECT version FROM ai_config_version WHERE id = 1`).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("get ai config version failed: %w", err)
	}
	return v, nil
}

// CountProviders 返回 provider 数量，用于判断是否需要从 ai.yaml 导入种子。
func (s *AIConfigStore) CountProviders(ctx context.Context) (int64, error) {
	var n int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ai_providers`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count ai providers failed: %w", err)
	}
	return n, nil
}

// Snapshot 在一个事务里读出所有启用的 provider / model 和版本号，
// 保证 gateway 重建 handler 时看到的是同一份配置。
func (s *AIConfigStore) Snapshot(ctx context.Context) ([]model.AIProvider, []model.AIModel, int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("begin ai config snapshot failed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx,
		`SELECT `+aiProviderCols+` FROM ai_providers WHERE enabled ORDER BY name`)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("list ai providers failed: %w", err)
	}
	providers := []model.AIProvider{}
	for rows.Next() {
		var p model.AIProvider
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.Endpoint, &p.UpstreamPath,
			&p.APIKeyEnv, &p.APIKey, &p.StreamUsage, &p.Enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
			rows.Close()
			return nil, nil, 0, fmt.Errorf("scan ai provider failed: %w", err)
		}
		providers = append(providers, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, 0, fmt.Errorf("iter ai providers failed: %w", err)
	}

	rows, err = tx.Query(ctx,
		`SELECT `+aiModelCols+` FROM ai_models WHERE enabled ORDER BY name`)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("list ai models failed: %w", err)
	}
	models := []model.AIModel{}
	for rows.Next() {
		var m model.AIModel
		if err := rows.Scan(&m.Name, &m.ProviderName, &m.UpstreamModel, &m.MaxConcurrency,
			&m.Enabled, &m.CreatedAt, &m.UpdatedAt); err != nil {
			rows.Close()
			return nil, nil, 0, fmt.Errorf("scan ai model failed: %w", err)
		}
		models = append(models, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, 0, fmt.Errorf("iter ai models failed: %w", err)
	}

	var version int64
	if err := tx.QueryRow(ctx, `SELECT version FROM ai_config_version WHERE id = 1`).Scan(&version); err != nil {
		return nil, nil, 0, fmt.Errorf("get ai config version failed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, 0, fmt.Errorf("commit ai config snapshot failed: %w", err)
	}
	return providers, models, version, nil
}

// Seed 在表为空时把 ai.yaml 的内容导入 DB。已有数据则什么都不做。
func (s *AIConfigStore) Seed(ctx context.Context, providers []model.AIProvider, models []model.AIModel) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin ai config seed failed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var n int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ai_providers`).Scan(&n); err != nil {
		return false, fmt.Errorf("count ai providers failed: %w", err)
	}
	if n > 0 {
		return false, nil
	}

	for _, p := range providers {
		if _, err := tx.Exec(ctx,
			`INSERT INTO ai_providers (name, type, endpoint, upstream_path, api_key_env, api_key, stream_usage, enabled)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			p.Name, p.Type, p.Endpoint, p.UpstreamPath, p.APIKeyEnv, p.APIKey, p.StreamUsage, p.Enabled,
		); err != nil {
			return false, fmt.Errorf("seed ai provider %s failed: %w", p.Name, err)
		}
	}
	for _, m := range models {
		if _, err := tx.Exec(ctx,
			`INSERT INTO ai_models (name, provider_name, upstream_model, max_concurrency, enabled)
			 VALUES ($1,$2,$3,$4,$5)`,
			m.Name, m.ProviderName, m.UpstreamModel, m.MaxConcurrency, m.Enabled,
		); err != nil {
			return false, fmt.Errorf("seed ai model %s failed: %w", m.Name, err)
		}
	}
	if err := bumpAIVersion(ctx, tx); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit ai config seed failed: %w", err)
	}
	return true, nil
}

// ---- provider CRUD ----

// ListProviders 返回所有 provider。
func (s *AIConfigStore) ListProviders(ctx context.Context) ([]model.AIProvider, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+aiProviderCols+` FROM ai_providers ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list ai providers failed: %w", err)
	}
	defer rows.Close()

	out := []model.AIProvider{}
	for rows.Next() {
		var p model.AIProvider
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.Endpoint, &p.UpstreamPath,
			&p.APIKeyEnv, &p.APIKey, &p.StreamUsage, &p.Enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan ai provider failed: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateProvider 插入一个 provider 并 bump 版本号。
func (s *AIConfigStore) CreateProvider(ctx context.Context, p *model.AIProvider) error {
	return s.write(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`INSERT INTO ai_providers (name, type, endpoint, upstream_path, api_key_env, api_key, stream_usage, enabled)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id, created_at, updated_at`,
			p.Name, p.Type, p.Endpoint, p.UpstreamPath, p.APIKeyEnv, p.APIKey, p.StreamUsage, p.Enabled,
		).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return fmt.Errorf("create ai provider (name=%s) failed: %w", p.Name, err)
		}
		return nil
	})
}

// UpdateProvider 按 ID 更新 provider 并 bump 版本号。
func (s *AIConfigStore) UpdateProvider(ctx context.Context, p *model.AIProvider) error {
	return s.write(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`UPDATE ai_providers
			 SET name=$1, type=$2, endpoint=$3, upstream_path=$4, api_key_env=$5,
			     api_key=$6, stream_usage=$7, enabled=$8, updated_at=now()
			 WHERE id=$9
			 RETURNING created_at, updated_at`,
			p.Name, p.Type, p.Endpoint, p.UpstreamPath, p.APIKeyEnv, p.APIKey,
			p.StreamUsage, p.Enabled, p.ID,
		).Scan(&p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("update ai provider (id=%d): %w", p.ID, ErrAINotFound)
			}
			return fmt.Errorf("update ai provider (id=%d) failed: %w", p.ID, err)
		}
		return nil
	})
}

// DeleteProvider 按 ID 删除 provider（级联删除其 model）并 bump 版本号。
func (s *AIConfigStore) DeleteProvider(ctx context.Context, id int64) error {
	return s.write(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM ai_providers WHERE id=$1`, id)
		if err != nil {
			return fmt.Errorf("delete ai provider (id=%d) failed: %w", id, err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("delete ai provider (id=%d): %w", id, ErrAINotFound)
		}
		return nil
	})
}

// GetProviderByID 按 ID 查 provider；不存在返回 (nil, nil)。
func (s *AIConfigStore) GetProviderByID(ctx context.Context, id int64) (*model.AIProvider, error) {
	var p model.AIProvider
	err := s.pool.QueryRow(ctx, `SELECT `+aiProviderCols+` FROM ai_providers WHERE id=$1`, id).
		Scan(&p.ID, &p.Name, &p.Type, &p.Endpoint, &p.UpstreamPath,
			&p.APIKeyEnv, &p.APIKey, &p.StreamUsage, &p.Enabled, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get ai provider (id=%d) failed: %w", id, err)
	}
	return &p, nil
}

// GetProviderByName 按名字查 provider；不存在返回 (nil, nil)。
func (s *AIConfigStore) GetProviderByName(ctx context.Context, name string) (*model.AIProvider, error) {
	var p model.AIProvider
	err := s.pool.QueryRow(ctx, `SELECT `+aiProviderCols+` FROM ai_providers WHERE name=$1`, name).
		Scan(&p.ID, &p.Name, &p.Type, &p.Endpoint, &p.UpstreamPath,
			&p.APIKeyEnv, &p.APIKey, &p.StreamUsage, &p.Enabled, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get ai provider (name=%s) failed: %w", name, err)
	}
	return &p, nil
}

// ---- model CRUD ----

// ListModels 返回所有对外模型。
func (s *AIConfigStore) ListModels(ctx context.Context) ([]model.AIModel, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+aiModelCols+` FROM ai_models ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list ai models failed: %w", err)
	}
	defer rows.Close()

	out := []model.AIModel{}
	for rows.Next() {
		var m model.AIModel
		if err := rows.Scan(&m.Name, &m.ProviderName, &m.UpstreamModel, &m.MaxConcurrency,
			&m.Enabled, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan ai model failed: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateModel 插入一个模型别名并 bump 版本号。
func (s *AIConfigStore) CreateModel(ctx context.Context, m *model.AIModel) error {
	return s.write(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`INSERT INTO ai_models (name, provider_name, upstream_model, max_concurrency, enabled)
			 VALUES ($1,$2,$3,$4,$5) RETURNING created_at, updated_at`,
			m.Name, m.ProviderName, m.UpstreamModel, m.MaxConcurrency, m.Enabled,
		).Scan(&m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return fmt.Errorf("create ai model (name=%s) failed: %w", m.Name, err)
		}
		return nil
	})
}

// UpdateModel 按 name 更新模型并 bump 版本号。别名本身不可改。
func (s *AIConfigStore) UpdateModel(ctx context.Context, m *model.AIModel) error {
	return s.write(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`UPDATE ai_models
			 SET provider_name=$1, upstream_model=$2, max_concurrency=$3, enabled=$4, updated_at=now()
			 WHERE name=$5
			 RETURNING created_at, updated_at`,
			m.ProviderName, m.UpstreamModel, m.MaxConcurrency, m.Enabled, m.Name,
		).Scan(&m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("update ai model (name=%s): %w", m.Name, ErrAINotFound)
			}
			return fmt.Errorf("update ai model (name=%s) failed: %w", m.Name, err)
		}
		return nil
	})
}

// DeleteModel 按 name 删除模型并 bump 版本号。
func (s *AIConfigStore) DeleteModel(ctx context.Context, name string) error {
	return s.write(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM ai_models WHERE name=$1`, name)
		if err != nil {
			return fmt.Errorf("delete ai model (name=%s) failed: %w", name, err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("delete ai model (name=%s): %w", name, ErrAINotFound)
		}
		return nil
	})
}

// write 把一次写操作和版本号 +1 放进同一个事务。
func (s *AIConfigStore) write(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin ai config write failed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(tx); err != nil {
		return err
	}
	if err := bumpAIVersion(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ai config write failed: %w", err)
	}
	return nil
}

func bumpAIVersion(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `UPDATE ai_config_version SET version = version + 1 WHERE id = 1`); err != nil {
		return fmt.Errorf("bump ai config version failed: %w", err)
	}
	return nil
}
