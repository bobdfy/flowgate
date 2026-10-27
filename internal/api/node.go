package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/bobdfy/flowgate/internal/model"
)

// CreateNode 处理 POST /api/v1/services/{id}/nodes
// 请求体：{"address":"...","weight":...,"enabled":...}
func (h *Handler) CreateNode(w http.ResponseWriter, r *http.Request) {
	serviceID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var node model.Node
	if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// 关键：serviceID 来自 URL，覆盖 body
	node.ServiceID = serviceID

	// 地址合法性（含 SSRF 拦截）走统一实现，Create / Update 共用同一份规则。
	if err := validateNodeForWrite(&node); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.nodes.Create(r.Context(), &node); err != nil {
		log.Println("create node error", err)
		writeError(w, http.StatusInternalServerError, "internal node error")
		return
	}

	writeJSON(w, http.StatusCreated, node)
}

// ListNodes 处理 GET /api/v1/services/{id}/nodes
func (h *Handler) ListNodes(w http.ResponseWriter, r *http.Request) {
	serviceID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	nodes, err := h.nodes.ListByService(r.Context(), serviceID)
	if err != nil {
		log.Println("query nodes failed", err)
		writeError(w, http.StatusInternalServerError, "internal node error")
		return
	}

	writeJSON(w, http.StatusOK, nodes)
}

// UpdateNode 处理 PUT /api/v1/nodes/{id}
//
// ★ 这里原先把 body 直接入库、零校验，有两个后果：
//  1. CreateNode 的 SSRF 拦截（内网地址）可以被绕过 ——
//     先 POST 一个合规地址，再 PUT 成 http://169.254.169.254 即可打到云元数据服务；
//  2. PUT 是全量覆盖语义，body 里不带 address 就会把地址写成空串，
//     路由仍然认为这条节点可用，转发时报 502 且很难排查。
//
// 修法：和 Create 共用 validateNodeForWrite。
func (h *Handler) UpdateNode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var node model.Node
	if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	node.ID = id

	if err := validateNodeForWrite(&node); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err = h.nodes.Update(r.Context(), &node); err != nil {
		log.Println("update node failed", err)
		writeError(w, http.StatusInternalServerError, "internal node error")
		return
	}

	writeJSON(w, http.StatusOK, node)
}

// DeleteNode 处理 DELETE /api/v1/nodes/{id}
func (h *Handler) DeleteNode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err = h.nodes.Delete(r.Context(), id); err != nil {
		log.Println("delete node failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
