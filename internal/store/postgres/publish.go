package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/jackc/pgx/v5"
)

// publishMu 串行化发布，避免并发发布算出同一个版本号。
var publishMu sync.Mutex

// ValidationError 表示发布校验失败（引用关系不完整等），handler 应映射成 HTTP 400。
type ValidationError struct {
	msg string
}

// Error 返回校验失败的描述信息，实现 error 接口。
func (e *ValidationError) Error() string {
	return e.msg
}

// PublishSnapshot 在一个事务里完成：读快照 → 校验 → 生成 items → 算 hash → 算版本号 → 写入。
// 要么全部成功提交，要么全部回滚。
func (v *VersionStore) PublishSnapshot(ctx context.Context) (*model.Version, error) {
	publishMu.Lock()
	defer publishMu.Unlock()

	tx, err := v.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	services, err := listServicesTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	routes, err := listRoutesTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	nodes, err := listNodesTx(ctx, tx)
	if err != nil {
		return nil, err
	}

	if err := validate(services, routes, nodes); err != nil {
		return nil, err
	}

	items := buildItems(services, routes, nodes)
	hash := computeHash(services, routes, nodes)

	var next int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(version),0)+1 FROM route_versions`).Scan(&next); err != nil {
		return nil, err
	}

	now := time.Now()
	ver := &model.Version{
		Version:     next,
		Status:      "published",
		ConfigHash:  hash,
		PublishedAt: &now,
	}

	if err := tx.QueryRow(ctx,
		`INSERT INTO route_versions (version, status, config_hash, published_at)
		 VALUES ($1,$2,$3,$4) RETURNING id, created_at`,
		ver.Version, ver.Status, ver.ConfigHash, ver.PublishedAt,
	).Scan(&ver.ID, &ver.CreatedAt); err != nil {
		return nil, err
	}

	for i := range items {
		items[i].VersionID = ver.ID
		if err := tx.QueryRow(ctx,
			`INSERT INTO route_version_items (version_id, resource_type, resource_id, resource_snapshot)
			 VALUES ($1,$2,$3,$4) RETURNING id`,
			ver.ID, items[i].ResourceType, items[i].ResourceID, items[i].ResourceSnapshot,
		).Scan(&items[i].ID); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return ver, nil
}

// Rollback 把某个历史版本重新发布为一个新的 published 版本（copy-forward）。
// 目标版本不存在时返回 (nil, nil)。
func (v *VersionStore) Rollback(ctx context.Context, targetID int64) (*model.Version, error) {
	publishMu.Lock()
	defer publishMu.Unlock()

	tx, err := v.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// 读目标版本元数据（复用其 config_hash）
	var hash string
	err = tx.QueryRow(ctx,
		`SELECT COALESCE(config_hash, '') FROM route_versions WHERE id = $1`,
		targetID).Scan(&hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	// 算新版本号
	var next int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(version),0)+1 FROM route_versions`).Scan(&next); err != nil {
		return nil, err
	}

	now := time.Now()
	ver := &model.Version{
		Version:     next,
		Status:      "published",
		ConfigHash:  hash,
		PublishedAt: &now,
	}

	if err := tx.QueryRow(ctx,
		`INSERT INTO route_versions (version, status, config_hash, published_at)
		 VALUES ($1,$2,$3,$4) RETURNING id, created_at`,
		ver.Version, ver.Status, ver.ConfigHash, ver.PublishedAt,
	).Scan(&ver.ID, &ver.CreatedAt); err != nil {
		return nil, err
	}

	// 复制目标版本的快照条目到新版本
	if _, err := tx.Exec(ctx,
		`INSERT INTO route_version_items (version_id, resource_type, resource_id, resource_snapshot)
		 SELECT $1, resource_type, resource_id, resource_snapshot
		 FROM route_version_items WHERE version_id = $2`,
		ver.ID, targetID); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return ver, nil
}

// validate 校验：每个启用路由引用的服务必须存在且启用，且该服务至少有一个启用实例。
func validate(services []model.Service, routes []model.Route, nodes []model.Node) error {
	byID := map[int64]model.Service{}
	for _, s := range services {
		byID[s.ID] = s
	}
	nodeCount := map[int64]int{}
	for _, n := range nodes {
		if n.Enabled {
			nodeCount[n.ServiceID]++
		}
	}

	// ★ 路由匹配规则唯一性：host + path_pattern + path_match_type + methods 不能重复。
	//
	// 为什么放在发布这里兜底（闸门 1 已经在 API 层挡了）：
	//   ① 挡住历史脏数据 —— 闸门 1 上线之前建出来的重复路由还在库里；
	//   ② 挡住任何绕过 admin API 的直接写库。
	//
	// 为什么必须挡：网关的 moreSpecific 比较 len(pattern) 的严格大于，
	// 规则相同的两条路由谁生效取决于遍历顺序（主键升序 = 先建的赢），
	// 而且完全静默 —— 使用者改了后建的那条会发现"改了没生效"。
	//
	// ★ 判定必须和 API 层（RouteStore.FindDuplicateByPattern）完全一致，
	//   用同一个 normalizeMethods —— 否则会出现"创建时放过、发布时被拒"
	//   这种自相矛盾的行为。
	//
	// ★ 不比较 enabled：停用的路由一旦启用就又会冲突，
	//   在这里挡住比等到启用时再出问题更安全。
	type routeKey struct{ host, pattern, matchType, methods string }
	seen := map[routeKey]model.Route{}
	var dupErrs []error
	for _, r := range routes {
		k := routeKey{r.Host, r.PathPattern, r.PathMatchType, normalizeMethods(r.Methods)}
		prev, ok := seen[k]
		if !ok {
			seen[k] = r
			continue
		}
		dupErrs = append(dupErrs, fmt.Errorf(
			"路由匹配规则重复: %q 与 %q 的 host=%q path=%q match=%q methods=%q 完全相同（只能保留一条，建议用 PUT 修改）",
			prev.Name, r.Name, r.Host, r.PathPattern, r.PathMatchType, r.Methods))
	}
	if len(dupErrs) > 0 {
		return &ValidationError{msg: errors.Join(dupErrs...).Error()}
	}

	for _, r := range routes {
		if !r.Enabled {
			continue
		}
		svc, ok := byID[r.ServiceID]
		if !ok {
			return &ValidationError{msg: fmt.Sprintf("路由 %q 引用的服务 %d 不存在", r.Name, r.ServiceID)}
		}
		if !svc.Enabled {
			return &ValidationError{msg: fmt.Sprintf("路由 %q 引用的服务 %q 已停用", r.Name, svc.Name)}
		}
		if nodeCount[r.ServiceID] == 0 {
			return &ValidationError{msg: fmt.Sprintf("服务 %q 没有可用实例", svc.Name)}
		}
	}
	return nil
}

// buildItems 只打包 enabled 的资源，生成快照条目。
func buildItems(services []model.Service, routes []model.Route, nodes []model.Node) []model.VersionItem {
	items := []model.VersionItem{}
	for _, s := range services {
		if !s.Enabled {
			continue
		}
		snap, _ := json.Marshal(s)
		items = append(items, model.VersionItem{ResourceType: "service", ResourceID: s.ID, ResourceSnapshot: snap})
	}
	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		snap, _ := json.Marshal(n)
		items = append(items, model.VersionItem{ResourceType: "node", ResourceID: n.ID, ResourceSnapshot: snap})
	}
	for _, r := range routes {
		if !r.Enabled {
			continue
		}
		snap, _ := json.Marshal(r)
		items = append(items, model.VersionItem{ResourceType: "route", ResourceID: r.ID, ResourceSnapshot: snap})
	}
	return items
}

// computeHash 把 services + routes + nodes 一起算哈希（旧版漏了 nodes）。
func computeHash(services []model.Service, routes []model.Route, nodes []model.Node) string {
	h := sha256.New()
	_ = json.NewEncoder(h).Encode(services)
	_ = json.NewEncoder(h).Encode(routes)
	_ = json.NewEncoder(h).Encode(nodes)
	return hex.EncodeToString(h.Sum(nil))
}

// listServicesTx 在事务内读取所有服务。
func listServicesTx(ctx context.Context, tx pgx.Tx) ([]model.Service, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, name, protocol, connect_timeout_ms, request_timeout_ms, enabled,
		        max_retries, retry_on_status, retry_backoff_ms, cb_failure_threshold, cb_cooldown_ms, cb_half_open_limit,
		        created_at, updated_at, response_header_timeout_ms, max_concurrency, queue_timeout_ms, overload_strategy
		 FROM gateway_services ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Service
	for rows.Next() {
		var s model.Service
		if err := rows.Scan(&s.ID, &s.Name, &s.Protocol, &s.ConnectTimeoutMs, &s.RequestTimeoutMs, &s.Enabled,
			&s.MaxRetries, &s.RetryOnStatus, &s.RetryBackoffMs, &s.CBFailureThreshold, &s.CBCooldownMs, &s.CBHalfOpenLimit,
			&s.CreatedAt, &s.UpdatedAt, &s.ResponseHeaderTimeoutMs, &s.MaxConcurrency, &s.QueueTimeoutMs, &s.OverloadStrategy); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// listRoutesTx 在事务内读取所有路由。
func listRoutesTx(ctx context.Context, tx pgx.Tx) ([]model.Route, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, service_id, name, host, path_pattern, path_match_type, methods, enabled, created_at, updated_at, max_concurrency
		 FROM gateway_routes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Route
	for rows.Next() {
		var r model.Route
		if err := rows.Scan(&r.ID, &r.ServiceID, &r.Name, &r.Host, &r.PathPattern, &r.PathMatchType, &r.Methods, &r.Enabled, &r.CreatedAt, &r.UpdatedAt, &r.MaxConcurrency); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// listNodesTx 在事务内读取所有实例。
func listNodesTx(ctx context.Context, tx pgx.Tx) ([]model.Node, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, service_id, address, weight, enabled, health_status, created_at, updated_at
		 FROM upstream_nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Node
	for rows.Next() {
		var n model.Node
		if err := rows.Scan(&n.ID, &n.ServiceID, &n.Address, &n.Weight, &n.Enabled, &n.HealthStatus, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
