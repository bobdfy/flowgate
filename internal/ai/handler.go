package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ProviderEntry 一个上游供应商的完整信息: 连接的地址、用哪个 key、
// 以及负责协议转换的适配器。
type ProviderEntry struct {
	// Name 是上游供应商标识（openai / deepseek / claude / ollama），
	// 只用于日志和 /v1/models 的 owned_by，不参与路由。
	Name string

	// Provider 是协议转换器。
	Provider Provider

	// BaseURL 是上游根地址，如 https://api.deepseek.com（不带路径）。
	// 具体接口路径由 ApiName.Path() 拼上去。
	BaseURL string

	// // APIKeys 是该 provider 的 key 池（至少一个；空串表示上游不需要鉴权）。
	// // 现在就用切片而不是单个 string，第四阶段的 failover 池要用，
	// // 避免以后改签名。
	// APIKeys []string

	// UpstreamModel 是发给上游时实际使用的模型名；空 = 与客户端请求的相同。
	// 用于模型别名与灰度：对外 flowgate-chat，对内 gpt-4o-mini。
	UpstreamModel string

	// Caps 是构造期探测的能力位图。
	//
	// 热路径只读布尔值，不再每请求做类型断言 ——
	// 能力和 provider 实例一样是"构造后不变"的。
	// 由 cmd/gateway 在装配时调 ProbeCapabilities(entry.Provider) 填充。
	Caps Capabilities

	// UpstreamPath 是上游真实路径；空 = 用 ApiName.Path()（默认行为）。
	// 为什么要它：对外路径和上游路径不总是同一个。
	//   对外（OpenAI SDK 写死的）: POST /v1/chat/completions
	//   DeepSeek 上游:            /v1/chat/completions     ← 一样
	//   智谱上游:                 /chat/completions       ← 不一样
	//     （智谱的 BaseURL 已经含 /api/paas/v4）
	UpstreamPath string
}

// Handler 处理 AI 请求（OpenAI 兼容接口）。
type Handler struct {
	// providers 的 key 是"对外模型名"，不是上游模型名。
	// 这是模型白名单的唯一实现点。
	providers map[string]*ProviderEntry

	client *http.Client

	// streams 是流式转发器，可为 nil（退化成缓冲转发）。
	streams StreamingForwarder
}

// NewHandler 创建 AI 处理器。
func NewHandler(provider map[string]*ProviderEntry) *Handler {
	return &Handler{
		providers: provider,
		client:    &http.Client{Timeout: 60 * time.Second},
		streams:   streamProxy,
	}
}

// SetStreamingForwarder 注入流式转发器。
//
// 没注入时 ServeHTTP 的流式分支会退化成缓冲转发，不会 panic ——
// 这样"还没做流式"的阶段也能跑通。
func (h *Handler) SetStreamingForwarder(f StreamingForwarder) {
	h.streams = f
}

// SetClient 替换底层 HTTP 客户端（测试用）。
func (h *Handler) SetClient(c *http.Client) {
	if c != nil {
		h.client = c
	}
}

// RegisterRoutes 把所有已注册的 AI 接口挂到 mux 上。
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	for _, rt := range RegisteredRoutes() {
		mux.Handle(rt.Method+" "+rt.Path, h)
	}
}

// ServeHTTP 处理 /v1/* 的 OpenAI 兼容请求。
// 每一步失败都必须立即返回，错误体统一用 OpenAI 格式（writeError）。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	apiName, ok := ApiNameFromPath(r.Method, r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, ErrTypeNotFound,
			"未知的 AI 接口: "+r.Method+" "+r.URL.Path)
		return
	}

	if apiName == Models {
		h.serveModels(w)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrTypeInvalidRequest,
			"读取请求体失败: "+err.Error())
		return
	}
	_ = r.Body.Close()

	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, ErrTypeInvalidRequest, "body 为空")
		return
	}

	// 4. 解析 model —— 路由的唯一依据。
	req, err := parseChatRequest(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrTypeInvalidRequest,
			"请求体不合法: "+err.Error())
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, ErrTypeInvalidRequest, "缺少 model 字段")
		return
	}

	// 5. 模型白名单：没配置的模型一律 404，绝不透传给任何上游。
	entry, ok := h.providers[req.Model]
	if !ok || entry == nil {
		writeError(w, http.StatusNotFound, ErrTypeNotFound, "未知模型: "+req.Model)
		return
	}

	// 6. 协议转换。先深拷一份客户端请求头交给 provider 改。
	header := headerToMap(r.Header)

	if entry.Caps.RequestBody {
		if bh, ok := entry.Provider.(RequestBodyHandler); ok {
			converted, berr := bh.OnRequestBody(r.Context(), apiName, body)
			if berr != nil {
				slog.Error("ai_request_body_failed",
					"model", req.Model, "provider", entry.Name, "err", berr)
				writeError(w, http.StatusBadGateway, ErrTypeUpstream,
					"请求体转换失败: "+berr.Error())
				return
			}
			body = converted
		}
	}

	// 7. 模型别名 / 灰度。失败只记 warn，继续用原 body ——
	// 别因为一个优化把请求打挂。
	if entry.UpstreamModel != "" && entry.UpstreamModel != req.Model {
		if rewritten, rerr := rewriteModel(body, entry.UpstreamModel); rerr == nil {
			body = rewritten
		} else {
			slog.Warn("ai_model_rewrite_failed", "model", req.Model, "err", rerr)
		}
	}

	// 8. ★ 单点判定：走 buffered 还是 streaming，只判一次。
	streaming := isStreamRequest(apiName, body)

	// 9. 发上游请求。
	resp, err := h.forward(r, entry, apiName, header, body, streaming)
	if err != nil {
		status, errType := classifyUpstreamError(err)
		slog.Error("ai_forward_failed",
			"model", req.Model, "provider", entry.Name,
			"api", string(apiName), "streaming", streaming, "err", err)
		writeError(w, status, errType, "上游调用失败: "+err.Error())
		return
	}
	defer resp.Body.Close()

	// 10~11. 输出。
	if streaming {
		h.serveStream(w, r, resp, req.Model)
		return
	}
	h.serveBuffered(w, resp, req.Model)
}

// serveModels 处理 GET /v1/models。
// TODO(阶段三)：改用 ModelRegistry —— 只暴露"已配置且启用"的模型，
// 不把上游的全部模型名泄露给客户端。
func (h *Handler) serveModels(w http.ResponseWriter) {
	list := ModelList{Object: "list", Data: make([]ModelInfo, 0, len(h.providers))}
	for name, entry := range h.providers {
		owned := "flowgate"
		if entry != nil && entry.Name != "" {
			owned = entry.Name
		}
		list.Data = append(list.Data, ModelInfo{ID: name, Object: "model", OwnedBy: owned})
	}
	writeJSON(w, http.StatusOK, list)
}

// serveBuffered 处理非流式响应：整读上游 body → 透传给客户端。
//
// 上游报错时原样透传它的状态码和错误体 —— 不要伪装成成功，
// 也不要自己改成 502。401 = 上游 key 配错了，429 = 被上游限流，
// 这两种和"网关自己 502"是完全不同的问题，混掉之后排障要多花几小时。
func (h *Handler) serveBuffered(w http.ResponseWriter, resp *http.Response, model string) {
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("ai_read_upstream_failed", "model", model, "err", err)
		writeError(w, http.StatusBadGateway, ErrTypeUpstream,
			"读取上游响应失败: "+err.Error())
		return
	}
	if resp.StatusCode >= 400 {
		copySafeHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}

	copySafeHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

// serveStream 处理流式响应：交给注入的 SSE 转发器。
//
// 三个必须处理的边界：
//
//  1. h.streams == nil —— 没注入转发器时退化成 serveBuffered，不要 panic
//  2. resp.StatusCode >= 400 —— 上游报错时返回的是 JSON 不是 SSE。
//     硬按 SSE 分帧会把整个 JSON 当成"没有分隔符的残留"，
//     最后 flush 出一个畸形事件，客户端解析器直接挂掉，真实错误信息全丢
//  3. 顺序：先 prepareStreamHeaders（含删 Content-Length）→ WriteHeader →
//     再开始转发。顺序反了头就不生效
func (h *Handler) serveStream(w http.ResponseWriter, r *http.Request, resp *http.Response, model string) {
	if h.streams == nil {
		slog.Warn("ai_stream_forwarder_missing", "model", model)
		h.serveBuffered(w, resp, model)
		return
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		copySafeHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return
	}

	prepareStreamHeaders(w, resp)
	w.WriteHeader(http.StatusOK)

	// sink 目前恒为 nil。
	//
	// 它原本是给用量统计用的钩子（逐事件抽 usage 交给 Meter）。
	// 现在不做计费，所以留 nil；以后要做的话在这里赋值即可，
	// streamProxy 的签名不用改。
	var sink StreamEventSink

	if err := h.streams(w, r, resp.Body, sink); err != nil {
		// 响应头可能已经发出去了，只能记日志。
		slog.Warn("ai_stream_failed", "model", model, "err", err)
	}
}

// forward 把请求发到上游，返回上游响应。
//
// 四条硬约束（写错不报错、只在运行时出问题）：
//
//  1. ★ 路径用 joinURL(entry.BaseURL, apiName.Path())，不手拼字符串 ——
//     手拼少一层 /v1 也能编译通过，只会在运行时报上游 404
//  2. ★ 请求体必须挂上去（bytes.NewReader(body)）——
//     漏了的话所有请求都会以空 body 发给上游
//  3. ★ ctx 必须派生自 r.Context()（用 NewRequestWithContext）——
//     客户端断开时取消才能传导到上游，否则上游还在生成，token 照烧
//  4. ★ 组头走白名单（copySafeRequestHeaders）——
//     Authorization / Host / Cookie / Content-Length 一律网关自己写，
//     否则客户端能用别的凭据覆盖上游鉴权
func (h *Handler) forward(r *http.Request, entry *ProviderEntry, apiName ApiName, header map[string][]string, body []byte, streaming bool) (*http.Response, error) {
	//url := joinURL(entry.BaseURL, apiName.Path())

	path := apiName.Path()
	if entry.UpstreamPath != "" {
		path = entry.UpstreamPath
	}

	url := joinURL(entry.BaseURL, path)
	// ★ 第四个参数是请求体，不能传 nil —— 传 nil 的话上游收到的是空 body。
	// bytes.NewReader 把字节切片包成可读流，http 包会据此自动推出 Content-Length。
	req, err := http.NewRequestWithContext(r.Context(), r.Method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	// ① 先放客户端来的头（白名单过滤）
	copySafeRequestHeaders(req.Header, header)

	// // 上游鉴权：provider 钩子写的 Authorization 不在客户端透传白名单里，需单独放行。
	// if vals := header["Authorization"]; len(vals) > 0 && vals[0] != "" {
	// 	req.Header.Set("Authorization", vals[0])
	// }

	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// ② 再让 provider 改「真实的上游请求头」
	//    ★ 顺序关键：provider 在最后，它的决定有最终效力
	//    ★ 客户端的 Authorization / Cookie 已经在 ① 被白名单挡掉了，
	//      所以客户端无法伪造凭证
	if entry.Caps.RequestHeaders {
		if hh, ok := entry.Provider.(RequestHeadersHandler); ok {
			if herr := hh.OnRequestHeaders(r.Context(), apiName, req.Header); herr != nil {
				return nil, fmt.Errorf("provider.OnRequestHeaders: %w", herr)
			}
		}
	}

	// 显式设长度：bytes.NewReader 已经能让包自动推出来，
	// 但写出来更明确，也避免以后换了 body 类型忘了这回事。
	req.ContentLength = int64(len(body))

	if streaming {
		req.Header.Set("Accept", "text/event-stream")
		// ★ 主动不带 Accept-Encoding：上游不压缩，回来的就是明文 SSE，
		// 分帧器才能逐事件解析。
		//（等收到响应再删 Content-Encoding 是无效操作 —— body 已经压缩过了。）
		req.Header.Del("Accept-Encoding")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	return h.client.Do(req)
}

// parseChatRequest 解析 Chat Completions 请求体。
func parseChatRequest(body []byte) (*ChatRequest, error) {
	var req ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

// isStreamRequest 判断这次请求要不要走流式。
//
// 只有 ChatCompletions 支持流式；其它接口一律 false。
// JSON 解析失败也返回 false（当作非流式处理，让上游去报错）。
func isStreamRequest(apiName ApiName, body []byte) bool {
	if apiName != ChatCompletions {
		return false
	}
	req, err := parseChatRequest(body)
	if err != nil {
		return false
	}
	return req.Stream
}

// rewriteModel 把请求体里的 model 字段替换成上游真实模型名（别名 / 灰度用）。

func rewriteModel(body []byte, upstreamModel string) ([]byte, error) {
	var rwm map[string]json.RawMessage
	if err := json.Unmarshal(body, &rwm); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(upstreamModel)
	if err != nil {
		return nil, err
	}
	rwm["model"] = raw
	return json.Marshal(rwm)
}

// joinURL 把 base 和 path 拼成一个完整 URL，处理两侧斜杠的重复或缺失。
func joinURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if path == "" {
		return base
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// headerToMap 把 http.Header 深拷成普通 map，交给 provider 钩子改写。
func headerToMap(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, v := range h {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// safeForwardHeaders 是允许从客户端透传到上游的白名单头（已规范化）。
var safeForwardHeaders = map[string]bool{
	"Accept-Language":             true,
	"User-Agent":                  true,
	"X-Request-Id":                true,
	"Openai-Organization":         true,
	"Openai-Project":              true,
	"X-Stainless-Lang":            true,
	"X-Stainless-Package-Version": true,
	"X-Stainless-Os":              true,
	"X-Stainless-Arch":            true,
	"X-Stainless-Runtime":         true,
	"X-Stainless-Runtime-Version": true,
}

// copySafeRequestHeaders 按白名单把客户端头拷到上游请求。
func copySafeRequestHeaders(dst http.Header, src map[string][]string) {
	for k, vv := range src {
		canonical := http.CanonicalHeaderKey(k)
		if !safeForwardHeaders[canonical] {
			continue
		}
		for _, v := range vv {
			dst.Add(canonical, v)
		}
	}
}

// copySafeHeaders 把上游响应头拷给客户端，剔除逐跳头和长度类头。
//
//	Connection / Keep-Alive 等 —— 逐跳头，只对"上游↔网关"这一跳有意义
func copySafeHeaders(dst, src http.Header) {
	for k, vv := range src {
		switch http.CanonicalHeaderKey(k) {
		case "Connection",
			"Proxy-Connection",
			"Keep-Alive",
			"Proxy-Authenticate",
			"Proxy-Authorization",
			"Te",
			"Trailer",
			"Transfer-Encoding",
			"Upgrade",
			"Content-Length":
			continue
		}
		//（X-Request-ID、X-Accel-Buffering 等），Set 会把它们覆盖掉。
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// classifyUpstreamError 把上游调用错误映射成 HTTP 状态码 + OpenAI 错误类型。
func classifyUpstreamError(err error) (int, string) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, ErrTypeTimeout
	case errors.Is(err, context.Canceled):
		return 499, ErrTypeInvalidRequest
	default:
		return http.StatusBadGateway, ErrTypeUpstream
	}
}

// pickKey 从 key 池里挑一个非空 key。
//
// 当前策略：第一个非空值。
// 所以现在就抽成函数，别内联。
// func pickKey(keys []string) string {
// 	for _, k := range keys {
// 		if k != "" {
// 			return k
// 		}
// 	}
// 	return ""
// }

// writeJSON 把 v 序列化成 JSON 写入响应。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// 响应头已经发出去了，这里只能放弃（access_log 里能看到状态码）。
		slog.Warn("ai_write_json_failed", "err", err)
	}
}

// writeError 返回 OpenAI 风格的错误响应。
func writeError(w http.ResponseWriter, status int, errType, msg string) {
	writeJSON(w, status, ErrorResponseBody{Error: ErrorDetail{
		Message: msg,
		Type:    errType,
	}})
}

// maxRequestBody 是请求体上限（16 MiB）。
// AI 请求可能带很长的上下文或 base64 图片，但不能无上限地吃内存。
const maxRequestBody = 16 << 20
