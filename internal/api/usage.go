package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/store/postgres"
)

// UsageHandler 提供 AI token 用量流水查询。
//
// 单独一个 handler，避免 api.Handler 继续膨胀：
// 用量查询只依赖 UsageStore，和租户 / 路由 / AI 配置互不相干。
type UsageHandler struct {
	usage *postgres.UsageStore
}

// NewUsageHandler 创建用量查询 handler。
func NewUsageHandler(usage *postgres.UsageStore) *UsageHandler {
	return &UsageHandler{usage: usage}
}

// RegisterRoutes 注册用量查询路由。
func (h *UsageHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/usage", h.ListUsage)
	mux.HandleFunc("GET /api/v1/usage/summary", h.SummaryUsage)
}

type usageListResponse struct {
	Items  []model.AIUsage `json:"items"`
	From   time.Time       `json:"from"`
	To     time.Time       `json:"to"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

type usageSummaryResponse struct {
	GroupBy string                 `json:"group_by"`
	From    time.Time              `json:"from"`
	To      time.Time              `json:"to"`
	Items   []model.AIUsageSummary `json:"items"`
}

// ListUsage 处理 GET /api/v1/usage。
//
// 过滤参数：tenant_id / key_id / model / from / to / limit / offset。
// from、to 接受 RFC3339 或 YYYY-MM-DD；缺省为最近 24 小时。
func (h *UsageHandler) ListUsage(w http.ResponseWriter, r *http.Request) {
	q, _, err := parseUsageQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	items, err := h.usage.List(r.Context(), q)
	if err != nil {
		log.Println("list ai usage failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}

	writeJSON(w, http.StatusOK, usageListResponse{
		Items:  items,
		From:   q.From,
		To:     q.To,
		Limit:  limit,
		Offset: offset,
	})
}

// SummaryUsage 处理 GET /api/v1/usage/summary。
//
// group_by：model（默认）/ key / tenant / day。
func (h *UsageHandler) SummaryUsage(w http.ResponseWriter, r *http.Request) {
	q, groupBy, err := parseUsageQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	items, err := h.usage.Summary(r.Context(), q, groupBy)
	if err != nil {
		log.Println("summarize ai usage failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, usageSummaryResponse{
		GroupBy: groupBy,
		From:    q.From,
		To:      q.To,
		Items:   items,
	})
}

// parseUsageQuery 解析公共过滤参数。返回的 groupBy 只对 summary 有意义。
func parseUsageQuery(r *http.Request) (postgres.UsageQuery, string, error) {
	v := r.URL.Query()
	q := postgres.UsageQuery{
		Model: strings.TrimSpace(v.Get("model")),
	}

	now := time.Now()
	q.To = now
	q.From = now.Add(-24 * time.Hour)

	if s := v.Get("tenant_id"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return q, "", errInvalid("tenant_id")
		}
		q.TenantID = &n
	}
	if s := v.Get("key_id"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return q, "", errInvalid("key_id")
		}
		q.KeyID = &n
	}
	if s := v.Get("from"); s != "" {
		t, err := parseUsageTime(s)
		if err != nil {
			return q, "", errInvalid("from")
		}
		q.From = t
	}
	if s := v.Get("to"); s != "" {
		t, err := parseUsageTime(s)
		if err != nil {
			return q, "", errInvalid("to")
		}
		q.To = t
	}
	if !q.From.Before(q.To) {
		return q, "", errors.New("from 必须早于 to")
	}

	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return q, "", errInvalid("limit")
		}
		q.Limit = n
	}
	if s := v.Get("offset"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return q, "", errInvalid("offset")
		}
		q.Offset = n
	}

	groupBy := v.Get("group_by")
	switch groupBy {
	case "", "model":
		groupBy = "model"
	case "key", "tenant", "day":
	default:
		return q, "", errInvalid("group_by（可选 model / key / tenant / day）")
	}

	return q, groupBy, nil
}

// parseUsageTime 支持 RFC3339（前端 Date.toISOString）和日期。
// 日期按服务器本地时区解释，方便手写查询。
func parseUsageTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.ParseInLocation("2006-01-02", s, time.Local)
}

type invalidParamError string

func (e invalidParamError) Error() string { return "参数不合法: " + string(e) }

func errInvalid(name string) error { return invalidParamError(name) }
