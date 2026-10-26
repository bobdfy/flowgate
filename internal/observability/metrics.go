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
		// route 是「路由模板」（配置里的 path_pattern）或固定枚举值
		// （unmatched / auth_rejected / rate_limited / ai/chat ...），不是原始 URL：
		// 原始路径由客户端控制，直接当标签会造出无界的时间序列。
		[]string{"method", "route", "status"},
	)

	// 请求总数（Counter：只增不减）
	RequestTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flowgate_requests_total",
			Help: "请求总数",
		},
		[]string{"method", "route", "status"},
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
	// label 用有界值：service 名有限，reason 是枚举（fail_fast / queue_full /
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

	//  日志队列（logsink）
	//
	// 日志改为「单 goroutine + 有界队列」之后，队列满会丢日志。
	// 这两个指标让“丢了多少、当前积压多少”可见，而不是静默丢失。

	// 被丢弃的日志条数（队列满 / sink 已关闭 / 下游写失败）
	LogDroppedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "flowgate_log_dropped_total",
			Help: "被丢弃的日志条数（队列满、sink 已关闭或下游写失败）",
		},
	)

	// 日志队列当前积压条数
	LogQueueDepth = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "flowgate_log_queue_depth",
			Help: "日志队列当前积压条数",
		},
	)

	// ===================== PostgreSQL 连接池（pgxpool）观测 =====================
	//
	// 为什么要有这组指标：为了定位「同一进程连续压测，QPS 逐轮劣化、重启即恢复」。
	// 这个现象最可能的形态之一就是连接池问题 —— 池被打满导致排队，
	// 或冷启动时 50 个并发逐个建连。而这两件事在此之前是完全不可见的：
	// 压测报告 §4.4 记录了「每轮约有 50 个请求卡 1.1 秒」，这个数字和并发数相同，
	// 高度疑似首次建连的耗时，但当时没有数据能证实。
	//
	// ★ 采样方式：后台 goroutine 每秒读一次 pool.Stat()（快照语义，读一次成本低）。
	//   瞬时量（连接数）用 Gauge.Set；累计量（次数/耗时）只上报两次采样之间的
	//   增量，用 Counter.Add —— 这样 rate() 语义才正确，
	//   也避免直接把快照值当 Counter 用导致的口径混乱。
	//
	// ★ 判断劣化是否来自连接池，看这两个就够：
	//   - flowgate_pgxpool_empty_acquires_total：取连接时池已空、必须等待的次数。
	//     稳态应当恒为 0；它一涨，说明连接数不够或连接被长时间占用。
	//   - flowgate_pgxpool_new_conns_total：累计新建连接数。
	//     它和压测轮次同步增长，就坐实了「每轮都在重新建连」的假设。

	// 当前被占用的连接数（Gauge：看水位）
	PGPoolAcquiredConns = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "flowgate_pgxpool_acquired_conns",
			Help: "连接池中当前被占用的连接数",
		},
	)

	// 当前空闲连接数
	PGPoolIdleConns = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "flowgate_pgxpool_idle_conns",
			Help: "连接池中当前空闲的连接数",
		},
	)

	// 池中连接总数（= 占用 + 空闲 + 正在建立）
	PGPoolTotalConns = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "flowgate_pgxpool_total_conns",
			Help: "连接池中当前的连接总数（占用+空闲+建立中）",
		},
	)

	// 正在建立中的连接数（冷启动时这个会飙）
	PGPoolConstructingConns = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "flowgate_pgxpool_constructing_conns",
			Help: "连接池中正在建立（握手）中的连接数",
		},
	)

	// 池上限，方便在图上直接对比水位和上限
	PGPoolMaxConns = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "flowgate_pgxpool_max_conns",
			Help: "连接池上限",
		},
	)

	// 取连接成功的累计次数
	PGPoolAcquiresTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "flowgate_pgxpool_acquires_total",
			Help: "从连接池取连接成功的累计次数",
		},
	)

	// ★ 关键指标：取连接时池已空、需要等待别人释放或新建连接的累计次数。
	// 稳态下应当恒为 0；它开始增长 = 连接池成了瓶颈。
	PGPoolEmptyAcquiresTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "flowgate_pgxpool_empty_acquires_total",
			Help: "取连接时池已空、必须等待的累计次数（稳态应为 0，增长即表示池不够用）",
		},
	)

	// 取连接被 context 取消的累计次数（客户端断开 / 超时）
	PGPoolCanceledAcquiresTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "flowgate_pgxpool_canceled_acquires_total",
			Help: "取连接时被 context 取消的累计次数",
		},
	)

	// 累计新建连接数（冷启动假设的核心证据）
	PGPoolNewConnsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "flowgate_pgxpool_new_conns_total",
			Help: "累计新建的物理连接数（持续增长说明连接在反复建立，而非复用）",
		},
	)

	// 因超过 MaxConnLifetime 被销毁的连接数
	PGPoolDestroyedLifetimeTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "flowgate_pgxpool_destroyed_lifetime_total",
			Help: "因超过最大存活时间而被销毁的连接数",
		},
	)

	// 因超过 MaxConnIdleTime 被销毁的连接数
	PGPoolDestroyedIdleTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "flowgate_pgxpool_destroyed_idle_total",
			Help: "因空闲过久而被销毁的连接数",
		},
	)

	// 取连接的累计耗时（秒）
	PGPoolAcquireSecondsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "flowgate_pgxpool_acquire_seconds_total",
			Help: "取连接的累计耗时（秒）",
		},
	)

	// 因池空而等待的累计耗时（秒）—— 这个才是"连接不够"的直接代价
	PGPoolEmptyAcquireWaitSecondsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "flowgate_pgxpool_empty_acquire_wait_seconds_total",
			Help: "因连接池为空而等待的累计耗时（秒）",
		},
	)

	UpstreamFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flowgate_upstream_failures_total",
			Help: "记在节点头上的失败次数（客户端取消不计入）",
		},
		[]string{"service", "reason"},
	)

	// 客户端主动取消或自身超时
	ClientCancelTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flowgate_client_cancel_total",
			Help: "客户端主动取消或因自身超时中断的请求数",
		},
		[]string{"service"},
	)

	// TPM 配额在 Redis 故障下按降级策略处理的次数
	TPMDegradedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flowgate_tpm_degraded_total",
			Help: "TPM 配额在 Redis 故障时按降级策略处理的次数",
		},
		[]string{"mode"},
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
		// 日志队列
		LogDroppedTotal,
		LogQueueDepth,
		// PostgreSQL 连接池观测
		PGPoolAcquiredConns,
		PGPoolIdleConns,
		PGPoolTotalConns,
		PGPoolConstructingConns,
		PGPoolMaxConns,
		PGPoolAcquiresTotal,
		PGPoolEmptyAcquiresTotal,
		PGPoolCanceledAcquiresTotal,
		PGPoolNewConnsTotal,
		PGPoolDestroyedLifetimeTotal,
		PGPoolDestroyedIdleTotal,
		PGPoolAcquireSecondsTotal,
		PGPoolEmptyAcquireWaitSecondsTotal,
		UpstreamFailuresTotal,
		ClientCancelTotal,
		TPMDegradedTotal,
	)
}
