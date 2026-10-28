package api

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bobdfy/flowgate/internal/errorlog"
)

// ErrorLister 读取最近失败请求（生产实现是 errorlog.RedisReader）。
type ErrorLister interface {
	List(ctx context.Context) ([]errorlog.Entry, error)
}

// LogsHandler 提供错误看板的只读查询。
//
// 数据来自 gateway 写入的 Redis 聚合表 flowgate:errors：多实例汇总、
// 最多保留 errorlog.Cap 种错误（不是条数），不是完整日志存储。
// 筛选在 admin 内存里做，因为窗口本身只有 500 行。
type LogsHandler struct {
	lister ErrorLister
}

// NewLogsHandler 创建 handler；lister 为 nil（未配置 Redis）时接口返回 503。
func NewLogsHandler(lister ErrorLister) *LogsHandler {
	return &LogsHandler{lister: lister}
}

// RegisterRoutes 注册错误看板路由。
func (h *LogsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/logs/errors", h.ListErrors)
}

type errorLogResponse struct {
	Entries []errorlog.Entry `json:"entries"`
	// Total 是过滤后的行数（= 错误种类数）；Entries 可能因为 limit 被截短。
	// 请求总次数不在这里，而是在各行 Entry.Count 里。
	Total int `json:"total"`
	// Capacity 是窗口上限（最多保留多少种错误），前端用它说明视图覆盖面。
	Capacity int `json:"capacity"`
}

// ListErrors 处理 GET /api/v1/logs/errors。
//
// 查询参数（都可选）：kind、status、route（子串）、request_id、tenant_id、
// since（如 15m / 1h）、limit（默认上限 500）。
func (h *LogsHandler) ListErrors(w http.ResponseWriter, r *http.Request) {
	if h.lister == nil {
		writeError(w, http.StatusServiceUnavailable, "未配置 Redis，无法读取错误请求")
		return
	}

	opts, err := parseErrorQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	entries, err := h.lister.List(r.Context())
	if err != nil {
		log.Println("list error log failed:", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	matched := filterEntries(entries, opts)
	total := len(matched)
	if len(matched) > opts.limit {
		matched = matched[:opts.limit]
	}
	if matched == nil {
		matched = []errorlog.Entry{}
	}

	writeJSON(w, http.StatusOK, errorLogResponse{
		Entries:  matched,
		Total:    total,
		Capacity: int(errorlog.Cap),
	})
}

type errorQuery struct {
	kind      errorlog.Kind
	status    int
	route     string
	requestID string
	tenantID  int64
	since     time.Time
	limit     int
}

func parseErrorQuery(r *http.Request) (errorQuery, error) {
	q := errorQuery{limit: int(errorlog.Cap)}
	values := r.URL.Query()

	if v := strings.TrimSpace(values.Get("kind")); v != "" {
		q.kind = errorlog.Kind(v)
	}
	if v := strings.TrimSpace(values.Get("status")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 100 || n > 599 {
			return q, errInvalidQuery("status 必须是 100-599 的整数")
		}
		q.status = n
	}
	if v := strings.TrimSpace(values.Get("route")); v != "" {
		q.route = strings.ToLower(v)
	}
	if v := strings.TrimSpace(values.Get("request_id")); v != "" {
		q.requestID = v
	}
	if v := strings.TrimSpace(values.Get("tenant_id")); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return q, errInvalidQuery("tenant_id 必须是正整数")
		}
		q.tenantID = n
	}
	if v := strings.TrimSpace(values.Get("since")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return q, errInvalidQuery("since 必须是正的时间长度，如 15m、1h")
		}
		q.since = time.Now().Add(-d)
	}
	if v := strings.TrimSpace(values.Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return q, errInvalidQuery("limit 必须是正整数")
		}
		if n < q.limit {
			q.limit = n
		}
	}
	return q, nil
}

type errInvalidQuery string

func (e errInvalidQuery) Error() string { return string(e) }

// filterEntries 按查询条件过滤；条目本身已按最近发生时间倒序（ZREVRANGE 顺序）。
func filterEntries(entries []errorlog.Entry, q errorQuery) []errorlog.Entry {
	out := make([]errorlog.Entry, 0, len(entries))
	for _, e := range entries {
		if q.kind != "" && e.Kind != q.kind {
			continue
		}
		if q.status != 0 && e.Status != q.status {
			continue
		}
		if q.route != "" && !strings.Contains(strings.ToLower(e.Route), q.route) {
			continue
		}
		if q.requestID != "" && e.RequestID != q.requestID {
			continue
		}
		if q.tenantID != 0 && e.TenantID != q.tenantID {
			continue
		}
		if !q.since.IsZero() && e.TS.Before(q.since) {
			continue
		}
		out = append(out, e)
	}
	return out
}
