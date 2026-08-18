// gateway 是 FlowGate 的网关主程序（V0 版本）。
// 它读取静态路由配置，将客户端请求反向代理到对应的上游服务，
// 并负责生成 Request ID、记录访问日志、控制超时。
package main

import (
	"crypto/rand"
	"flag"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 对应 config.yaml 的整体结构。
type Config struct {
	Routes []Route `yaml:"routes"`
}

// Route 表示一条静态路由规则：路径 → 上游地址。
type Route struct {
	Path     string `yaml:"path"`
	Upstream string `yaml:"upstream"`
}

func main() {
	// 【填空 1】用 flag 包定义"配置文件路径"参数（下一行 addr 是现成示例，照着写）
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	addr := flag.String("addr", ":8080", "网关监听地址")
	flag.Parse() // 【填空 2】解析命令行参数

	// 【填空 3】读取配置文件内容到 data
	data, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("读取配置失败: %v", err)
	}

	// 【填空 4】把 YAML 内容解析进 Config 结构体（注意要传指针 &cfg）
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("解析配置失败: %v", err)
	}

	// 为每条路由注册一个反向代理
	mux := http.NewServeMux() // 【填空 5】创建路由器
	for _, r := range cfg.Routes {
		// 【填空 6】把上游地址字符串解析成 URL 对象
		u, err := url.Parse(r.Upstream)
		if err != nil {
			log.Fatalf("上游地址配置错误 %q: %v", r.Upstream, err)
		}
		mux.Handle(r.Path, newProxy(u)) // 【填空 7】注册路由：路径 → 处理器
		log.Printf("route: %s -> %s", r.Path, r.Upstream)
	}

	// 组装服务器，套上中间件：先 Request ID，再访问日志，最后路由分发
	srv := &http.Server{
		Addr:              *addr,
		Handler:           withRequestID(withLogging(mux)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("gateway listening on %s", *addr)
	// 【填空 8】启动 HTTP 服务（srv 的方法，无参数）
	log.Fatal(srv.ListenAndServe())
}

// newProxy 为一个上游地址构造反向代理。
// 它封装了"单次转发"所需的连接与超时控制，后续版本的重试/熔断将在此之外包一层治理逻辑。
func newProxy(target *url.URL) *httputil.ReverseProxy {
	// 底层连接与超时控制
	transport := &http.Transport{
		// 【填空 9】建连超时：用 net 的哪个类型来配置 3 秒建连超时？
		DialContext: (&net.____{Timeout: 3 * time.Second}).DialContext,
		// 【填空 10】"等待上游响应头"的超时字段名（10 秒）
		____: 10 * time.Second,
		// 【填空 11】"空闲连接"的超时字段名（90 秒）
		____: 90 * time.Second,
	}

	return &httputil.ReverseProxy{
		// Director 修改请求目标：把请求重定向到上游地址
		Director: func(req *http.Request) {
			req.URL.Scheme = target.____ // 【填空 12】协议（http）
			req.URL.Host = target.____   // 【填空 13】主机:端口（localhost:8081）
			req.Host = target.Host
		},
		Transport: transport,
		// 上游出错（连接失败、超时等）时的兜底处理
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("代理错误: %v", err)
			http.Error(w, "Bad Gateway", http.____) // 【填空 14】502 状态码常量
		},
	}
}

// ---- 中间件：Request ID ----

// withRequestID 为每个请求生成或透传 Request ID。
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-ID")
		if rid == "" {
			rid = newID()
			r.Header.Set("X-Request-ID", rid)
		}
		w.Header().Set("X-Request-ID", rid)
		// 【填空 15】调用下一个处理器，让链路继续（方法名）
		next.____(w, r)
	})
}

// newID 生成一个 16 位十六进制随机 ID。
func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	// 【填空 16】把字节切片编码成十六进制字符串
	return hex.____(b)
}

// ---- 中间件：访问日志 ----

// statusRecorder 包装 ResponseWriter，在写响应的同时记录状态码。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader 记录状态码后，再委托给原始 ResponseWriter。
func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	// 【填空 17】调用"被嵌入的原始 ResponseWriter"的 WriteHeader
	r.____.WriteHeader(code)
}

// Flush 透传 Flusher 接口，避免破坏流式响应（后续 SSE 会用到）。
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// withLogging 记录每个请求的方法、路径、状态码和耗时。
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%s)",
			r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}
