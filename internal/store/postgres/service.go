package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ServiceStore 负责 gateway_services 表的数据访问。
type ServiceStore struct {
	pool *pgxpool.Pool
}

// NewServiceStore 创建一个 ServiceStore。
func NewServiceStore(pool *pgxpool.Pool) *ServiceStore {
	return &ServiceStore{pool: pool}
}

// Create 插入一条服务记录，并把数据库生成的自增 ID 写回 svc.ID。
func (s *ServiceStore) Create(ctx context.Context, svc *model.Service) error {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO gateway_services (name, protocol, connect_timeout_ms, request_timeout_ms, enabled, max_retries, retry_on_status, retry_backoff_ms,
		cb_failure_threshold, cb_cooldown_ms, cb_half_open_limit, response_header_timeout_ms)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 RETURNING id`,
		svc.Name, svc.Protocol, svc.ConnectTimeoutMs,
		svc.RequestTimeoutMs, svc.Enabled,
		svc.MaxRetries, svc.RetryOnStatus, svc.RetryBackoffMs,
		svc.CBFailureThreshold, svc.CBCooldownMs, svc.CBHalfOpenLimit, svc.ResponseHeaderTimeoutMs,
	).Scan(&svc.ID)

	if err != nil {
		return fmt.Errorf("create service failed: %w", err)
	}
	return nil
}

// GetByID 按 ID 查询服务。
// 约定：不存在时返回 (nil, nil)，而不是报错。
func (s *ServiceStore) GetByID(ctx context.Context, id int64) (*model.Service, error) {
	var svc model.Service
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, protocol, connect_timeout_ms, request_timeout_ms, enabled,
		        max_retries, retry_on_status, retry_backoff_ms, cb_failure_threshold, cb_cooldown_ms, cb_half_open_limit, response_header_timeout_ms,
		        created_at, updated_at
		 FROM gateway_services 
		 WHERE id = $1`,
		id,
	).Scan(
		&svc.ID, &svc.Name, &svc.Protocol, &svc.ConnectTimeoutMs, &svc.RequestTimeoutMs, &svc.Enabled,
		&svc.MaxRetries, &svc.RetryOnStatus, &svc.RetryBackoffMs, &svc.CBFailureThreshold, &svc.CBCooldownMs, &svc.CBHalfOpenLimit, &svc.ResponseHeaderTimeoutMs, &svc.CreatedAt, &svc.UpdatedAt,
	)

	if err != nil {
		// 按约定：不存在时返回 (nil, nil)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query service by id=%d failed: %w", id, err)
	}
	return &svc, nil
}

// List 返回所有服务。
func (s *ServiceStore) List(ctx context.Context) ([]model.Service, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, protocol, connect_timeout_ms, request_timeout_ms, enabled,
		        max_retries, retry_on_status, retry_backoff_ms, cb_failure_threshold, cb_cooldown_ms, cb_half_open_limit, response_header_timeout_ms,
		        created_at, updated_at
		 FROM gateway_services 
		 ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list services failed: %w", err)
	}
	defer rows.Close() // 重要：防止连接泄露

	var services []model.Service
	for rows.Next() {
		var svc model.Service
		err := rows.Scan(
			&svc.ID,
			&svc.Name,
			&svc.Protocol,
			&svc.ConnectTimeoutMs,
			&svc.RequestTimeoutMs,
			&svc.Enabled,
			&svc.MaxRetries,
			&svc.RetryOnStatus,
			&svc.RetryBackoffMs,
			&svc.CBFailureThreshold,
			&svc.CBCooldownMs,
			&svc.CBHalfOpenLimit,
			&svc.ResponseHeaderTimeoutMs,
			&svc.CreatedAt,
			&svc.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan service row failed: %w", err)
		}
		services = append(services, svc)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	// 无数据时返回空切片，避免调用方判空麻烦
	if services == nil {
		return []model.Service{}, nil
	}
	return services, nil
}

// Update 按 ID 更新服务的可变字段（name/protocol/超时/enabled）。
func (s *ServiceStore) Update(ctx context.Context, svc *model.Service) error {
	cmdTag, err := s.pool.Exec(ctx,
		`UPDATE gateway_services
		 SET name = $1, protocol = $2, connect_timeout_ms = $3, request_timeout_ms = $4, enabled = $5,
		     max_retries = $6, retry_on_status = $7, retry_backoff_ms = $8,
		     cb_failure_threshold = $9, cb_cooldown_ms = $10, cb_half_open_limit = $11, response_header_timeout_ms = $12,
		     updated_at = now()
		 WHERE id = $13`,
		svc.Name, svc.Protocol, svc.ConnectTimeoutMs,
		svc.RequestTimeoutMs, svc.Enabled,
		svc.MaxRetries, svc.RetryOnStatus, svc.RetryBackoffMs,
		svc.CBFailureThreshold, svc.CBCooldownMs, svc.CBHalfOpenLimit,
		svc.ResponseHeaderTimeoutMs, svc.ID,
	)
	if err != nil {
		return fmt.Errorf("update service failed: %w", err)
	}

	// 检查是否真的更新到了数据（防止静默失败）
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("update service failed: record not found")
	}
	return nil
}

// Delete 按 ID 删除服务。
// 提示：删除服务会级联删除其实例（SQL 里已配 ON DELETE CASCADE）。
func (s *ServiceStore) Delete(ctx context.Context, id int64) error {
	cmdTag, err := s.pool.Exec(ctx,
		`DELETE FROM gateway_services WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("delete service failed: %w", err)
	}

	// 如果影响行数为 0，说明该服务已不存在
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("delete service failed: record not found")
	}
	return nil
}
