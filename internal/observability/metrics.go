package observability

import "github.com/prometheus/client_golang/prometheus"

var (
	// 请求延迟（Histogram：能算 P50/P95/P99）
	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "flowgate_request_duration_seconds",
			Help:    "请求处理耗时（秒）",
			Buckets: prometheus.DefBuckets, // 默认分桶
		},
		[]string{"method", "path", "status"}, // 标签
	)

	// 请求总数（Counter：只增不减）
	RequestTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flowgate_requests_total",
			Help: "请求总数",
		},
		[]string{"method", "path", "status"},
	)

	// 限流拒绝数（Counter）
	RateLimitedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flowgate_rate_limited_total",
			Help: "被限流拒绝的请求数",
		},
		[]string{"scope"}, // scope: tenant / key / ip（不存具体 key 值，避免高基数）
	)

	// 限流决策耗时（Histogram：Redis 限流的附加延迟）
	RateLimitDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "flowgate_ratelimit_duration_seconds",
			Help:    "单次限流决策耗时",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1}, // 毫秒级分桶
		},
	)

	// 熔断状态（Gauge：可增可减）
	CircuitState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flowgate_circuit_state",
			Help: "熔断状态：0=closed, 1=open, 2=half_open",
		},
		[]string{"addr"}, // 上游地址
	)
)

func init() {
	prometheus.MustRegister(
		RequestDuration,
		RequestTotal,
		RateLimitedTotal,
		RateLimitDuration,
		CircuitState,
	)
}
