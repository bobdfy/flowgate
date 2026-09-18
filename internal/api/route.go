package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/bobdfy/flowgate/internal/model"
)

// checkRouteConflict 检查匹配规则（host + path + match_type + methods）是否已被别的路由占用。
//
// ★ 为什么必须挡：router.go 的 moreSpecific 比较的是 len(pattern) 的严格大于。
// 规则相同的两条路由，谁生效取决于遍历顺序（主键升序）——
// 也就是说【先建的赢】，而且完全静默：不报错、无日志、无指标。
// 使用者改了后建的那条却发现行为没变，只能靠读代码才能找到原因。
//
// ★ methods 也算进匹配规则：`path=/echo methods=GET` 和
// `path=/echo methods=POST` 匹配的是【不同的请求】，不算冲突。
// 只有什么都不填（= 不限方法）才和所有方法冲突。
//
// excludeID 传正在修改的路由 ID（Create 时传 0）。
// 冲突时写 409 并返回 false。
func (h *Handler) checkRouteConflict(w http.ResponseWriter, r *http.Request, route *model.Route, excludeID int64) bool {
	dupID, err := h.routes.FindDuplicateByPattern(
		r.Context(), route.Host, route.PathPattern, route.PathMatchType, route.Methods, excludeID)
	if err != nil {
		log.Println("check route conflict failed", err)
		writeError(w, http.StatusInternalServerError, "internal route error")
		return false
	}
	if dupID == 0 {
		return true // 没冲突
	}

	// ★ 报错里要带上已存在那条的 id —— 否则使用者只知道"冲突了"，
	// 还得自己去列表里翻是哪一条。直接告诉他"改这一条"。
	writeError(w, http.StatusConflict, fmt.Sprintf(
		"路由冲突: host=%q path=%q match=%q methods=%q 已被路由 id=%d 占用，请改为修改那条路由",
		route.Host, route.PathPattern, route.PathMatchType, route.Methods, dupID))
	return false
}

// CreateRoute 处理 POST /api/v1/routes
// 请求体：{"service_id":...,"name":"...","path_pattern":"...","path_match_type":"...","methods":"...","enabled":...}
func (h *Handler) CreateRoute(w http.ResponseWriter, r *http.Request) {
	var route model.Route

	if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if route.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// 闸门 1：禁止创建匹配规则重复的路由（要改就改已存在的那条）。
	if !h.checkRouteConflict(w, r, &route, 0) {
		return
	}

	if err := h.routes.Create(r.Context(), &route); err != nil {
		log.Println("create route error", err)
		writeError(w, http.StatusInternalServerError, "internal route error")
		return
	}

	writeJSON(w, http.StatusCreated, route)
}

// ListRoutes 处理 GET /api/v1/routes
func (h *Handler) ListRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := h.routes.List(r.Context())
	if err != nil {
		log.Println("list routes error", err)
		writeError(w, http.StatusInternalServerError, "internal route error")
		return
	}

	writeJSON(w, http.StatusOK, routes)
}

// UpdateRoute 处理 PUT /api/v1/routes/{id}
func (h *Handler) UpdateRoute(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var route model.Route
	if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// 关键：id 以 URL 为准
	route.ID = id

	// 闸门 1 同样要挡这里：否则绕开办法就是"先建一条不冲突的，
	// 再 PUT 成冲突的"。excludeID 传自己的 id，避免把自己算成冲突。
	if !h.checkRouteConflict(w, r, &route, id) {
		return
	}

	if err := h.routes.Update(r.Context(), &route); err != nil {
		log.Println("update route failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, route)
}

// DeleteRoute 处理 DELETE /api/v1/routes/{id}
func (h *Handler) DeleteRoute(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err = h.routes.Delete(r.Context(), id); err != nil {
		log.Println("delete route failed", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
