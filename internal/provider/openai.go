package provider

import (
	"context"
	"net/http"

	"github.com/bobdfy/flowgate/internal/ai"
)

// OpenAIProvider 对接 OpenAI 协议兼容的供应商
type OpenAIProvider struct {
	name string // 供应商标识，日志与 /v1/models 的 owned_by 用
	//baseURL string   // 上游根地址，如 https://api.deepseek.com
	apiKeys []string // key 池；当前只取第一个，第四阶段接 failover
}

// NewOpenAIProvider 创建一个 OpenAI 兼容供应商。
// apiKeys 允许为空（本地 Ollama 无鉴权）。
func NewOpenAIProvider(name string, apiKeys []string) *OpenAIProvider {
	return &OpenAIProvider{name: name, apiKeys: apiKeys}
}

// GetProviderType 实现 ai.Provider。
func (p *OpenAIProvider) GetProviderType() string {
	return p.name
}

// OnRequestHeaders 实现 ai.RequestHeadersHandler：
// 把请求头里的凭证换成网关自己配置的上游 key。
func (p *OpenAIProvider) OnRequestHeaders(ctx context.Context, apiName ai.ApiName, header http.Header) error {
	key := p.pickKey()
	if key == "" {
		header.Del("Authorization")
		return nil
	}
	header.Set("Authorization", "Bearer "+key)
	return nil
}

// OnRequestBody 实现 ai.RequestBodyHandler：
// OpenAI 格式对外即上游原生格式，直接透传。
func (p *OpenAIProvider) OnRequestBody(ctx context.Context, apiName ai.ApiName, body []byte) ([]byte, error) {
	return body, nil
}

// pickKey 返回本次请求要用的上游 key。
func (p *OpenAIProvider) pickKey() string {
	for _, k := range p.apiKeys {
		if k != "" {
			return k
		}
	}
	return ""
}
