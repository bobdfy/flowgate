// gateway 是 FlowGate 的网关主程序（数据面）。
// 负责连接数据库、周期性加载最新路由快照，并把请求反向代理到上游服务。
// 执行已经发布好的配置
package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bobdfy/flowgate/internal/ai"
	"github.com/bobdfy/flowgate/internal/errorlog"
	"github.com/bobdfy/flowgate/internal/gateway"
	"github.com/bobdfy/flowgate/internal/gatewayapp"
	"github.com/bobdfy/flowgate/internal/middleware"
	"github.com/bobdfy/flowgate/internal/overload"
	"github.com/bobdfy/flowgate/internal/quota"
	"github.com/bobdfy/flowgate/internal/ratelimit"
	"github.com/bobdfy/flowgate/internal/store/postgres"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

// main 是网关进程入口：加载配置、连接数据库、构建并周期性刷新路由表，最后启动 HTTP 服务并等待退出信号。
func main() {
	godotenv.Load()

	cfg := gatewayapp.ParseFlags()
	if err := cfg.Validate(); err != nil {
		gatewayapp.Fatal("启动参数不合法: %v", err)
	}

	logSink := gatewayapp.SetupLogging()

	// SSRF 策略：配置期由 admin 校验，这里负责拨号期的最后一道防线。
	upstreamPolicy, err := cfg.UpstreamPolicy()
	if err != nil {
		gatewayapp.Fatal("上游白名单不合法: %v", err)
	}
	if allowed := upstreamPolicy.Allowed(); len(allowed) > 0 {
		slog.Warn("upstream_private_allowed", "cidrs", allowed,
			"hint", "这些内网地址允许被拨号，生产环境应确认必要性")
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		gatewayapp.Fatal("DATABASE_URL 未设置")
	}
	db, err := postgres.NewPool(context.Background(), databaseURL)
	if err != nil {
		gatewayapp.Fatal("数据库连接失败: %v", err)
	}
	defer db.Close()

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		gatewayapp.Fatal("REDIS_ADDR 未设置")
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: os.Getenv("REDIS_PASSWORD"), // S6：Redis 加密码
	})

	// ---- 限流：本地令牌桶 + Redis 分布式令牌桶，按 fail-mode 组合 ----
	local := ratelimit.NewLocalLimiter(cfg.Burst)
	// 窗口 1s：qps_limit 才真的是「每秒请求数」。
	// 之前用 10s 窗口，窗口内最多 qps_limit 个请求，实际速率只有标称值的 1/10
	// （压测确认：qps_limit=5 的 key 10 秒只放行 5 个）。
	redisL := ratelimit.NewRedisLimiter(rdb, time.Second)

	// TPM 窗口：数据库是事实来源，-tpm-window 只用于首启种子。
	// 管理面改窗口后，下面的 watcher 会热切换。
	settingsStore := postgres.NewSettingsStore(db)
	window, windowSource := gatewayapp.ResolveTPMWindow(context.Background(), settingsStore, cfg.TPMWindow)

	tpm := quota.NewRedisTPMWithWindow(rdb, window)

	tpmLimiter := quota.NewFallbackTPM(tpm, quota.NewLocalTPM(tpm.Window()), quota.FailMode(cfg.TPMFailMode))

	slog.Info("tpm_window", "window", tpm.Window().String(), "source", windowSource)
	// 把窗口长度告诉管理面，TPM 用量查询才能对齐同一个窗口。
	if err := tpm.PublishWindow(context.Background()); err != nil {
		slog.Warn("tpm_window_publish_failed", "err", err)
	}
	settingsCtx, settingsCancel := context.WithCancel(context.Background())
	defer settingsCancel()
	go gatewayapp.WatchTPMWindow(settingsCtx, settingsStore, tpm)

	limiter := ratelimit.NewFallbackLimiter(redisL, local, ratelimit.FailMode(cfg.FailMode))

	// ---- 鉴权（可选进程内缓存） ----
	apiKeyStore := postgres.NewAPIKeyStore(db)

	aiConfigStore := postgres.NewAIConfigStore(db)

	var authenticator *middleware.Authenticator
	if cfg.AuthCacheTTL > 0 {
		authenticator = middleware.NewAuthenticatorWithCache(apiKeyStore, cfg.AuthCacheTTL)
		slog.Info("auth_cache_enabled", "ttl", cfg.AuthCacheTTL.String())

		// 缓存必须能被管理面改动作废，否则改白名单要等 TTL 过期。
		authCtx, authCancel := context.WithCancel(context.Background())
		defer authCancel()
		go gatewayapp.WatchAuthVersion(authCtx, db, authenticator)
	} else {
		authenticator = middleware.NewAuthenticator(apiKeyStore)
	}

	// ---- 路由表与节点池：启动时加载一次，之后后台每 5 秒跟进 ----
	hcCtx, hcCancel := context.WithCancel(context.Background())
	defer hcCancel()

	router := gateway.NewRouter()
	rm := gatewayapp.NewRouteManager(hcCtx, postgres.NewVersionStore(db), router, gateway.NewProxyCache(upstreamPolicy))
	if err := rm.Refresh(); err != nil {
		gatewayapp.Fatal("加载路由失败: %v", err)
	}
	rm.Start()

	// 观测类后台 goroutine 的根 ctx（连接池采样、指标上报）。
	// 单独一个而不是复用 hcCtx：语义不同，健康检查归健康检查、观测归观测。
	statsCtx, statsCancel := context.WithCancel(context.Background())
	defer statsCancel()

	// ---- AI Gateway ----
	aiCfg, cfgErr := gatewayapp.LoadAIConfig(cfg.AIConfigPath)
	if cfgErr != nil {
		slog.Error("ai_config_load_failed", "path", cfg.AIConfigPath, "err", cfgErr)
		aiCfg = nil
	}
	if aiCfg == nil {
		// ai.yaml 不存在也不代表 AI 关闭：DB 里的配置仍然是配置源。
		aiCfg = &gatewayapp.AIConfig{Enabled: true}
	}
	aiCfg.Enabled = cfg.AIEnabled

	// AI 用量流水：请求路径只做非阻塞入队，后台批量写库。
	// defer 顺序（逆序执行）：先 cancel 让后台把队列刷完，再 Close 等它退出。
	usageStore := postgres.NewUsageStore(db)
	usageCtx, usageCancel := context.WithCancel(context.Background())
	defer usageStore.Close()
	defer usageCancel()
	usageStore.Start(usageCtx)

	// DB 是配置的事实来源；ai.yaml 只做首次种子和 DB 不可用时的回退。
	aiCtx, aiCancel := context.WithCancel(context.Background())
	defer aiCancel()

	aiRT := gatewayapp.NewAIRuntime(aiCtx, *aiCfg, tpmLimiter, aiConfigStore, usageStore, upstreamPolicy, ai.Timeouts{
		Connect:        cfg.AIConnectTimeout,
		ResponseHeader: cfg.AIResponseHeaderTimeout,
		Buffered:       cfg.AIBufferedTimeout,
		StreamIdle:     cfg.AIStreamIdleTimeout,
		StreamMax:      cfg.AIStreamMaxDuration,
	})
	aiRT.Start(aiCtx)

	// 节点健康 + AI 加载结果只在本进程内存里，定期写 Redis 给管理面读取。
	instance, _ := os.Hostname()
	if instance == "" {
		instance = "gateway"
	}
	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	defer runtimeCancel()
	go gatewayapp.PublishRuntimeStatus(runtimeCtx, rdb, instance, rm.Pools, aiRT.Status)

	// ---- 错误看板 ----
	// 失败请求非阻塞写入 Redis：多实例共用、最多保留 500 种错误（按签名聚合）。
	// Redis 不可用时丢弃并计数（flowgate_errorlog_dropped_total），不影响请求。
	// dial 队列 256 条：错误是低频事件，够吸收一次 Redis 抖动的峰值。
	if err := resetErrorlogKeys(rdb); err != nil {
		slog.Warn("errorlog_reset_legacy_failed", "err", err)
	}
	errorRecorder := errorlog.NewRecorder(errorlog.NewRedisSink(rdb, errorlog.Key, errorlog.Cap), 256)
	defer errorRecorder.Close()

	// ---- 中间件链 ----
	// 网关自己拒绝请求时回什么 body：AI 路径回 OpenAI 错误结构，其余回纯文本。
	errorResponder := middleware.OpenAIErrorResponder(gatewayapp.IsAIRoute)

	guard := overload.NewGuard(cfg.MaxInflight, overload.StrategyFailFast, 0, 0)

	handler := middleware.RequestID(
		middleware.LoggingWithErrors(errorRecorder, instance,
			middleware.Inflight(guard, "instance")(
				authenticator.RequireAuth(
					middleware.RateLimitPair(
						limiter, gatewayapp.PairKey, gatewayapp.AIRouter(aiRT.Handler, router),
						errorResponder,
					),
					errorResponder,
				),
			),
		),
	)

	state := gatewayapp.NewState()

	root := http.NewServeMux()
	root.HandleFunc("/healthz", state.Healthz)
	root.HandleFunc("/readyz", state.Readyz)
	root.Handle("/", handler) // 其余全部走原链路

	gatewayapp.StartMetricsReporter(statsCtx, router)

	// H1-a：连接池采样（每秒一次 pool.Stat()）。
	// 用于定位"连续压测 QPS 逐轮劣化、重启即恢复"——如果劣化来自连接池，
	go postgres.StartPoolStats(statsCtx, db, time.Second)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           root,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	if cfg.PProf {
		gatewayapp.StartPProf()
	}
	gatewayapp.StartMetricsServer(cfg.MetricsAddr)

	state.MarkReady()

	// 在独立 goroutine 里监听，主 goroutine 等待退出信号。
	go func() {
		log.Println("FlowGate gateway listen ", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			gatewayapp.Fatal("gateway 启动失败: %v", err)
		}
	}()

	// 等待 Ctrl+C 或 SIGTERM。
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()

	log.Println("收到退出信号，停止接收新流量")
	state.MarkDraining()

	if cfg.DrainDelay > 0 {
		time.Sleep(cfg.DrainDelay)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)

	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("gateway 优雅关闭失败: %v", err)
	}

	// 先排干错误看板队列：最后几条往往就是「为什么刚才挂了」的答案。
	// 放在日志 sink 之前关闭，避免排干过程中的告警打不进日志。
	errorRecorder.Close()

	// 关闭日志 sink：排干队列并做最后一次 flush。
	gatewayapp.ShutdownLogging(logSink)
}

// resetErrorlogKeys 清掉旧版本（LIST 结构）遗留的错误看板 key。
// 类型不匹配会让后续所有写入 WRONGTYPE 失败、看板静默空白，所以启动时先修一次。
// 失败（比如 Redis 还没就绪）只告警不阻塞启动：看板是旁路功能，不能拦住网关。
func resetErrorlogKeys(rdb *redis.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return errorlog.ResetLegacy(ctx, rdb, errorlog.Key)
}
