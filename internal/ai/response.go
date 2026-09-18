package ai

// ChatResponse 是 OpenAI Chat Completions 的响应体。
type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice 是一个回复选项。
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage 是 token 用量。
//
// 非流式响应里上游会在 body 的 usage 字段返回它；
// 流式响应里默认不返回（要客户端显式要求 stream_options.include_usage）。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ErrorResponseBody 是 OpenAI 风格的错误体。
type ErrorResponseBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail 是错误体的细节。
type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

// 网关产生的错误类型（写入 ErrorDetail.Type）。
const (
	ErrTypeInvalidRequest = "invalid_request_error" // 400：JSON 坏了、缺 model
	ErrTypeNotFound       = "not_found_error"       // 404：模型或接口不存在
	ErrTypeUpstream       = "upstream_error"        // 502：上游调用失败
	ErrTypeGateway        = "gateway_error"         // 500：网关自身出错
	ErrTypeTimeout        = "timeout_error"         // 504：上游超时
)
