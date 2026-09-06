package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/bobdfy/flowgate/internal/model"
)

// CreateService 处理 POST /api/v1/services
// 请求体：{"name":"...","protocol":"...","connect_timeout_ms":...,"request_timeout_ms":...,"enabled":...}
func (h *Handler) CreateService(w http.ResponseWriter, r *http.Request) {
	var svc model.Service

	if err := json.NewDecoder(r.Body).Decode(&svc); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if svc.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if err := h.services.Create(r.Context(), &svc); err != nil {
		log.Println("create service error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, svc)
}

// ListServices 处理 GET /api/v1/services
func (h *Handler) ListServices(w http.ResponseWriter, r *http.Request) {
	services, err := h.services.List(r.Context())
	if err != nil {
		log.Println("list services error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, services)
}

// GetService 处理 GET /api/v1/services/{id}
// 约定：store 层"不存在返回 (nil, nil)"，所以这里用 svc == nil 判断 404。
func (h *Handler) GetService(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	svc, err := h.services.GetByID(r.Context(), id)
	if err != nil {
		log.Println("get service failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if svc == nil {
		writeError(w, http.StatusNotFound, "service not found")
		return
	}

	writeJSON(w, http.StatusOK, svc)
}

// UpdateService 处理 PUT /api/v1/services/{id}
func (h *Handler) UpdateService(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var svc model.Service
	if err := json.NewDecoder(r.Body).Decode(&svc); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// 关键：id 以 URL 为准，覆盖 body 里可能带的错误值
	svc.ID = id

	if err := h.services.Update(r.Context(), &svc); err != nil {
		log.Println("update service failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, svc)
}

// DeleteService 处理 DELETE /api/v1/services/{id}
// 删除前先检查是否还有路由引用，有则拒绝删除（返回 409）。
func (h *Handler) DeleteService(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	n, err := h.routes.CountByService(r.Context(), id)
	if err != nil {
		log.Println("count routes by service failed:", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if n > 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf("service still referenced by %d route(s); delete them first", n))
		return
	}

	if err = h.services.Delete(r.Context(), id); err != nil {
		log.Println("delete services failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
