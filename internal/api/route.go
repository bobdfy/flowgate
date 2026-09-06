package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/bobdfy/flowgate/internal/model"
)

// CreateRoute 处理 POST /api/v1/routes
// 请求体：{"service_id":...,"name":"...","path_pattern":"...","path_match_type":"...","methods":"...","enabled":...}
func (h *Handler) CreateRoute(w http.ResponseWriter, r *http.Request) {
	var route model.Route

	if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if route.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if err := h.routes.Create(r.Context(), &route); err != nil {
		log.Println("create route error", err)
		writeError(w, http.StatusInternalServerError, "internal route error")
		return
	}

	writeJSON(w, http.StatusCreated, route)
}

// ListRoutes 处理 GET /api/v1/routes
func (h *Handler) ListRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := h.routes.List(r.Context())
	if err != nil {
		log.Println("list routes error", err)
		writeError(w, http.StatusInternalServerError, "internal route error")
		return
	}

	writeJSON(w, http.StatusOK, routes)
}

// UpdateRoute 处理 PUT /api/v1/routes/{id}
func (h *Handler) UpdateRoute(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var route model.Route
	if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// 关键：id 以 URL 为准
	route.ID = id

	if err := h.routes.Update(r.Context(), &route); err != nil {
		log.Println("update route failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, route)
}

// DeleteRoute 处理 DELETE /api/v1/routes/{id}
func (h *Handler) DeleteRoute(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err = h.routes.Delete(r.Context(), id); err != nil {
		log.Println("delete route failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
