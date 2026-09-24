package model

import "time"

// Tenant 对应表 tenants（租户）。
type Tenant struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`    // active / disabled
	QPSLimit  int64     `json:"qps_limit"` // 租户级qps
	CreatedAt time.Time `json:"created_at"`
}

// APIKey 对应表 api_keys（API Key）。
type APIKey struct {
	ID             int64      `json:"id"`
	TenantID       int64      `json:"tenant_id"`
	Name           string     `json:"name"`
	KeyHash        string     `json:"-"`          // 哈希不对外暴露
	Status         string     `json:"status"`     // active / disabled
	QPSLimit       int64      `json:"qps_limit"`  // 这把 key 的 QPS 限额
	TenantQPSLimit int64      `json:"-"`          // 所属租户的 QPS（GetByHash JOIN 时填，不落库、不对外）
	ExpiresAt      *time.Time `json:"expires_at"` // nil = 永不过期
	CreatedAt      time.Time  `json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at"`
}
