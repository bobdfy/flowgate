package ai

import (
	"context"
	"net/http"
)

// Provider —— 所有供应商都要实现的基础接口（"锚接口"）。
type Provider interface {
	GetProviderType() string
}

// RequestHeadersHandler —— 改写上游请求头的能力（按需实现）。
type RequestHeadersHandler interface {
	OnRequestHeaders(ctx context.Context, apiName ApiName, header http.Header) error
}

// RequestBodyHandler —— 改写请求体的能力（按需实现）。
type RequestBodyHandler interface {
	OnRequestBody(ctx context.Context, apiName ApiName, body []byte) ([]byte, error)
}

// StreamingResponseBodyHandler 是"流式响应 chunk 级"的能力（按需实现）
// 这个接口是「拿到已分帧的 chunk，按厂商语义改写」。
type StreamingResponseBodyHandler interface {
	OnStreamingResponseBody(ctx context.Context, apiName ApiName, chunk []byte, isLastChunk bool) ([]byte, error)
}

// Capabilities 是构造期探测出来的能力位图。
type Capabilities struct {
	RequestHeaders bool
	RequestBody    bool
	StreamingBody  bool
}

// ProbeCapabilities 在构造期探测一个 Provider 支持哪些能力。
// 只在 NewProvider / 注册表构造时调用一次。三次类型断言的代价只付一次，之后热路径只读布尔值。
func ProbeCapabilities(p Provider) Capabilities {
	var c Capabilities
	_, c.RequestHeaders = p.(RequestHeadersHandler)
	_, c.RequestBody = p.(RequestBodyHandler)
	_, c.StreamingBody = p.(StreamingResponseBodyHandler)
	return c
}
