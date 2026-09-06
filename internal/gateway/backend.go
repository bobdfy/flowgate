package gateway

import (
	"net/http"

	"github.com/bobdfy/flowgate/internal/loadbalance"
	"github.com/bobdfy/flowgate/internal/model"
)

// Backend 是一个上游服务的转发入口。
// 它内部持有一个负载均衡器，负责从该服务的多个实例里挑一个健康实例并转发。
type Backend struct {
	svc        model.Service            // 这个后端是哪个服务（含超时配置）
	lb         loadbalance.LoadBalancer // 负载均衡器：负责「这次轮到哪个实例」
	proxyCache *ProxyCache              // 挑中实例后，去哪拿转发用的 proxy
	pool       *loadbalance.NodePool
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// ServeHTTP 实现 http.Handler：挑一个健康实例，用它的地址拿 proxy 转发。
func (b *Backend) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	node := b.lb.Pick()
	if node == nil {
		// 所有实例都不健康，没有可用的上游
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	u, err := parseUpstream(node.Address)
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	b.proxyCache.get(u, b.svc).ServeHTTP(rec, req)

	// 被动健康信号：最终状态码 >= 500 记失败，否则记成功
	b.pool.RecordResult(node, rec.status < 500)
}
