package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TenantStore 负责 tenants 表的数据访问。
type TenantStore struct {
	pool *pgxpool.Pool
}

// NewTenantStore 创建一个 TenantStore。
func NewTenantStore(pool *pgxpool.Pool) *TenantStore {
	return &TenantStore{pool: pool}
}

// Create 插入一条租户记录，并把自增 ID 写回 t.ID。
func (s *TenantStore) Create(ctx context.Context, t *model.Tenant) error {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO tenants (name, status, qps_limit, tpm_limit) VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		t.Name, t.Status, t.QPSLimit, t.TPMLimit,
	).Scan(&t.ID, &t.CreatedAt)
	if err != nil {
		return fmt.Errorf("create tenant (name=%s) failed: %w", t.Name, err)
	}
	return nil
}

// GetByID 按 ID 查租户；不存在返回 (nil, nil)。
func (s *TenantStore) GetByID(ctx context.Context, id int64) (*model.Tenant, error) {
	var t model.Tenant
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, status, qps_limit, tpm_limit, created_at FROM tenants WHERE id = $1`, id,
	).Scan(&t.ID, &t.Name, &t.Status, &t.QPSLimit, &t.TPMLimit, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get tenant by id=%d failed: %w", id, err)
	}
	return &t, nil
}

// List 返回所有租户。
func (s *TenantStore) List(ctx context.Context) ([]model.Tenant, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, status, qps_limit, tpm_limit, created_at FROM tenants ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list tenants failed: %w", err)
	}
	defer rows.Close()

	tenants := []model.Tenant{}
	for rows.Next() {
		var t model.Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Status, &t.QPSLimit, &t.TPMLimit, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tenant row failed: %w", err)
		}
		tenants = append(tenants, t)
	}
	return tenants, rows.Err()
}

// Update 按 ID 更新租户（name/status）。
func (s *TenantStore) Update(ctx context.Context, t *model.Tenant) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin update tenant (id=%d): %w", t.ID, err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE tenants SET name = $1, status = $2, qps_limit = $3, tpm_limit = $4 WHERE id = $5`,
		t.Name, t.Status, t.QPSLimit, t.TPMLimit, t.ID)
	if err != nil {
		return fmt.Errorf("update tenant (id=%d) failed: %w", t.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update tenant (id=%d) failed: record not found", t.ID)
	}
	// 租户限额 / 状态被缓存进 identity，改完要让 gateway 清缓存。
	if err := bumpAuthVersion(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit update tenant (id=%d) failed: %w", t.ID, err)
	}
	return nil
}

// Delete 按 ID 删除租户（级联删除其 api_keys）。
func (s *TenantStore) Delete(ctx context.Context, id int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete tenant (id=%d): %w", id, err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete tenant (id=%d) failed: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete tenant (id=%d) failed: record not found", id)
	}
	// 级联删掉的 key 可能还在 gateway 缓存里。
	if err := bumpAuthVersion(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete tenant (id=%d) failed: %w", id, err)
	}
	return nil
}
