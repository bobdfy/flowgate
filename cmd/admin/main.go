// admin 是 FlowGate 的管理服务（控制面）。
// 它连接 PostgreSQL，提供上游服务 / 实例 / 路由的管理 API。
// 控制面 admin : 管理配置
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bobdfy/flowgate/internal/adminui"
	"github.com/bobdfy/flowgate/internal/api"
	"github.com/bobdfy/flowgate/internal/quota"
	"github.com/bobdfy/flowgate/internal/store/postgres"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

func main() {
	// 加载 .env（与 gateway 保持一致）
	godotenv.Load()

	// 命令行参数：数据库连接串 + 监听地址
	dsn := flag.String("dsn", "", "数据库连接字符串（默认读 DATABASE_URL）")
	addr := flag.String("addr", "127.0.0.1:8092", "管理服务监听地址（默认仅本机）")
	adminKey := flag.String("admin-key", "", "管理面静态鉴权 key（空 = 不鉴权，仅限本地开发）")
	redisAddr := flag.String("redis", os.Getenv("REDIS_ADDR"), "Redis 地址（用于 TPM 用量查询；空 = 不启用）")
	tpmWindow := flag.Duration("tpm-window", time.Minute, "TPM 窗口长度兜底值（gateway 尚未发布窗口时使用）")
	webDir := flag.String("web-dir", "", "管理控制台静态文件目录（如 /app/web；空 = 不托管前端）")
	flag.Parse()

	// 未通过 -dsn 指定时，从环境变量读取（与 gateway 一致）
	if *dsn == "" {
		*dsn = os.Getenv("DATABASE_URL")
	}
	if *dsn == "" {
		log.Fatal("DATABASE_URL 未设置（可用 -dsn 指定或写入 .env）")
	}

	// 连接 PostgreSQL（docker-compose 把 5432 映射到宿主机 5433）
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, *dsn)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	defer pool.Close()

	// 组装 store 与 handler
	serviceStore := postgres.NewServiceStore(pool)
	nodeStore := postgres.NewNodeStore(pool)
	routeStore := postgres.NewRouteStore(pool)
	versionStore := postgres.NewVersionStore(pool)
	tenantStore := postgres.NewTenantStore(pool)
	apiKeyStore := postgres.NewAPIKeyStore(pool)
	aiConfigStore := postgres.NewAIConfigStore(pool)

	// TPM 用量查询直连 gateway 用的同一个 Redis；没配地址就关掉这个能力，
	// 其余管理接口不受影响。
	var rdb *redis.Client
	var tpm *quota.RedisTPM
	if *redisAddr != "" {
		rdb = redis.NewClient(&redis.Options{
			Addr:     *redisAddr,
			Password: os.Getenv("REDIS_PASSWORD"),
		})
		defer rdb.Close()
		tpm = quota.NewRedisTPMWithWindow(rdb, *tpmWindow)
		log.Printf("Redis 已连接（%s），TPM 用量查询已启用（兜底窗口 %s）", *redisAddr, tpmWindow.String())
	} else {
		log.Println("REDIS_ADDR 未设置，TPM 用量查询接口将返回 503")
	}

	apiHandler := api.NewHandler(serviceStore, nodeStore, routeStore, versionStore, tenantStore, apiKeyStore, aiConfigStore, tpm)

	// 注册管理 API 路由
	mux := http.NewServeMux()
	apiHandler.RegisterRoutes(mux)

	// AI 用量查询（数据由 gateway 异步写入 ai_usage）。
	usageStore := postgres.NewUsageStore(pool)
	api.NewUsageHandler(usageStore).RegisterRoutes(mux)

	// 运行时设置（TPM 窗口等），由 gateway 轮询热加载。
	settingsStore := postgres.NewSettingsStore(pool)
	api.NewSettingsHandler(settingsStore, tpm).RegisterRoutes(mux)

	// gateway 上报的节点健康与 AI 配置加载结果（只读）。
	api.NewRuntimeHandler(rdb).RegisterRoutes(mux)

	// 可选：同一端口托管管理控制台静态文件。
	// 前端请求走相对路径 /api/v1，天然同源，不需要额外 nginx。
	if *webDir != "" {
		if _, err := os.Stat(filepath.Join(*webDir, "index.html")); err != nil {
			// 没构建前端不应该拖垮管理面：只跳过静态托管。
			log.Printf("web-dir 缺少 index.html，跳过前端托管（先在 web/ 执行 pnpm build）: %v", err)
		} else {
			mux.Handle("/", adminui.SPAHandler(*webDir))
			log.Printf("管理控制台已托管: %s", *webDir)
		}
	}

	// 控制面有增删改 / publish / 返回明文 key 的权限，必须鉴权。
	var handler http.Handler = mux
	if *adminKey != "" {
		handler = adminui.RequireAdminKey(*adminKey, mux, *webDir != "")
	}

	srv := &http.Server{
		Addr:    *addr,
		Handler: handler,
	}

	// 在独立 goroutine 里监听，主 goroutine 等待退出信号
	go func() {
		log.Printf("admin listening on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("admin 启动失败: %v", err)
		}
	}()

	// 等待 Ctrl+C 或 SIGTERM
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()

	log.Println("收到退出信号，开始优雅关闭 admin ...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("admin 优雅关闭失败: %v", err)
	}
	log.Println("admin 已退出")
}
