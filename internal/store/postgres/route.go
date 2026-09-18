package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

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
		(service_id, name, host, path_pattern, path_match_type, methods, enabled, max_concurrency)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, max_concurrency`,
		route.ServiceID,
		route.Name,
		route.Host,
		route.PathPattern,
		route.PathMatchType,
		route.Methods,
		route.Enabled,
		route.MaxConcurrency,
	).Scan(&route.ID, &route.MaxConcurrency)

	if err != nil {
		return fmt.Errorf("create route failed: %w", err)
	}
	return nil
}

// normalizeMethods 把路由的 methods 字符串规范化，用于"语义相同"的比较。
//
// 为什么不能直接比字符串：`"GET,POST"` 和 `"POST,GET"` 语义完全一样
// （都是"只接受这两种方法"），字符串比较会把它们当成不同的路由，
// 于是漏判冲突 —— 而这正是最危险的方向（该拦的没拦）。
//
// 空串保持空串（语义是"不限方法"，和其他任何值都不同）。
func normalizeMethods(methods string) string {
	if strings.TrimSpace(methods) == "" {
		return ""
	}
	parts := strings.Split(methods, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// FindDuplicateByPattern 查找与给定匹配规则【语义相同】的另一条路由。
//
// 匹配规则 = host + path_pattern + path_match_type + methods（methods 规范化后比较）。
//
// 为什么需要它：路由匹配靠 router.go 的 moreSpecific，而那里的比较是
// len(pattern) 的【严格大于】—— 两条规则完全相同的路由，谁生效
// 取决于遍历顺序（= 主键升序 = 建得早的那条赢）。
//
// ★ 这个规则对使用者毫无意义：你改了后建的那条，生效的却是先建的那条，
// 而且不报错、无日志、无指标。所以必须在入口就禁止创建重复路由。
//
// excludeID 用于 Update 场景：改自己时不该把自己算成冲突，传 0 表示不排除。
// 返回 (0, nil) 表示没有冲突，否则返回已存在那条的 id。
//
// ★ 不比较 enabled：启用与否只影响"这条路由会不会进快照"，
// 不影响"它的匹配规则会不会和别的撞车"。停用的那条一旦启用就又会冲突，
// 所以在 Create/Update 阶段就挡住更安全。
//
// ★ 不比较 service_id：网关匹配时根本看不到它（见 routeEntry.matches），
// 三条 host/path/method 相同但指向不同服务的路由，永远只有第一条会生效。
func (r *RouteStore) FindDuplicateByPattern(ctx context.Context, host, pattern, matchType, methods string, excludeID int64) (int64, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, methods FROM gateway_routes
		 WHERE host = $1 AND path_pattern = $2 AND path_match_type = $3 AND id <> $4
		 ORDER BY id`,
		host, pattern, matchType, excludeID,
	)
	if err != nil {
		return 0, fmt.Errorf("find duplicate route failed: %w", err)
	}
	defer rows.Close()

	want := normalizeMethods(methods)
	for rows.Next() {
		var (
			id  int64
			got string
		)
		if err := rows.Scan(&id, &got); err != nil {
			return 0, fmt.Errorf("scan duplicate route failed: %w", err)
		}
		// methods 在 Go 侧规范化后再比 —— SQL 里做不了"排序后比较"。
		if normalizeMethods(got) == want {
			return id, nil
		}
	}
	return 0, rows.Err()
}

// GetByID 按 ID 查询路由；不存在时返回 (nil, nil)。
func (r *RouteStore) GetByID(ctx context.Context, id int64) (*model.Route, error) {
	var route model.Route
	err := r.pool.QueryRow(ctx,
		`SELECT id, service_id, name, host, path_pattern, path_match_type, methods, enabled, created_at, updated_at, max_concurrency
		 FROM gateway_routes 
		 WHERE id = $1`,
		id,
	).Scan(&route.ID, &route.ServiceID, &route.Name, &route.Host,
		&route.PathPattern, &route.PathMatchType, &route.Methods,
		&route.Enabled, &route.CreatedAt, &route.UpdatedAt, &route.MaxConcurrency,
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
		`SELECT id, service_id, name, host, path_pattern, path_match_type, methods, enabled, created_at, updated_at, max_concurrency
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
			&route.MaxConcurrency,
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
		     max_concurrency = $7,
		     updated_at = now() 
		 WHERE id = $8`,
		route.Name, route.Host, route.PathPattern, route.PathMatchType,
		route.Methods, route.Enabled, route.MaxConcurrency, route.ID,
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
