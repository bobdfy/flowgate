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

	// AI 流式首 token 耗时（TTFT）
	AIStreamTTFT = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "flowgate_ai_stream_ttft_seconds",
			Help:    "AI 流式首 token 耗时",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"model"},
	)

	// AI 流式总耗时
	AIStreamDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "flowgate_ai_stream_duration_seconds",
			Help:    "AI 流式总耗时",
			Buckets: prometheus.DefBuckets,
		},
	)

	// AI 流式事件总数
	AIStreamEventsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "flowgate_ai_stream_events_total",
			Help: "AI 流式事件总数",
		},
	)

	// 过载拒绝数（Counter）
	// ★ label 用有界值：service 名有限，reason 是枚举（fail_fast / queue_full /
	//   queue_timeout / queue_timeout_drop）。绝不能放 path / API Key。
	OverloadRejectedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flowgate_overload_rejected_total",
			Help: "因过载被拒绝的请求数",
		},
		[]string{"service", "reason"},
	)

	// 在途请求数（Gauge：看水位）
	InflightRequests = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flowgate_inflight_requests",
			Help: "当前在途请求数",
		},
		[]string{"service"},
	)

	// 排队深度（Gauge）
	QueueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flowgate_queue_depth",
			Help: "当前排队深度",
		},
		[]string{"service"},
	)

	// 路由级过载拒绝数(Bulkhed)
	BulkheadRejectedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flowgate_bulkhead_rejected_total",
			Help: "因路由级并发限制被拒绝的请求数",
		},
		[]string{"route", "reason"},
	)

	// 路由级在途请求数 (Bulkhead 水位)
	BulkheadInflight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flowgate_bulkhead_inflight_requests",
			Help: "路由级在途请求数",
		},
		[]string{"route"},
	)
)

func init() {
	prometheus.MustRegister(
		RequestDuration,
		RequestTotal,
		RateLimitedTotal,
		RateLimitDuration,
		CircuitState,
		AIStreamTTFT,
		AIStreamDuration,
		AIStreamEventsTotal,
		OverloadRejectedTotal,
		InflightRequests,
		QueueDepth,
		BulkheadInflight,
		BulkheadRejectedTotal,
	)
}
