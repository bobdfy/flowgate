package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/bobdfy/flowgate/internal/identity"
	"github.com/bobdfy/flowgate/internal/model"
)

// CreateAPIKey 处理 POST /api/v1/tenants/{id}/keys
// 关键：生成随机 key，库里只存 SHA256 哈希，明文只在本次响应里返回一次。
func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	tenantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var k model.APIKey
	if err := json.NewDecoder(r.Body).Decode(&k); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	k.TenantID = tenantID
	if k.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if k.Status == "" {
		k.Status = "active"
	}
	if k.QPSLimit <= 0 {
		k.QPSLimit = 100
	}

	// 模型白名单：只接受已存在的模型别名；空 = 不限制。
	allowed, err := h.normalizeAllowedModels(r.Context(), k.AllowedModels)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	k.AllowedModels = allowed

	plain, err := generateAPIKey()
	if err != nil {
		log.Println("generate api key failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	k.KeyHash = identity.HashKey(plain)

	if err := h.apikeys.Create(r.Context(), &k); err != nil {
		log.Println("create api key error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// 明文只在这次返回，之后库里的哈希无法反推
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":             k.ID,
		"tenant_id":      k.TenantID,
		"name":           k.Name,
		"status":         k.Status,
		"qps_limit":      k.QPSLimit,
		"allowed_models": k.AllowedModels,
		"expires_at":     k.ExpiresAt,
		"created_at":     k.CreatedAt,
		"key":            plain, // ← 明文，只此一次
	})
}

// ListAPIKeys 处理 GET /api/v1/tenants/{id}/keys
func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	tenantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	keys, err := h.apikeys.ListByTenant(r.Context(), tenantID)
	if err != nil {
		log.Println("list api keys error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

// UpdateAPIKey 处理 PUT /api/v1/keys/{id}
func (h *Handler) UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var k model.APIKey
	if err := json.NewDecoder(r.Body).Decode(&k); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	k.ID = id
	// S8：status 只允许 active / disabled，空则回填 active，避免写入脏数据。
	if k.Status == "" {
		k.Status = "active"
	}
	if k.Status != "active" && k.Status != "disabled" {
		writeError(w, http.StatusBadRequest, "status must be active or disabled")
		return
	}
	if k.QPSLimit <= 0 {
		k.QPSLimit = 100
	}

	// allowed_models 按需更新：
	//   nil（请求里没带）→ 保留原值，避免老客户端一次 PUT 把限制清空；
	//   []（显式空数组）→ 清空限制。
	if k.AllowedModels == nil {
		existing, gerr := h.apikeys.GetByID(r.Context(), id)
		if gerr != nil {
			log.Println("get api key failed", gerr)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "api key not found")
			return
		}
		k.AllowedModels = existing.AllowedModels
	} else {
		allowed, aerr := h.normalizeAllowedModels(r.Context(), k.AllowedModels)
		if aerr != nil {
			writeError(w, http.StatusBadRequest, aerr.Error())
			return
		}
		k.AllowedModels = allowed
	}

	if err := h.apikeys.Update(r.Context(), &k); err != nil {
		log.Println("update api key failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, k)
}

// DeleteAPIKey 处理 DELETE /api/v1/keys/{id}
func (h *Handler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.apikeys.Delete(r.Context(), id); err != nil {
		log.Println("delete api key failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// normalizeAllowedModels 去空白、去重，并校验每个模型别名都存在。
// 返回空切片表示不限制。
func (h *Handler) normalizeAllowedModels(ctx context.Context, in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		name := strings.TrimSpace(raw)
		if name == "" {
			return nil, fmt.Errorf("allowed_models must not contain empty values")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return out, nil
	}

	models, err := h.aiconfig.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("check allowed_models failed: %w", err)
	}
	known := make(map[string]bool, len(models))
	for _, m := range models {
		known[m.Name] = true
	}
	for _, name := range out {
		if !known[name] {
			return nil, fmt.Errorf("model %q does not exist", name)
		}
	}
	return out, nil
}

// generateAPIKey 用 crypto/rand 生成一个随机 API Key（sk_ 开头的十六进制）。
func generateAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "sk_" + hex.EncodeToString(b), nil
}
