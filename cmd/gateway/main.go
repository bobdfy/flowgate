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

	"github.com/bobdfy/flowgate/internal/gateway"
	"github.com/bobdfy/flowgate/internal/loadbalance"
	"github.com/bobdfy/flowgate/internal/middleware"
	"github.com/bobdfy/flowgate/internal/store/postgres"
	"github.com/joho/godotenv"
)

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

	versionStore := postgres.NewVersionStore(db)

	router := gateway.NewRouter()
	cache := gateway.NewProxyCache()

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

	// 每 5 秒刷新一次路由表。
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

	handler := middleware.RequestID(middleware.Logging(router))

	srv := &http.Server{
		Addr:    ":8090",
		Handler: handler,
	}

	// 在独立 goroutine 里监听，主 goroutine 等待退出信号。
	go func() {
		log.Println("FlowGate gateway listen :8090")
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
