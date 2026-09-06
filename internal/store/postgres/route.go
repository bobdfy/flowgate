package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RouteStore 负责 gateway_routes 表的数据访问。
type RouteStore struct {
	pool *pgxpool.Pool
}

// NewRouteStore 创建一个 RouteStore。
func NewRouteStore(pool *pgxpool.Pool) *RouteStore {
	return &RouteStore{pool: pool}
}

// Create 插入一条路由记录，并把自增 ID 写回 route.ID。
func (r *RouteStore) Create(ctx context.Context, route *model.Route) error {
	err := r.pool.QueryRow(ctx,
		`INSERT INTO gateway_routes 
		(service_id, name, host, path_pattern, path_match_type, methods, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		route.ServiceID,
		route.Name,
		route.Host,
		route.PathPattern,
		route.PathMatchType,
		route.Methods,
		route.Enabled,
	).Scan(&route.ID)

	if err != nil {
		return fmt.Errorf("create route failed: %w", err)
	}
	return nil
}

// GetByID 按 ID 查询路由；不存在时返回 (nil, nil)。
func (r *RouteStore) GetByID(ctx context.Context, id int64) (*model.Route, error) {
	var route model.Route
	err := r.pool.QueryRow(ctx,
		`SELECT id, service_id, name, host, path_pattern, path_match_type, methods, enabled, created_at, updated_at
		 FROM gateway_routes 
		 WHERE id = $1`,
		id,
	).Scan(&route.ID, &route.ServiceID, &route.Name, &route.Host,
		&route.PathPattern, &route.PathMatchType, &route.Methods,
		&route.Enabled, &route.CreatedAt, &route.UpdatedAt,
	)

	if err != nil {
		// 如果是“无记录”，返回 (nil, nil) 而不是错误，符合你的 TODO 要求
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query route failed: %w", err)
	}
	return &route, nil

}

// List 返回所有路由。
func (r *RouteStore) List(ctx context.Context) ([]model.Route, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, service_id, name, host, path_pattern, path_match_type, methods, enabled, created_at, updated_at
		 FROM gateway_routes 
		 ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list routes failed: %w", err)
	}
	defer rows.Close() // 重点：必须关闭，防止连接泄露

	var routes []model.Route
	for rows.Next() {
		var route model.Route
		err := rows.Scan(
			&route.ID,
			&route.ServiceID,
			&route.Name,
			&route.Host,
			&route.PathPattern,
			&route.PathMatchType,
			&route.Methods,
			&route.Enabled,
			&route.CreatedAt,
			&route.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan route row failed: %w", err)
		}
		routes = append(routes, route)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	// 如果没有数据，返回空切片（更符合直觉）
	if routes == nil {
		return []model.Route{}, nil
	}
	return routes, nil
}

// Update 按 ID 更新路由的可变字段。
func (r *RouteStore) Update(ctx context.Context, route *model.Route) error {
	cmdTag, err := r.pool.Exec(ctx,
		`UPDATE gateway_routes 
		 SET name = $1, 
		 	host = $2,
		     path_pattern = $3, 
		     path_match_type = $4, 
		     methods = $5, 
		     enabled = $6, 
		     updated_at = now() 
		 WHERE id = $7`,
		route.Name, route.Host, route.PathPattern, route.PathMatchType,
		route.Methods, route.Enabled, route.ID,
	)
	if err != nil {
		return fmt.Errorf("update route failed: %w", err)
	}

	// 判断是否真的更新到了数据
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("update route failed: record not found")
	}
	return nil
}

// Delete 按 ID 删除路由。
func (r *RouteStore) Delete(ctx context.Context, id int64) error {
	cmdTag, err := r.pool.Exec(ctx,
		`DELETE FROM gateway_routes WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("delete route failed: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("delete route failed: record not found")
	}
	return nil
}

// CountByService 返回某服务被多少条路由引用（用于删除服务前的预检）。
func (r *RouteStore) CountByService(ctx context.Context, serviceID int64) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM gateway_routes WHERE service_id = $1`, serviceID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count routes by service failed: %w", err)
	}
	return n, nil
}
