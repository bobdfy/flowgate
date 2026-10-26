package gatewayapp

// 主服务之外的辅助 HTTP 端点与服务端指标循环：Prometheus 抓取端、pprof、水位上报。

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	_ "net/http/pprof" // 注册到 DefaultServeMux，供 StartPProf 使用
	"time"

	"github.com/bobdfy/flowgate/internal/gateway"
	"github.com/bobdfy/flowgate/internal/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// StartMetricsReporter 每秒把路由级 / 服务级的并发水位刷进 Prometheus Gauge。
//
// 只刷 Gauge：Counter 在拒绝时自增，不需要轮询。
func StartMetricsReporter(ctx context.Context, router *gateway.Router) {
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				for _, b := range router.Backends() {
					b.ReportMetrics()
				}
				router.ReportMetrics()
			}
		}
	}()
}

// StartMetricsServer 启动 Prometheus 抓取端。
//
// 默认只绑回环（见 -metrics-addr）。容器里必须监听 0.0.0.0 才能被端口映射到，
// 此时打一条告警：安全性由宿主机侧的 127.0.0.1 端口绑定保证。
func StartMetricsServer(addr string) {
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		if !middleware.IsLoopbackAddr(addr) {
			slog.Warn("metrics_exposed", "addr", addr,
				"hint", "指标监听在非回环地址，请确认前面有鉴权代理")
		}
		log.Println("metrics listening on", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			Fatal("%v", err)
		}
	}()
}

// StartPProf 启动 pprof 调试端点（固定 127.0.0.1:6060）。
// 会暴露堆栈、goroutine 和内存布局，只在排障时临时开启。
func StartPProf() {
	slog.Warn("pprof_enabled", "addr", "127.0.0.1:6060",
		"hint", "调试端点会暴露堆栈、goroutine 和内存布局，用完请关闭")
	go func() {
		log.Println("pprof listen 127.0.0.1:6060")
		if err := http.ListenAndServe("127.0.0.1:6060", nil); err != nil {
			Fatal("%v", err)
		}
	}()
}
