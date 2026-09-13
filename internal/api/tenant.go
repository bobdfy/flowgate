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
	if t.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if t.Status == "" {
		t.Status = "active"
	}
	if t.QPSLimit <= 0 {
		t.QPSLimit = 100
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
	if t.QPSLimit <= 0 {
		t.QPSLimit = 100
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
