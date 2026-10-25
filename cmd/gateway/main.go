// gateway 是 FlowGate 的网关主程序（数据面）。
// 负责连接数据库、周期性加载最新路由快照，并把请求反向代理到上游服务。
// 执行已经发布好的配置
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/bobdfy/flowgate/internal/gateway"
	"github.com/bobdfy/flowgate/internal/gatewayapp"
	"github.com/bobdfy/flowgate/internal/loadbalance"
	"github.com/bobdfy/flowgate/internal/logsink"
	"github.com/bobdfy/flowgate/internal/middleware"
	"github.com/bobdfy/flowgate/internal/quota"
	"github.com/bobdfy/flowgate/internal/ratelimit"
	"github.com/bobdfy/flowgate/internal/store/postgres"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp" // ← 新增
	"github.com/redis/go-redis/v9"
)

// main 是网关进程入口：加载配置、连接数据库、构建并周期性刷新路由表，最后启动 HTTP 服务并等待退出信号。
func main() {
	godotenv.Load()

	// JSON 输出到 stdout (把日志系统输出成 JSON 格式)
	//
	// ★ 性能关键：不能直接写 os.Stdout。slog 每次日志都是一次 syscall，
	// 压测时 access_log 每请求写一次 stdout 会变成最大 CPU 热点（实测 ~30%）。
	// logsink 内部用 bufio.Writer 把小块 JSON 攒成 64KB 批量落盘，单 goroutine 定时 flush；
	// 请求 goroutine 只做拷贝 + 非阻塞入队，既避免了 bufio 的并发竞态，
	// 也不会被慢速 stdout 阻塞（队列满时丢弃并计数）。
	logSink := logsink.New(os.Stdout, 64*1024, 4096, 200*time.Millisecond)
	slog.SetDefault(slog.New(slog.NewJSONHandler(logSink, nil)))

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL 未设置")
	}

	db, err := postgres.NewPool(context.Background(), databaseURL)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	defer db.Close()

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		log.Fatal("REDIS_ADDR 未设置")
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: os.Getenv("REDIS_PASSWORD"), // S6：Redis 加密码
	})

	versionStore := postgres.NewVersionStore(db)
	router := gateway.NewRouter()
	cache := gateway.NewProxyCache()

	addr := flag.String("addr", ":8090", "监听地址")

	//  AI Gateway 开关与配置
	aiEnabled := flag.Bool("ai", true, "是否启用 AI Gateway(/v1/* 路径)")
	// AI 配置文件路径。文件不存在 → AI 不启用(不报错)。
	aiConfigPath := flag.String("ai-config", "ai.yaml", "AI Gateway 配置文件路径")
	burst := flag.Float64("burst", 200, "令牌桶容量，允许的最大突发请求数")
	failMode := flag.String("fail-mode", "fallback", "Redis 故障时的策略:closed / fallback / open")
	authCacheTTL := flag.Duration("auth-cache-ttl", 0, "鉴权结果进程内缓存 TTL（>0 启用；0 关闭）")
	tpmWindow := flag.Duration("tpm-window", time.Minute, "TPM 配额窗口长度（如 1m / 5m / 1h）")
	pprof := flag.Bool("pprof", false, "启用 pprof 调试端点(默认关闭, 仅 127.0.0.1)")
	flag.Parse()

	if *burst <= 0 {
		slog.Error("invalid rate limit config", "burst", *burst)
		os.Exit(1)
	}
	if *tpmWindow < time.Second {
		slog.Error("invalid tpm window", "tpm_window", tpmWindow.String())
		os.Exit(1)
	}

	local := ratelimit.NewLocalLimiter(*burst)

	redisL := ratelimit.NewRedisLimiter(rdb, 10*time.Second)

	// TPM 窗口：数据库是事实来源，-tpm-window 只用于首启种子。
	// 管理面改窗口后，下面的 watcher 会热切换。
	settingsStore := postgres.NewSettingsStore(db)
	window, windowSource := gatewayapp.ResolveTPMWindow(context.Background(), settingsStore, *tpmWindow)
	tpm := quota.NewRedisTPMWithWindow(rdb, window)
	slog.Info("tpm_window", "window", tpm.Window().String(), "source", windowSource)
	// 把窗口长度告诉管理面，TPM 用量查询才能对齐同一个窗口。
	if err := tpm.PublishWindow(context.Background()); err != nil {
		slog.Warn("tpm_window_publish_failed", "err", err)
	}
	settingsCtx, settingsCancel := context.WithCancel(context.Background())
	defer settingsCancel()
	go gatewayapp.WatchTPMWindow(settingsCtx, settingsStore, tpm)

	Limiter := ratelimit.NewFallbackLimiter(redisL, local, ratelimit.FailMode(*failMode))

	apiKeyStore := postgres.NewAPIKeyStore(db)
	aiConfigStore := postgres.NewAIConfigStore(db)

	var authenticator *middleware.Authenticator
	if *authCacheTTL > 0 {
		authenticator = middleware.NewAuthenticatorWithCache(apiKeyStore, *authCacheTTL)
		slog.Info("auth_cache_enabled", "ttl", authCacheTTL.String())

		// 缓存必须能被管理面改动作废，否则改白名单要等 TTL 过期。
		authCtx, authCancel := context.WithCancel(context.Background())
		defer authCancel()
		go gatewayapp.WatchAuthVersion(authCtx, db, authenticator)
	} else {
		authenticator = middleware.NewAuthenticator(apiKeyStore)
	}

	tenantKey := func(r *http.Request) (string, int64) {
		if id, ok := middleware.FromContext(r.Context()); ok {
			return "tenant:" + strconv.FormatInt(id.TenantID, 10), id.TenantQPS
		}
		return "anonymous", 100
	}

	apiKey := func(r *http.Request) (string, int64) {
		if id, ok := middleware.FromContext(r.Context()); ok {
			return "key:" + strconv.FormatInt(id.KeyID, 10), id.KeyQPS
		}
		return "anonymous", 100
	}

	// p租户级 + Key 级合并
	// 顺序：key1=租户（先判、更粗），key2=Key（后判、更细）。
	pairKey := func(r *http.Request) (string, int64, string, int64) {
		k1, l1 := tenantKey(r)
		k2, l2 := apiKey(r)
		return k1, l1, k2, l2
	}

	pools := map[int64]*loadbalance.NodePool{}
	// 路由刷新 goroutine 会写 pools，运行状态上报 goroutine 会读；
	// 用读写锁把 map 本身保护起来（NodePool 内部另有一把锁）。
	var poolsMu sync.RWMutex

	// loadTable 拉取当前已发布版本，构建 path → proxy 路由表。
	loadTable := func() (int64, gateway.RouteTable, error) {
		ver, err := versionStore.GetPublished(context.Background())
		if err != nil {
			return 0, nil, err
		}
		if ver == nil {
			return 0, gateway.RouteTable{}, nil
		}

		items, err := versionStore.GetItemsByVersion(context.Background(), ver.ID)
		if err != nil {
			return 0, nil, err
		}

		poolsMu.Lock()
		table, err := gateway.BuildRoutes(items, cache, pools)
		poolsMu.Unlock()
		if err != nil {
			return 0, nil, err
		}
		return ver.ID, table, nil
	}

	lastVersion, table, err := loadTable()
	if err != nil {
		log.Fatalf("加载路由失败: %v", err)
	}
	router.Swap(table)

	hcCtx, hcCancel := context.WithCancel(context.Background())
	defer hcCancel()

	// 观测类后台 goroutine 的根 ctx（连接池采样等）。
	// 单独一个而不是复用 hcCtx：语义不同，健康检查归健康检查、观测归观测，
	statsCtx, statsCancel := context.WithCancel(context.Background())
	defer statsCancel()

	//记录已启动的健康检查的池
	started := map[int64]bool{}
	startCheckers := func() {
		poolsMu.RLock()
		defer poolsMu.RUnlock()
		for sid, pool := range pools {
			if !started[sid] {
				go loadbalance.NewHealthChecker(pool).Start(hcCtx)
				started[sid] = true
			}
		}
	}
	startCheckers()

	// 每 5 秒检查一次路由表, 发生改变就刷新
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		for range ticker.C {
			ver, table, err := loadTable()
			if err != nil {
				log.Printf("刷新路由失败: %v", err)
				continue
			}
			if ver == lastVersion {
				continue
			}
			router.Swap(table)
			startCheckers()
			lastVersion = ver
			log.Println("路由刷新成功")
		}
	}()

	aiCfg, cfgErr := gatewayapp.LoadAIConfig(*aiConfigPath)
	if cfgErr != nil {
		slog.Error("ai_config_load_failed", "path", *aiConfigPath, "err", cfgErr)
		aiCfg = nil
	}
	if aiCfg == nil {
		// ai.yaml 不存在也不代表 AI 关闭：DB 里的配置仍然是配置源。
		aiCfg = &gatewayapp.AIConfig{Enabled: true}
	}
	aiCfg.Enabled = *aiEnabled

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
	aiRT := gatewayapp.NewAIRuntime(aiCtx, *aiCfg, tpm, aiConfigStore, usageStore)
	aiRT.Start(aiCtx)

	// 节点健康 + AI 加载结果只在本进程内存里，定期写 Redis 给管理面读取。
	instance, _ := os.Hostname()
	if instance == "" {
		instance = "gateway"
	}
	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	defer runtimeCancel()
	snapshotPools := func() map[int64]*loadbalance.NodePool {
		poolsMu.RLock()
		defer poolsMu.RUnlock()
		out := make(map[int64]*loadbalance.NodePool, len(pools))
		for serviceID, pool := range pools {
			out[serviceID] = pool
		}
		return out
	}
	go gatewayapp.PublishRuntimeStatus(runtimeCtx, rdb, instance, snapshotPools, aiRT.Status)

	// 网关自己拒绝请求时回什么 body：AI 路径回 OpenAI 错误结构，其余回纯文本。
	errorResponder := middleware.OpenAIErrorResponder(gatewayapp.IsAIRoute)

	handler := middleware.RequestID(
		middleware.Logging(
			authenticator.RequireAuth(
				middleware.RateLimitPair(
					Limiter, pairKey, gatewayapp.AIRouter(aiRT.Handler, router),
					errorResponder,
				),
				errorResponder,
			),
		),
	)

	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for range tick.C {
			for _, b := range router.Backends() {
				b.ReportMetrics()
			}
			router.ReportMetrics()
		}
	}()

	// H1-a：连接池采样（每秒一次 pool.Stat()）。
	// 用于定位"连续压测 QPS 逐轮劣化、重启即恢复"——如果劣化来自连接池，
	go postgres.StartPoolStats(statsCtx, db, time.Second)

	srv := &http.Server{
		Addr:    *addr,
		Handler: handler,
	}

	if *pprof {
		go func() {
			log.Println("pprof listen 127.0.0.1:6060")
			if err := http.ListenAndServe("127.0.0.1:6060", nil); err != nil {
				log.Fatal(err)
			}
		}()
	}

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		log.Println("metrics listening on :9090")
		if err := http.ListenAndServe(":9090", mux); err != nil {
			log.Fatal(err)
		}
	}()

	// 在独立 goroutine 里监听，主 goroutine 等待退出信号。
	go func() {
		log.Println("FlowGate gateway listen ", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("gateway 启动失败: %v", err)
		}
	}()

	// 等待 Ctrl+C 或 SIGTERM。
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()

	log.Println("收到退出信号，开始优雅关闭 gateway ...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("gateway 优雅关闭失败: %v", err)
	}
	// 关闭日志 sink：排干队列并做最后一次 flush。
	//
	// 注意两点：
	//   1. 当前 Go 版本里 slog.SetDefault 会把标准库 log 也桥接到 slog，
	//      所以 Close 之后再 log 的内容会被丢弃（计入 dropped）；
	//      退出类信息必须在 Close 之前发出，由 Close 负责刷盘。
	//   2. 函数末尾的 deferred cancel 会在这之后才停掉健康检查等后台 goroutine，
	//      它们若再写日志同样计入 dropped，不影响正确性。
	if n := logSink.Dropped(); n > 0 {
		slog.Warn("log_queue_overflow", "dropped", n)
	}
	slog.Info("gateway 已退出")
	if err := logSink.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "日志落盘失败: %v\n", err)
	}
}
