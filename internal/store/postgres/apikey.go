package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// APIKeyStore 负责 api_keys 表的数据访问。
type APIKeyStore struct {
	pool *pgxpool.Pool
}

// NewAPIKeyStore 创建一个 APIKeyStore。
func NewAPIKeyStore(pool *pgxpool.Pool) *APIKeyStore {
	return &APIKeyStore{pool: pool}
}

// Create 插入一把 key，并把自增 ID 写回 k.ID。
func (s *APIKeyStore) Create(ctx context.Context, k *model.APIKey) error {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (tenant_id, name, key_hash, status, qps_limit, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		k.TenantID, k.Name, k.KeyHash, k.Status, k.QPSLimit, k.ExpiresAt,
	).Scan(&k.ID, &k.CreatedAt)
	if err != nil {
		return fmt.Errorf("create api key (name=%s) failed: %w", k.Name, err)
	}
	return nil
}

// GetByHash 按 key_hash 查 key（鉴权核心）。
// SQL 里直接过滤无效状态：key 停用、租户停用、已过期的一律查不到（= 该拒绝）。
// 不存在返回 (nil, nil)。
func (s *APIKeyStore) GetByHash(ctx context.Context, hash string) (*model.APIKey, error) {
	var k model.APIKey
	err := s.pool.QueryRow(ctx,
		`SELECT k.id, k.tenant_id, k.name, k.key_hash, k.status, k.qps_limit, k.expires_at, k.created_at,
		        t.qps_limit
		 FROM api_keys k
		 JOIN tenants t ON t.id = k.tenant_id
		 WHERE k.key_hash = $1
		   AND k.status = 'active'
		   AND t.status = 'active'
		   AND (k.expires_at IS NULL OR k.expires_at > now())`,
		hash,
	).Scan(&k.ID, &k.TenantID, &k.Name, &k.KeyHash, &k.Status, &k.QPSLimit, &k.ExpiresAt, &k.CreatedAt, &k.TenantQPSLimit)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get api key by hash failed: %w", err)
	}
	return &k, nil
}

// ListByTenant 返回某租户的所有 key。
func (s *APIKeyStore) ListByTenant(ctx context.Context, tenantID int64) ([]model.APIKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, name, key_hash, status, qps_limit, expires_at, created_at
		 FROM api_keys WHERE tenant_id = $1 ORDER BY id`,
		tenantID)
	if err != nil {
		return nil, fmt.Errorf("list api keys by tenant=%d failed: %w", tenantID, err)
	}
	defer rows.Close()

	keys := []model.APIKey{}
	for rows.Next() {
		var k model.APIKey
		if err := rows.Scan(&k.ID, &k.TenantID, &k.Name, &k.KeyHash, &k.Status, &k.QPSLimit, &k.ExpiresAt, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan api key row failed: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// Update 按 ID 更新 key（name/status/expires_at）。
func (s *APIKeyStore) Update(ctx context.Context, k *model.APIKey) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE api_keys SET name = $1, status = $2, qps_limit = $3, expires_at = $4 WHERE id = $5`,
		k.Name, k.Status, k.QPSLimit, k.ExpiresAt, k.ID)
	if err != nil {
		return fmt.Errorf("update api key (id=%d) failed: %w", k.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update api key (id=%d) failed: record not found", k.ID)
	}
	return nil
}

// Delete 按 ID 删除 key。
func (s *APIKeyStore) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete api key (id=%d) failed: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete api key (id=%d) failed: record not found", id)
	}
	return nil
}
