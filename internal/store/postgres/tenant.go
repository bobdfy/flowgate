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
		`INSERT INTO tenants (name, status, qps_limit) VALUES ($1, $2, $3) RETURNING id, created_at`,
		t.Name, t.Status, t.QPSLimit,
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
		`SELECT id, name, status, qps_limit, created_at FROM tenants WHERE id = $1`, id,
	).Scan(&t.ID, &t.Name, &t.Status, &t.QPSLimit, &t.CreatedAt)
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
		`SELECT id, name, status, qps_limit, created_at FROM tenants ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list tenants failed: %w", err)
	}
	defer rows.Close()

	tenants := []model.Tenant{}
	for rows.Next() {
		var t model.Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Status, &t.QPSLimit, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tenant row failed: %w", err)
		}
		tenants = append(tenants, t)
	}
	return tenants, rows.Err()
}

// Update 按 ID 更新租户（name/status）。
func (s *TenantStore) Update(ctx context.Context, t *model.Tenant) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE tenants SET name = $1, status = $2, qps_limit = $3 WHERE id = $4`,
		t.Name, t.Status, t.QPSLimit, t.ID)
	if err != nil {
		return fmt.Errorf("update tenant (id=%d) failed: %w", t.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update tenant (id=%d) failed: record not found", t.ID)
	}
	return nil
}

// Delete 按 ID 删除租户（级联删除其 api_keys）。
func (s *TenantStore) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete tenant (id=%d) failed: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete tenant (id=%d) failed: record not found", id)
	}
	return nil
}
