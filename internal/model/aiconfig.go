package model

import "time"

// AIProvider 对应表 ai_providers（上游厂商）。
type AIProvider struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Type         string    `json:"type"` // openai（当前只支持 OpenAI 兼容协议）
	Endpoint     string    `json:"endpoint"`
	UpstreamPath string    `json:"upstream_path"`
	APIKeyEnv    string    `json:"api_key_env"` // 环境变量名，密钥不落库
	APIKey       string    `json:"-"`           // 仅本地假上游用，永不返回给客户端
	StreamUsage  bool      `json:"stream_usage"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AIModel 对应表 ai_models（对外模型别名）。
type AIModel struct {
	Name          string    `json:"name"`
	ProviderName  string    `json:"provider_name"`
	UpstreamModel string    `json:"upstream_model"` // 空 = 与别名相同
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
