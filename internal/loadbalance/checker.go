package loadbalance

import (
	"context"
	"net/http"
	"time"
)

// HealthChecker 定时主动健康检查器
type HealthChecker struct {
	pool     *NodePool
	interval time.Duration // 检查间隔，如 5 秒
	path     string        // 检查路径，如 "/health"
	timeout  time.Duration // 单次请求超时
	client   *http.Client
}

// 默认 5s/路径 /health/超时 2s
func NewHealthChecker(pool *NodePool) *HealthChecker {
	return &HealthChecker{
		pool:     pool,
		interval: 5 * time.Second,
		path:     "/health",
		timeout:  2 * time.Second,
		client:   &http.Client{Timeout: 2 * time.Second},
	}
}

// Start 启动健康检查循环（阻塞直到 ctx 取消，用于优雅退出）。
// time.Ticker 每隔 interval 触发一次；收到 ctx.Done() 时退出。
func (h *HealthChecker) Start(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.checkOnce()
		}
	}
}

// checkOnce 遍历池子所有节点逐个检查。
// 注意：必须包含不健康的节点——不健康的也要继续探测，才能发现它恢复了。
func (h *HealthChecker) checkOnce() {
	for _, node := range h.pool.AllNodes() {
		if node.State == StateOpen && time.Now().Before(node.OpenUntil) {
			continue
		}
		h.checkNode(node)
	}
}

// 发 GET {address}{path}，成功调 RecordResult(n,true)，失败调 RecordResult(n,false)
func (h *HealthChecker) checkNode(n *NodeState) {
	resp, err := h.client.Get(n.Address + h.path)
	if err != nil {
		h.pool.RecordResult(n, false)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		h.pool.RecordResult(n, true)
	} else {
		h.pool.RecordResult(n, false)
	}
}
