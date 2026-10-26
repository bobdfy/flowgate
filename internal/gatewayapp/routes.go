package gatewayapp

// 路由表的热更新：把「已发布的配置版本」变成运行时路由表，并维护节点池。

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/bobdfy/flowgate/internal/gateway"
	"github.com/bobdfy/flowgate/internal/loadbalance"
	"github.com/bobdfy/flowgate/internal/store/postgres"
)

// routeRefreshInterval 是拉取新版本的间隔：管理面 publish 后最迟这么久生效。
const routeRefreshInterval = 5 * time.Second

// RouteManager 负责路由表与节点池的生命周期。
//
// 它持有 pools：路由刷新 goroutine 会写，运行状态上报 goroutine 会读，
// 所以用一把读写锁保护 map 本身（NodePool 内部另有一把锁）。
type RouteManager struct {
	store  *postgres.VersionStore
	router *gateway.Router
	cache  *gateway.ProxyCache
	hcCtx  context.Context // 健康检查的根 ctx，随进程退出取消

	mu      sync.RWMutex
	pools   map[int64]*loadbalance.NodePool
	started map[int64]bool // 已启动健康检查的服务，避免重复拉 goroutine

	version int64 // 当前已生效的配置版本
}

// NewRouteManager 创建管理器。hcCtx 取消后，所有健康检查 goroutine 与刷新循环一起退出。
func NewRouteManager(hcCtx context.Context, store *postgres.VersionStore, router *gateway.Router, cache *gateway.ProxyCache) *RouteManager {
	return &RouteManager{
		store:   store,
		router:  router,
		cache:   cache,
		hcCtx:   hcCtx,
		pools:   make(map[int64]*loadbalance.NodePool),
		started: make(map[int64]bool),
	}
}

// Load 拉取当前已发布版本并构建路由表（不切换）。
// 返回 0 + 空表表示还没有任何发布版本。
func (m *RouteManager) Load() (int64, gateway.RouteTable, error) {
	ctx := context.Background()

	ver, err := m.store.GetPublished(ctx)
	if err != nil {
		return 0, nil, err
	}
	if ver == nil {
		return 0, gateway.RouteTable{}, nil
	}

	items, err := m.store.GetItemsByVersion(ctx, ver.ID)
	if err != nil {
		return 0, nil, err
	}

	// BuildRoutes 会就地增删 pools 里的节点池，必须独占。
	m.mu.Lock()
	table, err := gateway.BuildRoutes(items, m.cache, m.pools)
	m.mu.Unlock()
	if err != nil {
		return 0, nil, err
	}

	return ver.ID, table, nil
}

// Refresh 加载一次并原子替换路由表，同时为新出现的服务启动健康检查。
func (m *RouteManager) Refresh() error {
	ver, table, err := m.Load()
	if err != nil {
		return err
	}
	m.router.Swap(table)
	m.startCheckers()
	m.version = ver
	return nil
}

// Start 后台每 5 秒查一次版本号，变了才重建路由表。
func (m *RouteManager) Start() {
	go func() {
		ticker := time.NewTicker(routeRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-m.hcCtx.Done():
				return
			case <-ticker.C:
				ver, table, err := m.Load()
				if err != nil {
					log.Printf("刷新路由失败: %v", err)
					continue
				}
				if ver == m.version {
					continue
				}
				m.router.Swap(table)
				m.startCheckers()
				m.version = ver
				log.Println("路由刷新成功")
			}
		}
	}()
}

// Pools 返回当前节点池的浅拷贝，供运行状态上报使用（拿快照而不是共享 map）。
func (m *RouteManager) Pools() map[int64]*loadbalance.NodePool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[int64]*loadbalance.NodePool, len(m.pools))
	for serviceID, pool := range m.pools {
		out[serviceID] = pool
	}
	return out
}

// startCheckers 给每个还没起过健康检查的服务拉一个 goroutine。
func (m *RouteManager) startCheckers() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for sid, pool := range m.pools {
		if m.started[sid] {
			continue
		}
		go loadbalance.NewHealthChecker(pool).Start(m.hcCtx)
		m.started[sid] = true
	}
}
