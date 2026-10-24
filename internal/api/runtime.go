package api

import (
	"log"
	"net/http"
	"time"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/runtimeinfo"
	"github.com/redis/go-redis/v9"
)

// RuntimeHandler 提供 gateway 运行时只读状态查询。
//
// 数据来自 gateway 每 5 秒写入 Redis 的快照；admin 不跑健康检查，
// 所以这里返回的是“gateway 最近一次上报的内容”，不是 admin 自己探测的结果。
type RuntimeHandler struct {
	rdb *redis.Client
}

// NewRuntimeHandler 创建运行时状态 handler；rdb 为 nil 时接口返回 503。
func NewRuntimeHandler(rdb *redis.Client) *RuntimeHandler {
	return &RuntimeHandler{rdb: rdb}
}

// RegisterRoutes 注册运行时状态路由。
func (h *RuntimeHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/runtime/nodes", h.Nodes)
	mux.HandleFunc("GET /api/v1/runtime/ai", h.AI)
}

type runtimeMeta struct {
	Instance    string    `json:"instance"`
	PublishedAt time.Time `json:"published_at"`
	AgeSeconds  int       `json:"age_seconds"`
	Stale       bool      `json:"stale"`
}

type runtimeNodesResponse struct {
	runtimeMeta
	Nodes []model.NodeRuntime `json:"nodes"`
}

type runtimeAIResponse struct {
	runtimeMeta
	AI model.AIRuntimeStatus `json:"ai"`
}

// Nodes 处理 GET /api/v1/runtime/nodes。
func (h *RuntimeHandler) Nodes(w http.ResponseWriter, r *http.Request) {
	st, meta, ok := h.read(w, r)
	if !ok {
		return
	}
	if st.Nodes == nil {
		st.Nodes = []model.NodeRuntime{}
	}
	writeJSON(w, http.StatusOK, runtimeNodesResponse{runtimeMeta: meta, Nodes: st.Nodes})
}

// AI 处理 GET /api/v1/runtime/ai。
func (h *RuntimeHandler) AI(w http.ResponseWriter, r *http.Request) {
	st, meta, ok := h.read(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, runtimeAIResponse{runtimeMeta: meta, AI: st.AI})
}

// read 读取快照并计算上报年龄；ok=false 时已经写好响应。
func (h *RuntimeHandler) read(w http.ResponseWriter, r *http.Request) (model.GatewayRuntime, runtimeMeta, bool) {
	if h.rdb == nil {
		writeError(w, http.StatusServiceUnavailable, "未配置 Redis，无法读取 gateway 运行状态")
		return model.GatewayRuntime{}, runtimeMeta{}, false
	}

	st, found, err := runtimeinfo.Read(r.Context(), h.rdb)
	if err != nil {
		log.Println("read runtime status failed:", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return model.GatewayRuntime{}, runtimeMeta{}, false
	}
	if !found {
		writeError(w, http.StatusServiceUnavailable, "gateway 尚未上报运行状态")
		return model.GatewayRuntime{}, runtimeMeta{}, false
	}

	age := time.Since(st.PublishedAt)
	if age < 0 {
		age = 0
	}
	meta := runtimeMeta{
		Instance:    st.Instance,
		PublishedAt: st.PublishedAt,
		AgeSeconds:  int(age.Seconds()),
		Stale:       age > runtimeinfo.StaleAfter,
	}
	return st, meta, true
}
