package ai

// ChatRequest 是 OpenAI Chat Completions 的请求体。
// 客户端（OpenAI SDK / curl）发来的 JSON 就反序列化进这个结构。
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream,omitempty"`
}

// Message 是对话里的一条消息。
type Message struct {
	Role    string `json:"role"`    // system / user / assistant
	Content string `json:"content"` // 消息内容
}

// ModelInfo 是 /v1/models 里的一项，字段与 OpenAI 保持一致。
type ModelInfo struct {
	ID      string `json:"id"`       // 对外模型名
	Object  string `json:"object"`   // 固定 "model"
	Created int64  `json:"created"`  // Unix 时间戳
	OwnedBy string `json:"owned_by"` // 供应商标识
}

// ModelList 是 /v1/models 的响应体。
type ModelList struct {
	Object string      `json:"object"` // 固定 "list"
	Data   []ModelInfo `json:"data"`
}
