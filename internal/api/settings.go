package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/bobdfy/flowgate/internal/quota"
	"github.com/bobdfy/flowgate/internal/store/postgres"
)

const (
	minTPMWindowSeconds = 1
	maxTPMWindowSeconds = 7 * 24 * 60 * 60 // 7 天
)

// SettingsHandler 提供运行时可配置参数（目前只有 TPM 窗口）。
type SettingsHandler struct {
	settings *postgres.SettingsStore
	// tpm 只用于读 gateway 已发布的窗口，nil = 未配置 Redis。
	tpm *quota.RedisTPM
}

// NewSettingsHandler 创建 SettingsHandler。
func NewSettingsHandler(settings *postgres.SettingsStore, tpm *quota.RedisTPM) *SettingsHandler {
	return &SettingsHandler{settings: settings, tpm: tpm}
}

// RegisterRoutes 注册设置相关路由。
func (h *SettingsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/settings/tpm-window", h.GetTPMWindow)
	mux.HandleFunc("PUT /api/v1/settings/tpm-window", h.UpdateTPMWindow)
}

type tpmWindowResponse struct {
	Configured bool `json:"configured"`
	// WindowSeconds 是数据库里保存的配置；0 = 还没通过控制台配置过。
	WindowSeconds int `json:"window_seconds"`
	// EffectiveWindowSeconds 是 gateway 当前实际使用的窗口；0 = 尚未上报。
	EffectiveWindowSeconds int        `json:"effective_window_seconds"`
	UpdatedAt              *time.Time `json:"updated_at"`
}

type tpmWindowRequest struct {
	WindowSeconds int `json:"window_seconds"`
}

// GetTPMWindow 处理 GET /api/v1/settings/tpm-window。
func (h *SettingsHandler) GetTPMWindow(w http.ResponseWriter, r *http.Request) {
	seconds, updatedAt, err := h.settings.TPMWindow(r.Context())
	if err != nil {
		log.Println("get tpm window setting failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	resp := tpmWindowResponse{
		Configured:    seconds > 0,
		WindowSeconds: seconds,
	}
	if !updatedAt.IsZero() {
		resp.UpdatedAt = &updatedAt
	}
	if h.tpm != nil {
		if d, ok, derr := h.tpm.PublishedWindow(r.Context()); derr != nil {
			log.Println("read published tpm window failed", derr)
		} else if ok {
			resp.EffectiveWindowSeconds = int(d / time.Second)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// UpdateTPMWindow 处理 PUT /api/v1/settings/tpm-window。
//
// 只写数据库；gateway 每 3 秒轮询 gateway_settings 并热切换窗口，
// 切换后会更新 Redis 里的 tpm:window，管理面的用量查询随之对齐。
func (h *SettingsHandler) UpdateTPMWindow(w http.ResponseWriter, r *http.Request) {
	var req tpmWindowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.WindowSeconds < minTPMWindowSeconds || req.WindowSeconds > maxTPMWindowSeconds {
		writeError(w, http.StatusBadRequest, "window_seconds 必须在 1 到 604800 之间")
		return
	}

	updatedAt, err := h.settings.SetTPMWindow(r.Context(), req.WindowSeconds)
	if err != nil {
		log.Println("set tpm window setting failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	resp := tpmWindowResponse{
		Configured:    true,
		WindowSeconds: req.WindowSeconds,
		UpdatedAt:     &updatedAt,
	}
	// 此刻 gateway 可能还没来得及切换；返回它当前的值，前端据此展示“应用中”。
	if h.tpm != nil {
		if d, ok, derr := h.tpm.PublishedWindow(r.Context()); derr != nil {
			log.Println("read published tpm window failed", derr)
		} else if ok {
			resp.EffectiveWindowSeconds = int(d / time.Second)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
