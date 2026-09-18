package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// 故障注入模式。
//
// 命名规则：sse_ 前缀的是流式模式，其余是非流式。
const (
	// ---- 非流式 ----
	modeNormal    = "normal"    // 正常返回
	modeError     = "error"     // 固定 500
	modeRateLimit = "ratelimit" // 固定 429 + Retry-After

	// ---- 流式：正常 ----
	modeSSENormal = "sse_normal" // 一个事件一个 chunk

	// ---- 流式：故意切坏字节（★ 这些是用来测分帧器的）----
	modeSSESplit    = "sse_split"    // 一个事件切成两个 chunk
	modeSSEDelim    = "sse_delim"    // \r\n\r\n 切在 CR 和 LF 之间
	modeSSENoTail   = "sse_no_tail"  // 最后不补结尾空行就关流
	modeSSESlow     = "sse_slow"     // 事件之间 sleep，验证 flush
	modeSSEAbort    = "sse_abort"    // 发一半直接断连
	modeSSEOversize = "sse_oversize" // 先发 1MiB+ 垃圾，再发正常事件
)

var allModes = []string{
	modeNormal, modeError, modeRateLimit,
	modeSSENormal, modeSSESplit, modeSSEDelim,
	modeSSENoTail, modeSSESlow, modeSSEAbort, modeSSEOversize,
}

type modeStore struct {
	mu sync.Mutex
	m  string
}

func newModeStore() *modeStore {
	return &modeStore{m: modeNormal}
}

func (s *modeStore) get() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.m
}

// set 切换模式。
//
// ★ 模式名非法时返回 error，不静默忽略 ——
// 写错模式名却以为生效了，会让整轮测试的结论作废。
func (s *modeStore) set(m string) error {
	for _, ok := range allModes {
		if m == ok {
			s.mu.Lock()
			s.m = m
			s.mu.Unlock()
			return nil
		}
	}
	return fmt.Errorf("未知模式 %q，合法值: %v", m, allModes)
}

// modeHandler 处理 POST /__mode，body 形如 {"mode":"error"}。
func modeHandler(modes *modeStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(r)
		if err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}

		var req struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error",
				"请求体不是合法 JSON: "+err.Error())
			return
		}
		if req.Mode == "" {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "缺少 mode 字段")
			return
		}

		if err := modes.set(req.Mode); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":   true,
			"mode": modes.get(),
		})
	}
}

// modeGetHandler 处理 GET /__mode：返回当前模式，方便脚本里查状态。
func modeGetHandler(modes *modeStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		writeJSON(w, http.StatusOK, map[string]any{
			"mode": modes.get(),
			"all":  allModes,
		})
	}
}
