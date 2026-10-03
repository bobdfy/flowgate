package api

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
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

	if node.Address == "" {
		writeError(w, http.StatusBadRequest, "address is required")
		return
	}
	// 地址必须带 scheme（gateway 的 parseUpstream 只兜底，这里要求完整 URL）
	u, err := url.Parse(node.Address)
	if err != nil || u.Scheme == "" || u.Host == "" {
		writeError(w, http.StatusBadRequest, "address must be a full URL, e.g. http://localhost:8091")
		return
	}
	// S2：只允许 http/https，并拦内网/回环地址，防止 SSRF（本地联调可用 ALLOW_PRIVATE_UPSTREAM=1 放开）。
	if u.Scheme != "http" && u.Scheme != "https" {
		writeError(w, http.StatusBadRequest, "address scheme must be http or https")
		return
	}
	if os.Getenv("ALLOW_PRIVATE_UPSTREAM") != "1" && isPrivateHost(u.Hostname()) {
		writeError(w, http.StatusBadRequest, "address points to a private/loopback host")
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

// isPrivateHost 判断 host 是否解析到内网 / 回环 / 链路本地地址（SSRF 防护）。
// 解析失败按私有处理：宁可拒绝，不放过可疑地址。
func isPrivateHost(host string) bool {
	ips, err := net.LookupIP(host)
	if err != nil {
		return true
	}
	for _, ip := range ips {
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return true
		}
	}
	return false
}
