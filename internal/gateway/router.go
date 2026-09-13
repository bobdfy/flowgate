package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/bobdfy/flowgate/internal/loadbalance"
	"github.com/bobdfy/flowgate/internal/model"
)

type routeEntry struct {
	host      string
	pattern   string
	matchType string
	methods   map[string]bool
	backend   *Backend
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
			backend = &Backend{svc: svc, lb: lb, pool: pool, proxyCache: cache}
			backends[route.ServiceID] = backend
			pools[route.ServiceID] = pool
		}
		entries = append(entries, &routeEntry{
			host:      route.Host,
			pattern:   route.PathPattern,
			matchType: route.PathMatchType,
			methods:   parseMethods(route.Methods),
			backend:   backend,
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
