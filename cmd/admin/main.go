// admin 是 FlowGate 的管理服务（控制面）。
// 它连接 PostgreSQL，提供上游服务 / 实例 / 路由的管理 API。
// 控制面 admin : 管理配置
package main

import (
	"context"
	"crypto/subtle"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bobdfy/flowgate/internal/api"
	"github.com/bobdfy/flowgate/internal/store/postgres"
	"github.com/joho/godotenv"
)

// main 是管理服务进程入口：解析参数、连接数据库、注册管理 API 路由，最后启动 HTTP 服务并等待退出信号。
func main() {
	// 加载 .env（与 gateway 保持一致）
	godotenv.Load()

	// 命令行参数：数据库连接串 + 监听地址
	dsn := flag.String("dsn", "", "数据库连接字符串（默认读 DATABASE_URL）")
	addr := flag.String("addr", "127.0.0.1:8092", "管理服务监听地址（默认仅本机）")
	adminKey := flag.String("admin-key", "", "管理面静态鉴权 key（空 = 不鉴权，仅限本地开发）")
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

	apiHandler := api.NewHandler(serviceStore, nodeStore, routeStore, versionStore, tenantStore, apiKeyStore)

	// 注册管理 API 路由
	mux := http.NewServeMux()
	apiHandler.RegisterRoutes(mux)

	// S1：控制面有增删改 / publish / 返回明文 key 的权限，必须鉴权。
	var handler http.Handler = mux
	if *adminKey != "" {
		handler = requireAdminKey(*adminKey, mux)
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

// requireAdminKey 给管理面加静态鉴权：X-Admin-Key 必须与启动参数一致（常数时间比较）。
func requireAdminKey(key string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Admin-Key")), []byte(key)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
