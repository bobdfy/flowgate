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
	"syscall"
	"time"

	"github.com/bobdfy/flowgate/internal/api"
	"github.com/bobdfy/flowgate/internal/store/postgres"
	"github.com/joho/godotenv"
)

func main() {
	// 加载 .env（与 gateway 保持一致）
	godotenv.Load()

	// 命令行参数：数据库连接串 + 监听地址
	dsn := flag.String("dsn", "", "数据库连接字符串（默认读 DATABASE_URL）")
	addr := flag.String("addr", ":8092", "管理服务监听地址")
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

	apiHandler := api.NewHandler(serviceStore, nodeStore, routeStore, versionStore)

	// 注册管理 API 路由
	mux := http.NewServeMux()
	apiHandler.RegisterRoutes(mux)

	srv := &http.Server{
		Addr:    *addr,
		Handler: mux,
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
