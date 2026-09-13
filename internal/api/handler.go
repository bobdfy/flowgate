// Package api 提供网关的管理 API（HTTP 层）。
// 负责解析 HTTP 请求、调用 store 层、返回 JSON 响应。
//
// 文件组织：按资源拆分 —— service.go（服务）、node.go（实例）、
// route.go（路由）、version.go（发布 / 版本 / 健康检查）。
// 本文件只保留 Handler 的组装、路由注册和公共响应工具。
package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/bobdfy/flowgate/internal/store/postgres"
)

// Handler 管理 API 的处理器，持有各 store 的引用。
type Handler struct {
	services *postgres.ServiceStore
	nodes    *postgres.NodeStore
	routes   *postgres.RouteStore
	versions *postgres.VersionStore
	tenants  *postgres.TenantStore
	apikeys  *postgres.APIKeyStore
}

// NewHandler 创建一个 Handler。
func NewHandler(services *postgres.ServiceStore, nodes *postgres.NodeStore, routes *postgres.RouteStore, versions *postgres.VersionStore, tenants *postgres.TenantStore, apikeys *postgres.APIKeyStore) *Handler {
	return &Handler{
		services: services,
		nodes:    nodes,
		routes:   routes,
		versions: versions,
		tenants:  tenants,
		apikeys:  apikeys,
	}
}

// RegisterRoutes 把管理 API 的路由注册到 mux。
// 使用 Go 1.22+ 的 method 路由，例如 mux.HandleFunc("POST /api/v1/services", h.CreateService)。
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// 健康检查：
	mux.HandleFunc("GET /api/v1/health", h.Health)

	// 服务：
	mux.HandleFunc("POST /api/v1/services", h.CreateService)
	mux.HandleFunc("GET /api/v1/services", h.ListServices)
	mux.HandleFunc("GET /api/v1/services/{id}", h.GetService)
	mux.HandleFunc("PUT /api/v1/services/{id}", h.UpdateService)
	mux.HandleFunc("DELETE /api/v1/services/{id}", h.DeleteService)

	// 实例：
	mux.HandleFunc("POST /api/v1/services/{id}/nodes", h.CreateNode)
	mux.HandleFunc("GET /api/v1/services/{id}/nodes", h.ListNodes)
	mux.HandleFunc("PUT /api/v1/nodes/{id}", h.UpdateNode)
	mux.HandleFunc("DELETE /api/v1/nodes/{id}", h.DeleteNode)

	// 路由：
	mux.HandleFunc("POST /api/v1/routes", h.CreateRoute)
	mux.HandleFunc("GET /api/v1/routes", h.ListRoutes)
	mux.HandleFunc("PUT /api/v1/routes/{id}", h.UpdateRoute)
	mux.HandleFunc("DELETE /api/v1/routes/{id}", h.DeleteRoute)

	// 发布与版本：
	mux.HandleFunc("POST /api/v1/publish", h.Publish)
	mux.HandleFunc("GET /api/v1/versions", h.ListVersions)
	mux.HandleFunc("GET /api/v1/versions/{id}", h.GetVersion)
	mux.HandleFunc("POST /api/v1/versions/{id}/rollback", h.Rollback)

	// 租户与 API Key（V4 身份层）：
	mux.HandleFunc("POST /api/v1/tenants", h.CreateTenant)
	mux.HandleFunc("GET /api/v1/tenants", h.ListTenants)
	mux.HandleFunc("GET /api/v1/tenants/{id}", h.GetTenant)
	mux.HandleFunc("PUT /api/v1/tenants/{id}", h.UpdateTenant)
	mux.HandleFunc("DELETE /api/v1/tenants/{id}", h.DeleteTenant)

	mux.HandleFunc("POST /api/v1/tenants/{id}/keys", h.CreateAPIKey)
	mux.HandleFunc("GET /api/v1/tenants/{id}/keys", h.ListAPIKeys)
	mux.HandleFunc("PUT /api/v1/keys/{id}", h.UpdateAPIKey)
	mux.HandleFunc("DELETE /api/v1/keys/{id}", h.DeleteAPIKey)
}

// writeJSON 把 v 序列化成 JSON 写入响应。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Println("write json error:", err)
	}
}

// writeError 返回统一格式的错误响应 {"error":"..."}。
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
