package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/bobdfy/flowgate/internal/model"
)

// CreateTenant 处理 POST /api/v1/tenants
// 请求体：{"name":"...","status":"..."}
func (h *Handler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	var t model.Tenant
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// 默认值与状态白名单抽到 validate.go，Create / Update 共用同一份规则。
	if err := validateTenantForWrite(&t); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.tenants.Create(r.Context(), &t); err != nil {
		log.Println("create tenant error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// ListTenants 处理 GET /api/v1/tenants
func (h *Handler) ListTenants(w http.ResponseWriter, r *http.Request) {
	tenants, err := h.tenants.List(r.Context())
	if err != nil {
		log.Println("list tenants error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, tenants)
}

// GetTenant 处理 GET /api/v1/tenants/{id}
func (h *Handler) GetTenant(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	t, err := h.tenants.GetByID(r.Context(), id)
	if err != nil {
		log.Println("get tenant failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if t == nil {
		writeError(w, http.StatusNotFound, "tenant not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// UpdateTenant 处理 PUT /api/v1/tenants/{id}
//
// ★ 这里修掉一个真实缺陷：原先 Update 不做默认值填充，
// 但 SQL 会写入 status 列，于是 PUT 一个不带 status 的 body
// 会把状态写成空串 —— 鉴权侧只认 "active"，
// 结果是「改一下租户名字，名下所有 API Key 全部失效」。
// 现在和 Create 共用 validateTenantForWrite（补默认值 + 状态白名单）。
func (h *Handler) UpdateTenant(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var t model.Tenant
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	t.ID = id

	if err := validateTenantForWrite(&t); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.tenants.Update(r.Context(), &t); err != nil {
		log.Println("update tenant failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// DeleteTenant 处理 DELETE /api/v1/tenants/{id}
func (h *Handler) DeleteTenant(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.tenants.Delete(r.Context(), id); err != nil {
		log.Println("delete tenant failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
