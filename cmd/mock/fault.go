// 故障注入模块：让 mock 能按规则制造可控故障（延迟 / 状态码 / 概率），
// 用于验证网关在慢、错、偶发故障等场景下的行为。
package main

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// faultRule 一条故障规则：请求路径命中后，按规则注入延迟和状态码。
type faultRule struct {
	Path    string  `json:"path"`     // 生效路径，如 /api/orders
	Status  int     `json:"status"`   // 要返回的状态码；0 = 只延迟不拦截
	DelayMs int     `json:"delay_ms"` // 延迟毫秒数
	Prob    float64 `json:"prob"`     // 生效概率 0~1（默认 1 = 必然生效）
}

// faultStore 故障规则表。map 并发读写会 panic，所以用锁保护。
type faultStore struct {
	mu    sync.RWMutex
	rules map[string]faultRule // key = 路径
}

func newFaultStore() *faultStore {
	return &faultStore{rules: map[string]faultRule{}}
}

// Set 新增或覆盖一条规则（按 Path 定位）。
func (s *faultStore) Set(rule faultRule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules[rule.Path] = rule
}

// Get 查询某路径的规则；没有时 ok=false。
func (s *faultStore) Get(path string) (faultRule, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rule, ok := s.rules[path]
	return rule, ok
}

// Delete 删除某路径的规则。
func (s *faultStore) Delete(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rules, path)
}

// All 返回当前所有规则。
func (s *faultStore) All() []faultRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]faultRule, 0, len(s.rules))
	for _, rule := range s.rules {
		out = append(out, rule)
	}
	return out
}

// maybeInject 在业务 handler 的最前面调用。
// 返回 true = 已按规则注入故障并写了响应，调用方应直接 return，不再执行业务逻辑。
func (s *faultStore) maybeInject(w http.ResponseWriter, r *http.Request) bool {
	rule, ok := s.Get(r.URL.Path)
	if !ok {
		return false // 该路径没配规则，正常放行
	}
	if rand.Float64() >= rule.Prob {
		return false // 概率没命中，正常放行
	}
	if rule.DelayMs > 0 {
		time.Sleep(time.Duration(rule.DelayMs) * time.Millisecond)
	}
	if rule.Status != 0 {
		w.WriteHeader(rule.Status) // 拦截：直接返回故障状态码
		return true
	}
	return false // 只延迟不拦截，继续走正常业务
}

// withFault 包装器：给业务 handler 套上"先查故障、再执行业务"。
func withFault(f *faultStore, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if f.maybeInject(w, r) {
			return
		}
		next(w, r)
	}
}

// ---- /__fault 管理接口 ----

// handleSet 处理 POST /__fault：设置一条规则。
func (s *faultStore) handleSet(w http.ResponseWriter, r *http.Request) {
	var rule faultRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if rule.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	if rule.Prob <= 0 {
		rule.Prob = 1 // 默认必然生效
	}
	s.Set(rule)
	writeJSON(w, http.StatusOK, map[string]string{"set": rule.Path})
}

// handleList 处理 GET /__fault：列出所有规则。
func (s *faultStore) handleList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.All())
}

// handleDelete 处理 DELETE /__fault?path=/x：删除某路径的规则。
func (s *faultStore) handleDelete(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path query param is required"})
		return
	}
	s.Delete(path)
	writeJSON(w, http.StatusOK, map[string]string{"deleted": path})
}

// writeJSON 输出 JSON 响应（mock 内部工具函数）。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
