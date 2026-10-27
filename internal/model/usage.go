package model

import "time"

// AIUsage 是一次 AI 请求的 token 用量流水（只记量，不记费用）。
type AIUsage struct {
	ID               int64     `json:"id,omitempty"`
	TenantID         int64     `json:"tenant_id"`
	KeyID            int64     `json:"key_id"`
	Model            string    `json:"model"` // 客户端请求的对外模型名
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	Estimated        bool      `json:"estimated"` // 上游没给 usage，token 数为估算
	StatusCode       int       `json:"status_code"`
	Streaming        bool      `json:"streaming"`
	DurationMS       int       `json:"duration_ms"`
	RequestID        string    `json:"request_id"`
	CreatedAt        time.Time `json:"created_at"`
}

// AIUsageSummary 是按某个维度聚合后的用量。
type AIUsageSummary struct {
	Group            string `json:"group"` // 模型名 / key_id / tenant_id / 日期
	Requests         int64  `json:"requests"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
	EstimatedCount   int64  `json:"estimated_count"`
}
