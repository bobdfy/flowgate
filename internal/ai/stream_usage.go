package ai

import (
	"bytes"
	"encoding/json"
)

type streamUsage struct {
	promptTokens     int64 // 请求体估算，转发前算一次
	completionBytes  int64 // 流式 delta 累计字节数
	promptActual     int64 // 上游 usage 里的 prompt_tokens
	completionActual int64 // 上游 usage 里的 completion_tokens
	total            int64
	seen             bool
	stripUsage       bool // 网关替客户端要的 usage：不发给客户端
}

func newStreamUsage(body []byte, stripUsage bool) *streamUsage {
	return &streamUsage{
		promptTokens: estimatePromptTokens(body),
		stripUsage:   stripUsage,
	}
}

// sink 返回给 StreamingForwarder 的钩子：读事件做统计，必要时吞掉 usage 帧。
func (u *streamUsage) sink() StreamEventSink {
	return func(event []byte, isLast bool) []byte {
		usageOnly := u.observe(event)
		if usageOnly && u.stripUsage && !isLast {
			return nil // writeEvent 会跳过 nil 事件
		}
		return event
	}
}

// observe 解析一个 SSE 事件: 累加 delta 内容，有 usage 就记录。
// 返回 true 表示这是只有 usage 的帧（choices 为空），供上层决定要不要吞掉。
func (u *streamUsage) observe(event []byte) bool {
	payload := ssePayload(event)
	if len(payload) == 0 {
		return false
	}
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"delta"`
		} `json:"choices"`
		Usage *Usage `json:"usage"`
	}
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return false
	}
	for _, c := range chunk.Choices {
		u.completionBytes += int64(len(c.Delta.Content) + len(c.Delta.ReasoningContent))
	}
	// usage 在最后一帧，中间帧可能是 "usage":null —— 所以用指针
	if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
		u.total = int64(chunk.Usage.TotalTokens)
		u.promptActual = int64(chunk.Usage.PromptTokens)
		u.completionActual = int64(chunk.Usage.CompletionTokens)
		u.seen = true
	}
	return chunk.Usage != nil && len(chunk.Choices) == 0
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

// ensureStreamUsage 往请求体里注入 stream_options.include_usage=true。
// 保留 stream_options 里的其它字段；它如果不是对象，直接覆盖。
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
