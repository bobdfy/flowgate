package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

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
		"id":         k.ID,
		"tenant_id":  k.TenantID,
		"name":       k.Name,
		"status":     k.Status,
		"expires_at": k.ExpiresAt,
		"created_at": k.CreatedAt,
		"key":        plain, // ← 明文，只此一次
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
	if k.QPSLimit <= 0 {
		k.QPSLimit = 100
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

// generateAPIKey 用 crypto/rand 生成一个随机 API Key（sk_ 开头的十六进制）。
func generateAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "sk_" + hex.EncodeToString(b), nil
}
