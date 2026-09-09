// Package model 定义 FlowGate 的核心数据模型，
// 与 migrations/001_init.sql 中的 5 张表一一对应。
package model

import "time"

// Service 对应表 gateway_services（上游服务）。
type Service struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Protocol         string    `json:"protocol"`
	ConnectTimeoutMs int       `json:"connect_timeout_ms"`
	RequestTimeoutMs int       `json:"request_timeout_ms"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`

	// 重试
	MaxRetries     int    `json:"max_retries"`
	RetryOnStatus  string `json:"retry_on_status"`
	RetryBackoffMs int    `json:"retry_backoff_ms"`
	// 熔断
	CBFailureThreshold int `json:"cb_failure_threshold"`
	CBCooldownMs       int `json:"cb_cooldown_ms"`
	CBHalfOpenLimit    int `json:"cb_half_open_limit"`
	// 超时
	ResponseHeaderTimeoutMs int `json:"response_header_timeout_ms"`
}

// Node 对应表 upstream_nodes（上游实例）。
type Node struct {
	ID           int64     `json:"id"`
	ServiceID    int64     `json:"service_id"`
	Address      string    `json:"address"`
	Weight       int       `json:"weight"`
	Enabled      bool      `json:"enabled"`
	HealthStatus string    `json:"health_status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Route 对应表 gateway_routes（路由）。
type Route struct {
	ID            int64     `json:"id"`
	ServiceID     int64     `json:"service_id"`
	Name          string    `json:"name"`
	Host          string    `json:"host"`
	PathPattern   string    `json:"path_pattern"`
	PathMatchType string    `json:"path_match_type"`
	Methods       string    `json:"methods"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Version 对应表 route_versions（配置版本）。
type Version struct {
	ID          int64      `json:"id"`
	Version     int        `json:"version"`
	Status      string     `json:"status"`
	ConfigHash  string     `json:"config_hash"`
	PublishedAt *time.Time `json:"published_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// VersionItem 对应表 route_version_items（版本条目快照）。
type VersionItem struct {
	ID               int64  `json:"id"`
	VersionID        int64  `json:"version_id"`
	ResourceType     string `json:"resource_type"`
	ResourceID       int64  `json:"resource_id"`
	ResourceSnapshot []byte `json:"resource_snapshot"` // JSONB 序列化后的字节
}
