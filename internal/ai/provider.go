package ai

import (
	"context"
	"net/http"
)

// Provider —— 所有供应商都要实现的基础接口（"锚接口"）。
// 只要一个方法：报出自己是什么类型（openai / claude / ollama）。
type Provider interface {
	GetProviderType() string
}

// RequestHeadersHandler —— 改写上游请求头的能力（按需实现）。
// 典型用途：把网关自己的凭证换成上游凭证。
type RequestHeadersHandler interface {
	OnRequestHeaders(ctx context.Context, apiName ApiName, header http.Header) error
}

// RequestBodyHandler —— 改写请求体的能力（按需实现）。
// OpenAI 兼容的厂商直接 return body, nil（透传）即可。
type RequestBodyHandler interface {
	OnRequestBody(ctx context.Context, apiName ApiName, body []byte) ([]byte, error)
}

// StreamingResponseBodyHandler 是"流式响应 chunk 级"的能力（按需实现）
// ★ 当前没有任何 provider 实现它，也没有任何地方读 Caps.StreamingBody。
// 它是为"需要改流式 chunk 内容"的厂商预留的 ——
// 比如 Claude 的流式事件结构是
//
//	message_start / content_block_delta / message_delta / message_stop
//
// 和 OpenAI 的 choices[].delta 完全不同，将来接 Claude 时要在这里做转换。
// 层级别搞混：真正的 SSE 字节分帧在 internal/sse（那层不认识 JSON），
// 这个接口是「拿到已分帧的 chunk，按厂商语义改写」。
type StreamingResponseBodyHandler interface {
	OnStreamingResponseBody(ctx context.Context, apiName ApiName, chunk []byte, isLastChunk bool) ([]byte, error)
}

// Capabilities 是构造期探测出来的能力位图（对应借鉴总结的"改进点"）。
//
//	而能力和 provider 实例一样是"构造后不变"的。
//	在 NewProvider 里探一次、缓存成位图，热路径只读布尔值。
type Capabilities struct {
	RequestHeaders bool
	RequestBody    bool
	StreamingBody  bool
}

// ProbeCapabilities 在构造期探测一个 Provider 支持哪些能力。
// 只在 NewProvider / 注册表构造时调用一次。
//
// 三次类型断言的代价只付一次，之后热路径只读布尔值。
func ProbeCapabilities(p Provider) Capabilities {
	var c Capabilities
	_, c.RequestHeaders = p.(RequestHeadersHandler)
	_, c.RequestBody = p.(RequestBodyHandler)
	_, c.StreamingBody = p.(StreamingResponseBodyHandler)
	return c
}
