package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bobdfy/flowgate/internal/loadbalance"
	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/observability"
	"github.com/bobdfy/flowgate/internal/overload"
)

type routeEntry struct {
	host      string
	pattern   string
	matchType string
	methods   map[string]bool
	backend   *Backend

	// 路由级并发护栏
	// 保护同一服务上的其他路由
	guard *overload.Guard
}

// RouteTable 是路由表（有序候选列表），导出供 main 引用。
type RouteTable []*routeEntry

// BuildRoutes 从版本快照条目构建 path → Backend 的路由表。
// 分三遍遍历：先收集服务、再收集实例、最后用路由把两者拼装起来。
func BuildRoutes(items []model.VersionItem, cache *ProxyCache, pools map[int64]*loadbalance.NodePool) (RouteTable, error) {

	services := map[int64]model.Service{}
	for _, item := range items {
		if item.ResourceType != "service" {
			continue
		}
		var svc model.Service
		if err := json.Unmarshal(item.ResourceSnapshot, &svc); err != nil {
			return nil, fmt.Errorf("解析 service 快照失败: %w", err)
		}
		services[svc.ID] = svc
	}

	nodeSpecs := map[int64][]loadbalance.NodeSpec{}
	for _, item := range items {
		if item.ResourceType != "node" {
			continue
		}
		var node model.Node
		if err := json.Unmarshal(item.ResourceSnapshot, &node); err != nil {
			return nil, fmt.Errorf("解析 node 快照失败: %w", err)
		}

		if !node.Enabled {
			continue
		}

		nodeSpecs[node.ServiceID] = append(nodeSpecs[node.ServiceID], loadbalance.NodeSpec{
			Address: node.Address,
			Weight:  node.Weight,
		})
	}

	// serviceID → 后端，保证同一服务共享同一套负载均衡器
	backends := map[int64]*Backend{}

	entries := RouteTable{}
	for _, item := range items {
		if item.ResourceType != "route" {
			continue
		}
		var route model.Route
		if err := json.Unmarshal(item.ResourceSnapshot, &route); err != nil {
			return nil, fmt.Errorf("解析 route 快照失败: %w", err)
		}
		svc, ok := services[route.ServiceID]
		if !ok {
			continue
		}
		if !route.Enabled {
			continue
		}
		specs := nodeSpecs[route.ServiceID]
		if len(specs) == 0 {
			continue
		}

		// 同一服务的多个路由复用同一个 Backend（游标状态要共享）
		backend, ok := backends[route.ServiceID]
		if !ok {
			pool, hasPool := pools[route.ServiceID]
			if hasPool {
				pool.SyncNodes(specs)
			} else {
				pool = loadbalance.NewNodePool(specs)
				pools[route.ServiceID] = pool
			}
			pool.SetCBConfig(svc.CBFailureThreshold, svc.CBCooldownMs, svc.CBHalfOpenLimit)

			lb := loadbalance.NewRoundRobin(pool)
			backend = &Backend{svc: svc, lb: lb, pool: pool, proxyCache: cache,
				guard: overload.NewGuard(svc.MaxConcurrency,
					overload.Strategy(svc.OverloadStrategy),
					time.Duration(svc.QueueTimeoutMs)*time.Millisecond,
					overload.DefaultQueueCapacity(svc.MaxConcurrency))}
			backends[route.ServiceID] = backend
			pools[route.ServiceID] = pool
		}
		entries = append(entries, &routeEntry{
			host:      route.Host,
			pattern:   route.PathPattern,
			matchType: route.PathMatchType,
			methods:   parseMethods(route.Methods),
			backend:   backend,

			// 路由级固定fail-fast
			guard: overload.NewGuard(
				route.MaxConcurrency,
				overload.StrategyFailFast,
				0,
				0,
			),
		})
	}
	return entries, nil
}

// Router 是动态路由器：请求进来时查当前路由表，转发至对应上游。
// 路由表用 atomic.Value 持有，配置刷新时整个替换，读请求无锁。
type Router struct {
	table atomic.Value // 存 RouteTable
}

// NewRouter 创建一个空路由器。
// 创建时就存一张空表，保证 dispatch 里的 Load().(RouteTable) 永远不会拿到 nil 而 panic。
func NewRouter() *Router {
	r := &Router{}
	r.table.Store(RouteTable{})
	return r
}

// Swap 原子替换当前路由表。
func (r *Router) Swap(table RouteTable) {
	r.table.Store(table)
}

// ServeHTTP 实现 http.Handler。
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.dispatch(w, req)
}

// 遍历找匹配、按优先级选择具体的
func (r *Router) dispatch(w http.ResponseWriter, req *http.Request) {
	entries := r.table.Load().(RouteTable)
	var best *routeEntry
	for _, e := range entries {
		if e.matches(req) {
			if best == nil || e.moreSpecific(best) {
				best = e
			}
		}
	}
	if best == nil {
		http.NotFound(w, req)
		return
	}

	// ★ 两层护栏的获取顺序是硬约束：路由级先（外），服务级后（内）
	release, err := best.guard.Acquire(req.Context())
	if err != nil {
		rejectOverload(w, err, "route", best.pattern, func(reason string) {
			observability.BulkheadRejectedTotal.WithLabelValues(best.pattern, reason).Inc()
		})
		return
	}
	defer release()

	best.backend.ServeHTTP(w, req)

}

// matches 判断该路由是否匹配请求（方法 + 路径都满足）。
func (e *routeEntry) matches(req *http.Request) bool {
	if e.host != "" && req.Host != e.host {
		return false
	}
	if len(e.methods) > 0 && !e.methods[req.Method] {
		return false
	}
	if e.matchType == "exact" {
		return req.URL.Path == e.pattern
	}
	return strings.HasPrefix(req.URL.Path, e.pattern)
}

// moreSpecific 返回 e 是否比 other 更具体（优先级更高）。
func (e *routeEntry) moreSpecific(other *routeEntry) bool {
	if e.matchType != other.matchType {
		return e.matchType == "exact"
	}
	return len(e.pattern) > len(other.pattern)
}

// parseMethods 把 "GET,POST" 拆成集合；空串返回 nil（不限）。
func parseMethods(s string) map[string]bool {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	m := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			m[p] = true
		}
	}
	return m
}

// Backends 返回当前路由表里的所有后端 (已去重)
func (r *Router) Backends() []*Backend {
	entries := r.table.Load().(RouteTable)
	seen := map[*Backend]bool{}
	out := make([]*Backend, 0, len(entries))
	for _, e := range entries {
		if seen[e.backend] {
			continue
		}
		seen[e.backend] = true
		out = append(out, e.backend)
	}
	return out
}

// ReportMetrics 刷新路由级并发水位。
//
// ★ 只刷 Gauge（水位），不刷 Counter —— Counter 在拒绝时 +1，不需要轮询。
//
// ★ 已知限制：如果两条路由的 path_pattern 相同（不同 host 的同名路径），
// 它们会写同一个 label 值，后者覆盖前者。V3 范围里不处理
// （我们的路由 pattern 是唯一的），但要知道有这个边界。
func (r *Router) ReportMetrics() {
	entries := r.table.Load().(RouteTable)
	for _, e := range entries {
		st := e.guard.Stats()
		observability.BulkheadInflight.WithLabelValues(e.pattern).Set(float64(st.InFlight))
	}
}
