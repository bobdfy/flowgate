// gateway 是 FlowGate 的网关主程序（数据面）。
// 负责连接数据库、周期性加载最新路由快照，并把请求反向代理到上游服务。
// 执行已经发布好的配置
package main

import (
	"context"
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

	handler := middleware.RequestID(
		middleware.Logging(
			authenticator.RequireAuth(
				middleware.RateLimit(
					Limiter, tenantKey, middleware.RateLimit(
						Limiter, apiKey, router,
					)),
			),
		),
	)

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
