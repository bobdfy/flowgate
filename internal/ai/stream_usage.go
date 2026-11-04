package ai

import (
	"bytes"
	"encoding/json"
	"time"
)

type streamUsage struct {
	promptTokens     int64 // 请求体估算，转发前算一次
	completionBytes  int64 // 流式 delta 累计字节数
	promptActual     int64 // 上游 usage 里的 prompt_tokens
	completionActual int64 // 上游 usage 里的 completion_tokens
	total            int64
	seen             bool
	stripUsage       bool // 网关替客户端要的 usage：不发给客户端

	firstEventAt time.Time // 首个带正文的帧到达时刻，用于 TTFT
}

func newStreamUsage(body []byte, stripUsage bool) *streamUsage {
	return &streamUsage{
		promptTokens: estimatePromptTokens(body),
		stripUsage:   stripUsage,
	}
}

// sseChunk 是流式事件里我们关心的字段。
type sseChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
}

// hasContent 报告这一帧是否带了用户可见的正文。
//
// 上游首帧通常只有 {"delta":{"role":"assistant"}}，正文要晚一两帧才到；
// 把「首帧到达」当首 token 会让 TTFT 系统性偏乐观。
// reasoning_content 也算：对推理模型来说，那是客户端第一个能看到的东西。
func (c sseChunk) hasContent() bool {
	for _, ch := range c.Choices {
		if ch.Delta.Content != "" || ch.Delta.ReasoningContent != "" {
			return true
		}
	}
	return false
}

// parseChunk 解析一个 SSE 事件；data 为空或不是合法 JSON 时 ok=false。
func parseChunk(event []byte) (sseChunk, bool) {
	payload := ssePayload(event)
	if len(payload) == 0 {
		return sseChunk{}, false
	}
	var c sseChunk
	if err := json.Unmarshal(payload, &c); err != nil {
		return sseChunk{}, false
	}
	return c, true
}

// eventHasContent 是给转发器用的轻量判断：只看这一帧有没有正文。
// 转发器不持有 streamUsage（它是通用的），所以这里独立解析一次。
func eventHasContent(event []byte) bool {
	c, ok := parseChunk(event)
	return ok && c.hasContent()
}

// eventResult 是解析一帧后的结论。
type eventResult struct {
	hasContent bool // delta 里有正文
	usageOnly  bool // 只有 usage、没有 choices（可被吞掉）
}

// sink 返回给 StreamingForwarder 的钩子：读事件做统计，必要时吞掉 usage 帧。
func (u *streamUsage) sink() StreamEventSink {
	return func(event []byte, isLast bool) []byte {
		st := u.observe(event)

		// ★ 只有带正文的帧才算「首 token」。
		// 之前在解析之前就打点，首帧（role-only）一到 TTFT 就停表，
		// 报出来的数字比用户真实体感快 1~2 帧。
		if st.hasContent && u.firstEventAt.IsZero() {
			u.firstEventAt = time.Now()
		}

		if st.usageOnly && u.stripUsage && !isLast {
			return nil // writeEvent 会跳过 nil 事件
		}
		return event
	}
}

// observe 解析一个 SSE 事件: 累加 delta 内容，有 usage 就记录。
func (u *streamUsage) observe(event []byte) eventResult {
	c, ok := parseChunk(event)
	if !ok {
		return eventResult{}
	}
	for _, ch := range c.Choices {
		u.completionBytes += int64(len(ch.Delta.Content) + len(ch.Delta.ReasoningContent))
	}
	// usage 在最后一帧，中间帧可能是 "usage":null —— 所以用指针
	if c.Usage != nil && c.Usage.TotalTokens > 0 {
		u.total = int64(c.Usage.TotalTokens)
		u.promptActual = int64(c.Usage.PromptTokens)
		u.completionActual = int64(c.Usage.CompletionTokens)
		u.seen = true
	}
	return eventResult{
		hasContent: c.hasContent(),
		usageOnly:  c.Usage != nil && len(c.Choices) == 0,
	}
}

// prompt 返回 prompt token 数：上游给了就用真实值，否则用请求体估算。
func (u *streamUsage) prompt() int64 {
	if u.seen {
		return u.promptActual
	}
	return u.promptTokens
}

// completion 返回 completion token 数：上游给了就用真实值，否则按累计字节 / 4 估算。
func (u *streamUsage) completion() int64 {
	if u.seen {
		return u.completionActual
	}
	return u.completionBytes / 4
}

// estimated 兜底估算：prompt 按内容字符、completion 按累计字节 / 4。
func (u *streamUsage) estimated() int64 {
	return u.promptTokens + u.completionBytes/4
}

// actual 结算值：真实 usage 优先，没有就用估算。
func (u *streamUsage) actual() int64 {
	if u.seen {
		return u.total
	}
	return u.estimated()
}

// wantsStreamUsage 客户端是否显式要求流式响应带 usage。
func wantsStreamUsage(body []byte) bool {
	var p struct {
		StreamOptions struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
	}
	return json.Unmarshal(body, &p) == nil && p.StreamOptions.IncludeUsage
}

// ensureStreamUsage 往请求体里注入 stream_options.include_usage=true
// 保留 stream_options 里的其它字段；它如果不是对象，直接覆盖
func ensureStreamUsage(body []byte) ([]byte, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	opts := map[string]json.RawMessage{}
	if raw, ok := root["stream_options"]; ok {
		_ = json.Unmarshal(raw, &opts)
	}
	opts["include_usage"] = json.RawMessage("true")
	rawOpts, err := json.Marshal(opts)
	if err != nil {
		return nil, err
	}
	root["stream_options"] = rawOpts
	return json.Marshal(root)
}

// ssePayload 从事件里取data: 行；[DONE] 返回 nil。多个 data 行按 SSE 规范用 \n 拼接
func ssePayload(event []byte) []byte {
	var payload []byte
	for _, line := range bytes.Split(event, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[len("data:"):])
		if bytes.Equal(data, []byte("[DONE]")) {
			return nil
		}
		if len(payload) > 0 {
			payload = append(payload, '\n')
		}
		payload = append(payload, data...)
	}
	return payload
}

// estimatePromptTokens 只算 messages 内容，不把 JSON 结构算进去。
func estimatePromptTokens(body []byte) int64 {
	var req struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil || len(req.Messages) == 0 {
		return int64(len(body)) / 4
	}
	n := 0
	for _, m := range req.Messages {
		n += contentBytes(m.Content)
	}
	if n == 0 {
		return int64(len(body)) / 4
	}
	return int64(n)/4 + int64(4*len(req.Messages))
}

// contentBytes 兼容 content 是字符串或多模态数组；图片等非文本部分不估。
func contentBytes(raw json.RawMessage) int {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return len(s)
		}
		return 0
	}
	if raw[0] == '[' {
		var parts []struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &parts) == nil {
			n := 0
			for _, p := range parts {
				n += len(p.Text)
			}
			return n
		}
	}
	return 0
}
