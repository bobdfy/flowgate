package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bobdfy/flowgate/internal/identity"
	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/netguard"
	"github.com/bobdfy/flowgate/internal/observability"
	"github.com/bobdfy/flowgate/internal/quota"
)

// UsageRecorder 接收每次请求的 token 用量流水。
// 实现必须是非阻塞的：它跑在请求返回路径上。
type UsageRecorder interface {
	Record(model.AIUsage)
}

// ProviderEntry 一个上游供应商的完整信息: 连接的地址、用哪个 key、
// 以及负责协议转换的适配器。
type ProviderEntry struct {
	// Name 是上游供应商标识（openai / deepseek / claude / ollama），
	// 只用于日志和 /v1/models 的 owned_by，不参与路由。
	Name string

	// Provider 是协议转换器。
	Provider Provider

	// BaseURL 是上游根地址，如 https://api.deepseek.com（不带路径）。
	BaseURL string

	// UpstreamModel 是发给上游时实际使用的模型名；空 = 与客户端请求的相同。
	// 用于模型别名与灰度：对外 flowgate-chat，对内 gpt-4o-mini。
	UpstreamModel string

	// Caps 是构造期探测的能力位图。
	// 由 cmd/gateway 在装配时调 ProbeCapabilities(entry.Provider) 填充。
	Caps Capabilities

	// UpstreamPath 是上游真实路径；空 = 用 ApiName.Path()（默认行为）。
	UpstreamPath string

	// StreamUsage 表示上游支持 stream_options.include_usage。
	// 开启后网关会替没要 usage 的客户端向上游要，结算用真实值，再把 usage 帧吞掉。
	StreamUsage bool
}

// Handler 处理 AI 请求（OpenAI 兼容接口）。
type Handler struct {
	// providers 的 key 是"对外模型名"，不是上游模型名。
	// 这是模型白名单的唯一实现点。
	providers map[string]*ProviderEntry

	clients *Clients

	// streams 是流式转发器，可为 nil（退化成缓冲转发）。
	streams StreamingForwarder

	tpm quota.Limiter

	// usage 可为 nil（不记录用量）。
	usage UsageRecorder
}

// Timeout 是 AI 链路的分层超时
type Timeouts struct {
	Connect        time.Duration // TCP 建连
	ResponseHeader time.Duration // 发出请求到收到响应头
	Buffered       time.Duration // 非流式请求总时长
	StreamIdle     time.Duration // 流式读空闲： 多久没数据就断
	StreamMax      time.Duration // 流式总时长上限: 0 = 不限
}

func DefaultTimeouts() Timeouts {
	return Timeouts{5 * time.Second, 60 * time.Second, 60 * time.Second, 60 * time.Second, 0}
}

// Clients 把流式和非流式两种超时语义分开
type Clients struct {
	Stream   *http.Client
	Buffered *http.Client
	timeouts *Timeouts
}

// NewClient 构造 AI 链路的 HTTP 客户端。policy 为 nil 时默认拒绝所有内网上游。
//
//	客户端要由调用方创建一次并长期复用，不要在每次重建 Handler 时新建
func NewClients(policy *netguard.Policy, t Timeouts) *Clients {
	tr := &http.Transport{
		// 拨号层校验与普通转发共用 netguard，不因为走的是 AI 链路就放宽。
		DialContext: policy.Dialer(t.Connect).DialContext,

		MaxIdleConnsPerHost: 64,

		// 即使 Transport 被换掉，空闲连接也会在 90s 后自行关闭，不会无限堆积。
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: t.ResponseHeader,
	}

	noRedirect := func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &Clients{
		// 流式
		Stream: &http.Client{Transport: tr, CheckRedirect: noRedirect},

		// 非流式
		Buffered: &http.Client{Timeout: t.Buffered, Transport: tr, CheckRedirect: noRedirect},

		timeouts: &t,
	}
}

// NewHandler 创建 AI 处理器。client 由调用方持有并复用（见 NewClient）。
func NewHandler(provider map[string]*ProviderEntry, clients *Clients) *Handler {
	if clients == nil {
		clients = NewClients(nil, DefaultTimeouts()) // 兜底：宁可全拒，也不能变成不校验
	}
	return &Handler{
		providers: provider,
		clients:   clients,
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
func (h *Handler) SetClient(c *Clients) {
	if c != nil {
		h.clients = c
	}
}

// RegisterRoutes 把所有已注册的 AI 接口挂到 mux 上。
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	for _, rt := range RegisteredRoutes() {
		mux.Handle(rt.Method+" "+rt.Path, h)
	}
}

// SetTPM 注入 Token 级配额限流器。nil 表示不启用 TPM。
func (h *Handler) SetTPM(l quota.Limiter) {
	h.tpm = l
}

// SetUsageRecorder 注入用量记录器。nil 表示不记录。
func (h *Handler) SetUsageRecorder(r UsageRecorder) {
	h.usage = r
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
		h.serveModels(w, r)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, ErrTypeInvalidRequest,
				fmt.Sprintf("请求体超过上限 %d 字节", maxRequestBody))
			return
		}
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

	// 5.1 Key 级模型白名单：配置了 allowed_models 的 key 只能用列出的模型。
	if !modelAllowed(r.Context(), req.Model) {
		slog.Warn("ai_model_forbidden", "model", req.Model, "path", r.URL.Path)
		writeError(w, http.StatusForbidden, ErrTypePermission,
			"该 API Key 无权使用模型: "+req.Model)
		return
	}

	// 5.2 用量流水：模型解析通过后开始记录，状态码由 statusWriter 捕获。
	// 这次的 w 会被后面所有写操作共用，flush 通过 Unwrap 仍然能拿到原始 writer。
	start := time.Now()
	sw := &statusWriter{ResponseWriter: w}
	w = sw
	rec := model.AIUsage{
		Model:     req.Model,
		RequestID: r.Header.Get("X-Request-ID"),
	}
	if id, ok := identity.FromContext(r.Context()); ok {
		rec.TenantID, rec.KeyID = id.TenantID, id.KeyID
	}
	var streaming bool
	defer func() {
		rec.Streaming = streaming
		rec.DurationMS = int(time.Since(start).Milliseconds())
		rec.StatusCode = sw.status
		if rec.StatusCode == 0 {
			rec.StatusCode = 499 // 客户端断开，没来得及回写
		}
		if h.usage != nil {
			h.usage.Record(rec)
		}
	}()

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

	// 8. 单点判定：走 buffered 还是 streaming，只判一次。
	streaming = isStreamRequest(apiName, body)

	// 8.1 上游支持时，替没要 usage 的客户端向上游要：
	// 结算用真实 token，客户端那边再把多出来的 usage 帧吞掉。
	stripUsage := false
	if streaming && entry.StreamUsage && !wantsStreamUsage(body) {
		if injected, ierr := ensureStreamUsage(body); ierr != nil {
			slog.Warn("ai_stream_usage_inject_failed", "model", req.Model, "err", ierr)
		} else {
			body = injected
			stripUsage = true
		}
	}

	// TPM 预扣: 转发前先占额度，避免并发下集体穿透
	var settler *tpmSettler
	if h.tpm != nil {
		if id, ok := identity.FromContext(r.Context()); ok {
			need := estimateNeed(body)
			res, d, qerr := h.tpm.Reserve(r.Context(), id.TenantID, need, id.TenantTPM)
			switch {
			case errors.Is(qerr, quota.ErrUnavailable):
				writeError(w, http.StatusServiceUnavailable, ErrTypeGateway, "配额服务暂不可用，请稍后重试")
				return
			case qerr != nil:
				slog.Error("tpm_reserve_failed",
					"tenant_id", id.TenantID, "model", req.Model, "err", qerr)
				writeError(w, http.StatusInternalServerError, ErrTypeGateway, "配额校验失败")
				return
			case !d.Allowed:
				retryAfter := int(math.Ceil(d.RetryAfter.Seconds()))
				if retryAfter > 0 {
					w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				}
				slog.Warn("tpm_limited",
					"tenant_id", id.TenantID, "model", req.Model,
					"used", d.Used, "limit", d.Limit, "need", need)
				writeError(w, http.StatusTooManyRequests, ErrTypeLimit,
					fmt.Sprintf("本窗口 TPM 配额已用尽：已用 %d / 上限 %d，%d 秒后重置",
						d.Used, d.Limit, retryAfter))
				return
			default:
				settler = &tpmSettler{ql: h.tpm, res: res, base: r.Context()}
				defer settler.Settle(0)
			}
		}
	}

	// 9. 发上游请求。
	resp, err := h.forward(r, entry, apiName, header, body, streaming)
	if err != nil {
		// C8：客户端主动取消时连接已断开，回写无意义，别把取消误报成非标准 499。
		if errors.Is(err, context.Canceled) {
			slog.Info("ai_forward_canceled",
				"model", req.Model, "provider", entry.Name, "api", string(apiName))
			return
		}
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
		h.serveStream(w, r, resp, req.Model, body, settler, stripUsage, &rec)
		return
	}
	u := h.serveBuffered(w, resp, req.Model)
	rec.PromptTokens = int64(u.PromptTokens)
	rec.CompletionTokens = int64(u.CompletionTokens)
	rec.TotalTokens = int64(u.TotalTokens)
	if settler != nil {
		settler.Settle(int64(u.TotalTokens))
	}

}

// modelAllowed 判断当前请求的 API Key 是否有权使用该模型。
// 没有身份（理论上不会发生）或白名单为空 → 不限制。
func modelAllowed(ctx context.Context, model string) bool {
	id, ok := identity.FromContext(ctx)
	if !ok || len(id.AllowedModels) == 0 {
		return true
	}
	for _, m := range id.AllowedModels {
		if m == model {
			return true
		}
	}
	return false
}

// serveModels 处理 GET /v1/models。
// 不把上游的全部模型名泄露给客户端；配置了 allowed_models 的 key 只看到自己的模型。
func (h *Handler) serveModels(w http.ResponseWriter, r *http.Request) {
	list := ModelList{Object: "list", Data: make([]ModelInfo, 0, len(h.providers))}
	// C9：map 遍历顺序不稳定，先排序再构造，保证 /v1/models 返回稳定（也避免 golden 测试 flaky）。
	names := make([]string, 0, len(h.providers))
	for name := range h.providers {
		if !modelAllowed(r.Context(), name) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := h.providers[name]
		owned := "flowgate"
		if entry != nil && entry.Name != "" {
			owned = entry.Name
		}
		list.Data = append(list.Data, ModelInfo{ID: name, Object: "model", OwnedBy: owned})
	}
	writeJSON(w, http.StatusOK, list)
}

// serveBuffered 处理非流式响应：整读上游 body → 透传给客户端。
// 401 = 上游 key 配错了，429 = 被上游限流，
// 这两种和"网关自己 502"是完全不同的问题，混掉之后排障要多花几小时。
func (h *Handler) serveBuffered(w http.ResponseWriter, resp *http.Response, model string) Usage {
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("ai_read_upstream_failed", "model", model, "err", err)
		writeError(w, http.StatusBadGateway, ErrTypeUpstream,
			"读取上游响应失败: "+err.Error())
		return Usage{}
	}
	if resp.StatusCode >= 400 {
		copySafeHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return Usage{}
	}

	copySafeHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)

	// 解析 usage 用于 TPM 结算和用量流水；解析失败不影响响应（已经写出去了）。
	var cr ChatResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return Usage{}
	}
	return cr.Usage
}

// serveStream 处理流式响应：交给注入的 SSE 转发器。
// rec 不为 nil 时，把流式统计写回去（prompt/completion 可能是估算值）。
func (h *Handler) serveStream(w http.ResponseWriter, r *http.Request, resp *http.Response, model string, body []byte, settler *tpmSettler, stripUsage bool, rec *model.AIUsage) {
	if h.streams == nil {
		slog.Warn("ai_stream_forwarder_missing", "model", model)
		u := h.serveBuffered(w, resp, model)
		if rec != nil {
			rec.PromptTokens = int64(u.PromptTokens)
			rec.CompletionTokens = int64(u.CompletionTokens)
			rec.TotalTokens = int64(u.TotalTokens)
		}
		if settler != nil {
			settler.Settle(int64(u.TotalTokens))
		}
		return
	}

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		copySafeHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}

	prepareStreamHeaders(w, resp)
	w.WriteHeader(http.StatusOK)

	usage := newStreamUsage(body, stripUsage)

	if err := h.streams(w, r, resp.Body, usage.sink(), model); err != nil {
		reason := "upstream_error"
		if b, ok := resp.Body.(*streamBody); ok {
			switch {
			case b.idleHit.Load():
				reason = "idle_timeout" // 上游不说话
			case r.Context().Err() != nil:
				reason = "client_cancel" // 用户自己走了
			case errors.Is(err, context.DeadlineExceeded):
				reason = "max_duration" // 说太久了
			}
		}
		observability.AIStreamTerminatedTotal.WithLabelValues(reason).Inc()
		slog.Warn("ai_stream_failed", "model", model, "reason", reason, "err", err)
	} else {
		observability.AIStreamTerminatedTotal.WithLabelValues("normal").Inc()
	}
	if settler != nil {
		settler.Settle(usage.actual())
	}
	if rec != nil {
		rec.PromptTokens = usage.prompt()
		rec.CompletionTokens = usage.completion()
		rec.TotalTokens = usage.actual()
		rec.Estimated = !usage.seen
	}
	if usage.seen {
		slog.Info("tpm_stream_usage",
			"model", model,
			"actual", usage.total,
			"estimated", usage.estimated(),
			"prompt_est", usage.promptTokens,
			"completion_est", usage.completionBytes/4,
			"ratio", float64(usage.estimated())/float64(usage.total))
	}
}

// forward 把请求发到上游，返回上游响应。

func (h *Handler) forward(r *http.Request, entry *ProviderEntry, apiName ApiName, header map[string][]string, body []byte, streaming bool) (*http.Response, error) {
	path := apiName.Path()
	if entry.UpstreamPath != "" {
		path = entry.UpstreamPath
	}

	url := joinURL(entry.BaseURL, path)

	ctx := r.Context()
	var cancels []context.CancelFunc
	if streaming {
		// 流式必须始终有一个可取消的 ctx：读空闲计时器要靠它把阻塞的 Read 打断，
		// 不能只在配了总时长上限时才建 —— 否则 StreamMax=0（默认）时空闲超时是哑的。
		var cancelIdle context.CancelFunc
		ctx, cancelIdle = context.WithCancel(ctx)
		cancels = append(cancels, cancelIdle)

		if h.clients.timeouts.StreamMax > 0 {
			var cancelMax context.CancelFunc
			ctx, cancelMax = context.WithTimeout(ctx, h.clients.timeouts.StreamMax)
			cancels = append(cancels, cancelMax)
		}
	}

	req, err := http.NewRequestWithContext(ctx, r.Method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	//  先放客户端来的头（白名单过滤）
	copySafeRequestHeaders(req.Header, header)

	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// 让 provider 改「真实的上游请求头」
	if entry.Caps.RequestHeaders {
		if hh, ok := entry.Provider.(RequestHeadersHandler); ok {
			if herr := hh.OnRequestHeaders(r.Context(), apiName, req.Header); herr != nil {
				return nil, fmt.Errorf("provider.OnRequestHeaders: %w", herr)
			}
		}
	}

	// 显式设长度：bytes.NewReader 已经能让包自动推出来，
	req.ContentLength = int64(len(body))

	if streaming {
		req.Header.Set("Accept", "text/event-stream")

		// C5：显式设 identity 才能真正禁用压缩 —— http.Transport 在 header 缺省时会自动补 gzip。
		req.Header.Set("Accept-Encoding", "identity")

	} else {
		req.Header.Set("Accept", "application/json")
	}

	c := h.clients.Buffered
	if streaming {
		c = h.clients.Stream
	}

	resp, err := c.Do(req)
	if err != nil {
		for _, f := range cancels {
			f()
		} // Do 失败时没人会 Close，这里兜住
		return nil, err
	}
	if streaming {
		resp.Body = newStreamBody(resp.Body, h.clients.timeouts.StreamIdle, func() {
			for _, f := range cancels {
				f()
			}
		})
	}
	return resp, nil

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
// Connection / Keep-Alive 等 —— 逐跳头，只对"上游---网关"这一跳有意义
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
	// context.Canceled 已在上层单独处理（客户端断开，不回写）。
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, ErrTypeTimeout
	default:
		return http.StatusBadGateway, ErrTypeUpstream
	}
}

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

// statusWriter 只做一件事：记住最终写出的状态码，供用量流水使用。
// 实现 Unwrap，http.NewResponseController 才能继续拿到 Flush 等能力。
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// maxRequestBody 是请求体上限（16 MiB）。
const maxRequestBody = 16 << 20
