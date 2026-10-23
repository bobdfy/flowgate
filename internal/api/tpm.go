package api

import (
	"log"
	"math"
	"net/http"
	"strconv"
	"time"
)

// tpmUsageResponse 是租户当前窗口的 TPM 用量。
type tpmUsageResponse struct {
	TenantID        int64     `json:"tenant_id"`
	WindowSeconds   int64     `json:"window_seconds"`
	WindowStart     time.Time `json:"window_start"`
	WindowEnd       time.Time `json:"window_end"`
	ResetsInSeconds int64     `json:"resets_in_seconds"`
	Used            int64     `json:"used"`
	Limit           int64     `json:"limit"`     // 0 = 不限制
	Remaining       *int64    `json:"remaining"` // limit=0 时为 null
	Unlimited       bool      `json:"unlimited"`
}

// GetTenantTPMUsage 处理 GET /api/v1/tenants/{id}/tpm-usage。
//
// 读的是 gateway 正在用的 Redis 固定窗口计数器，因此和限流判定完全一致；
// 窗口长度优先取 gateway 启动时发布的 tpm:window。
func (h *Handler) GetTenantTPMUsage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	tenant, err := h.tenants.GetByID(r.Context(), id)
	if err != nil {
		log.Println("get tenant for tpm usage failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if tenant == nil {
		writeError(w, http.StatusNotFound, "tenant not found")
		return
	}
	if h.tpm == nil {
		// admin 没配 Redis：管理面可用，但查不了实时配额。
		writeError(w, http.StatusServiceUnavailable, "TPM usage unavailable: Redis not configured")
		return
	}

	usage, err := h.tpm.Usage(r.Context(), id)
	if err != nil {
		log.Println("get tpm usage failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	resp := tpmUsageResponse{
		TenantID:      usage.TenantID,
		WindowSeconds: int64(usage.Window / time.Second),
		WindowStart:   usage.WindowStart,
		WindowEnd:     usage.WindowEnd,
		Used:          usage.Used,
		Limit:         tenant.TPMLimit,
		Unlimited:     tenant.TPMLimit <= 0,
	}
	if d := usage.WindowEnd.Sub(time.Now()); d > 0 {
		resp.ResetsInSeconds = int64(math.Ceil(d.Seconds()))
	}
	if !resp.Unlimited {
		remaining := resp.Limit - resp.Used
		if remaining < 0 {
			remaining = 0
		}
		resp.Remaining = &remaining
	}

	writeJSON(w, http.StatusOK, resp)
}
