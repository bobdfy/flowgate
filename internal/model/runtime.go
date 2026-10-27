package model

import "time"

// NodeRuntime 是 gateway 内存里的实时节点状态（健康检查 / 熔断）。
//
// 和 upstream_nodes.health_status 不同：那个是持久化配置字段，
// 这个只反映当前进程最近一次检查的结果。
type NodeRuntime struct {
	ServiceID        int64      `json:"service_id"`
	NodeID           int64      `json:"node_id"`
	Address          string     `json:"address"`
	State            string     `json:"state"` // closed / half_open / open
	Healthy          bool       `json:"healthy"`
	Fails            int        `json:"fails"`
	OpenUntil        *time.Time `json:"open_until,omitempty"`
	HalfOpenInFlight int        `json:"half_open_in_flight"`
}

// AIRuntimeStatus 是 gateway 最近一次加载 AI 配置的结果。
type AIRuntimeStatus struct {
	Enabled       bool       `json:"enabled"`
	Version       int64      `json:"version"`
	LoadedAt      *time.Time `json:"loaded_at,omitempty"`
	ProviderCount int        `json:"provider_count"`
	ModelCount    int        `json:"model_count"`
	LastError     string     `json:"last_error,omitempty"`
	LastErrorAt   *time.Time `json:"last_error_at,omitempty"`
}

// GatewayRuntime 是 gateway 周期性上报到 Redis 的运行状态快照。
type GatewayRuntime struct {
	Instance    string          `json:"instance"`
	PublishedAt time.Time       `json:"published_at"`
	Nodes       []NodeRuntime   `json:"nodes"`
	AI          AIRuntimeStatus `json:"ai"`
}
