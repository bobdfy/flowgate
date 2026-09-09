// mock 是一个用于测试网关的模拟上游服务。
// 提供回显、慢响应、故障响应、健康检查四类接口，
// 并支持通过 /__fault 接口做可控故障注入（延迟 / 状态码 / 概率），
// 用于验证网关在正常转发、超时、错误透传、偶发故障等场景下的行为。
package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"time"
)

// main 启动 mock 模拟上游服务，注册回显/慢响应/故障/健康检查等测试接口并开始监听。
func main() {
	addr := flag.String("addr", ":8091", "mock监听地址")
	flag.Parse()
	// 故障规则表：所有业务接口共享同一份
	faults := newFaultStore()

	mux := http.NewServeMux()

	// 故障管理接口：往规则表里加 / 查 / 删规则
	mux.HandleFunc("POST /__fault", faults.handleSet)
	mux.HandleFunc("GET /__fault", faults.handleList)
	mux.HandleFunc("DELETE /__fault", faults.handleDelete)

	// 通用回显接口：将请求的方法、路径、查询串、请求头和请求体
	// 原样打包为 JSON 返回，用于验证网关转发的正确性。
	mux.HandleFunc("/", withFault(faults, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		resp := map[string]any{
			"method":     r.Method,                     // 请求方法（GET/POST/...）
			"path":       r.URL.Path,                   // 请求路径（不含查询串）
			"query":      r.URL.RawQuery,               // 原始查询串（? 之后的内容）
			"headers":    r.Header,                     // 请求头
			"body":       string(body),                 // 请求体原文
			"request_id": r.Header.Get("X-Request-ID"), // 透传的请求 ID
			"addr":       *addr,
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Mock-Header", "from-mock-upstream")
		_ = json.NewEncoder(w).Encode(resp)
	}))

	// 慢响应接口：故意等待 15 秒再返回，
	// 用于验证网关的响应头超时控制是否生效。
	mux.HandleFunc("/slow", withFault(faults, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(15 * time.Second)
		_, _ = w.Write([]byte("slow done"))
	}))

	// 故障接口：固定返回 500 状态码，
	// 用于验证网关是否将上游错误状态码原样透传。
	mux.HandleFunc("/fail", withFault(faults, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))

	// 健康检查接口：供网关的健康检查逻辑调用。
	mux.HandleFunc("/health", withFault(faults, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	log.Printf("mock upstream listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
