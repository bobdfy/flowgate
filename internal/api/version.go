package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/store/postgres"
)

// Health 处理 GET /api/v1/health，返回服务存活状态（liveness）。
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Publish 处理 POST /api/v1/publish。
// 发布逻辑在 store 层（PublishSnapshot）完成：读快照 → 校验 → 生成版本 → 原子写入。
// 校验失败（ValidationError）映射成 400，其余错误映射成 500。
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	ver, err := h.versions.PublishSnapshot(r.Context())
	if err != nil {
		var vErr *postgres.ValidationError
		if errors.As(err, &vErr) {
			writeError(w, http.StatusBadRequest, vErr.Error())
			return
		}
		log.Println("publish failed:", err)
		writeError(w, http.StatusInternalServerError, "publish failed")
		return
	}
	writeJSON(w, http.StatusOK, ver)
}

// ListVersions 处理 GET /api/v1/versions，列出所有版本（倒序）。
func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := h.versions.ListVersions(r.Context())
	if err != nil {
		log.Println("list versions error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, versions)
}

// versionDetail 是 GET /api/v1/versions/{id} 的响应：版本元数据 + 快照条目。
type versionDetail struct {
	Version model.Version       `json:"version"`
	Items   []model.VersionItem `json:"items"`
}

// GetVersion 处理 GET /api/v1/versions/{id}，返回版本元数据和该版本的快照条目。
func (h *Handler) GetVersion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	ver, err := h.versions.GetByID(r.Context(), id)
	if err != nil {
		log.Println("get version failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if ver == nil {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}

	items, err := h.versions.GetItemsByVersion(r.Context(), id)
	if err != nil {
		log.Println("get version items failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, versionDetail{Version: *ver, Items: items})
}

// Rollback 处理 POST /api/v1/versions/{id}/rollback，
// 把历史版本的快照重新发布为一个新的 published 版本（copy-forward）。
func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	ver, err := h.versions.Rollback(r.Context(), id)
	if err != nil {
		log.Println("rollback failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if ver == nil {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}

	writeJSON(w, http.StatusOK, ver)
}
