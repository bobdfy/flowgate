// mock 是一个用于测试网关的模拟上游服务。
// 它提供正常回显、慢响应、故障响应和健康检查四类接口，
// 用于验证网关在正常转发、超时、错误透传等场景下的行为是否正确。
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"
)

func main() {
	// NewServeMux 创建一个 HTTP 路由器，
	// 用于将不同的请求路径分发到对应的处理函数。
	mux := http.NewServeMux()

	// 通用回显接口：将请求的方法、路径、查询串、请求头和请求体
	// 原样打包为 JSON 返回，用于验证网关转发的正确性。
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// 读取完整请求体
		body, _ := io.ReadAll(r.Body)

		// 组装需要回显的请求信息
		resp := map[string]any{
			"method":     r.Method,                     // 请求方法（GET/POST/...）
			"path":       r.URL.Path,                   // 请求路径（不含查询串）
			"query":      r.URL.RawQuery,               // 原始查询串（? 之后的内容）
			"headers":    r.Header,                     // 请求头
			"body":       string(body),                 // 请求体原文
			"request_id": r.Header.Get("X-Request-ID"), // 透传的请求 ID
		}

		// 声明响应类型为 JSON
		w.Header().Set("Content-Type", "application/json")
		// 自定义响应头，用于验证响应头能否正确回传
		w.Header().Set("X-Mock-Header", "from-mock-upstream")

		// 将回显内容编码为 JSON 写入响应体
		_ = json.NewEncoder(w).Encode(resp)
	})

	// 慢响应接口：故意等待 15 秒再返回，
	// 用于验证网关的响应头超时控制是否生效。
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(15 * time.Second)
		_, _ = w.Write([]byte("slow done"))
	})

	// 故障接口：固定返回 500 状态码，
	// 用于验证网关是否将上游错误状态码原样透传。
	mux.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	})

	// 健康检查接口：供后续网关的健康检查逻辑调用。
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	log.Println("mock upstream listening on :8081")

	// 启动 HTTP 服务，监听 8081 端口，由 mux 负责请求分发。
	// 服务启动失败时记录致命错误并退出。
	log.Fatal(http.ListenAndServe(":8081", mux))
}
