// gateway 是 FlowGate 的网关主程序（数据面）。
// 负责连接数据库、周期性加载最新路由快照，并把请求反向代理到上游服务。
// 执行已经发布好的配置
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/bobdfy/flowgate/internal/gateway"
	"github.com/bobdfy/flowgate/internal/loadbalance"
	"github.com/bobdfy/flowgate/internal/middleware"
	"github.com/bobdfy/flowgate/internal/ratelimit"
	"github.com/bobdfy/flowgate/internal/store/postgres"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp" // ← 新增
	"github.com/redis/go-redis/v9"
)

// main 是网关进程入口：加载配置、连接数据库、构建并周期性刷新路由表，最后启动 HTTP 服务并等待退出信号。
func main() {
	godotenv.Load()

	// 结构化日志：JSON 输出到 stdout
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

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

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})

	versionStore := postgres.NewVersionStore(db)

	router := gateway.NewRouter()
	cache := gateway.NewProxyCache()

	addr := flag.String("addr", ":8090", "监听地址")

	// ★ AI Gateway 开关与配置
	aiEnabled := flag.Bool("ai", true, "是否启用 AI Gateway（/v1/* 路径）")

	// AI 配置文件路径。文件不存在 → AI 不启用（不报错）。
	aiConfigPath := flag.String("ai-config", "ai.yaml", "AI Gateway 配置文件路径")

	burst := flag.Float64("burst", 200, "令牌桶容量，允许的最大突发请求数")
	failMode := flag.String("fail-mode", "fallback", "Redis 故障时的策略:closed / fallback / open")
	flag.Parse()
	if *burst <= 0 {
		slog.Error("invalid rate limit config", "burst", *burst)
		os.Exit(1)
	}

	local := ratelimit.NewLocalLimiter(*burst)

	redisL := ratelimit.NewRedisLimiter(rdb, 10*time.Second)

	Limiter := ratelimit.NewFallbackLimiter(redisL, local, ratelimit.FailMode(*failMode))

	apiKeyStore := postgres.NewAPIKeyStore(db)
	authenticator := middleware.NewAuthenticator(apiKeyStore)

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

	pools := map[int64]*loadbalance.NodePool{}

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
		table, err := gateway.BuildRoutes(items, cache, pools)
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

	//记录已启动的健康检查的池
	started := map[int64]bool{}
	startCheckers := func() {
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

	aiCfg, cfgErr := loadAIConfig(*aiConfigPath)
	if cfgErr != nil {
		slog.Error("ai_config_load_failed", "path", *aiConfigPath, "err", cfgErr)
		aiCfg = nil
	}
	if aiCfg == nil {
		slog.Info("ai_gateway_off", "reason", "没有可用的 AI 配置")
		aiCfg = &aiConfig{} // 空配置 → buildAIHandler 返回 nil
	}
	aiCfg.Enabled = aiCfg.Enabled && *aiEnabled

	handler := middleware.RequestID(
		middleware.Logging(
			authenticator.RequireAuth(
				middleware.RateLimit(
					Limiter, tenantKey, middleware.RateLimit(
						Limiter, apiKey, buildAIRouter(
							*aiCfg, router),
						gatewayErrorResponse,
					),
					gatewayErrorResponse,
				),
				gatewayErrorResponse,
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

	srv := &http.Server{
		Addr:    *addr,
		Handler: handler,
	}

	go func() {
		log.Println("pprof listen :6060")
		log.Println(http.ListenAndServe(":6060", nil))
	}()

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		log.Println("metrics listening on :9090")
		http.ListenAndServe(":9090", mux)
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
	log.Println("gateway 已退出")
}

// 两条链路的调用方不是同一批程序，所以 body 格式必须分开：
//
//	AI 路径   → OpenAI 错误结构。调用方是 SDK，它按 {"error":{"message":...}} 解析，
//	            给它纯文本，用户看到的是 SDK 自己抛的解析异常，而不是真正的原因。
//	普通路径  → 纯文本。调用方多半是人或内部服务，状态码就够，没必要编 JSON。
//
// 判定用 isAIRoute（cmd/gateway/ai.go），和真正的分流共用同一份事实来源，
// 不会出现"分流当成 AI、401 当成普通"这种两处漂移。
//
// 状态码原样透传，这里只换 body —— 401 还是 401，500 还是 500。
func gatewayErrorResponse(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if isAIRoute(r) {
		writeAIError(w, status, msg)
		return
	}
	http.Error(w, msg, status)
}

// writeAIError 按 OpenAI 的错误结构回一个 JSON body。
//
// 形状对齐 internal/ai/response.go 里的 ErrorResponseBody，故意不 import 它：
// 那是"上游错误"的类型，这里是"网关自己拒绝"的 body，两者生命周期不同 ——
// 以后给上游错误加字段不该顺带改掉网关的拒绝响应。
//
// type 字段用 status 映射成 OpenAI 的几个枚举值：
// 401/403 归 authentication_error，其余按状态码给通用值。
// 这个字段 SDK 只用来分类展示，填错不影响解析，但不能不填 —— 它是必填。
func writeAIError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// 和 http.Error 保持一致：这个 body 不该被任何中间层嗅探成别的类型。
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)

	body := map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    aiErrorType(status),
			"code":    status,
		},
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// 响应已经开始写了，改不了状态码，只能记一笔。
		slog.Warn("write_ai_error_failed", "err", err)
	}
}

// aiErrorType 把 HTTP 状态码映射成 OpenAI 风格的错误类型字符串。
func aiErrorType(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "authentication_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusNotFound:
		return "not_found_error"
	default:
		return "gateway_error"
	}
}
