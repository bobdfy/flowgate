-- 1. tenants —— 租户
--    限流、配额、用量归属的基本单位。
CREATE TABLE tenants (
    id         BIGSERIAL PRIMARY KEY,
    name       VARCHAR(255) NOT NULL UNIQUE,             -- 租户名，唯一
    status     VARCHAR(20)  NOT NULL DEFAULT 'active',   -- active / disabled
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 2. api_keys —— 租户的 API Key
--    一个租户可以有多个 Key（不同用途、不同环境）。
CREATE TABLE api_keys (
    id           BIGSERIAL PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL REFERENCES tenants(id) ON DELETE CASCADE, 
                                                    -- 属于哪个租户
    name         VARCHAR(255) NOT NULL,             -- Key 的名字（如 "prod", "test"）
    key_hash     VARCHAR(64)  NOT NULL UNIQUE,      -- Key 的 SHA256 哈希（不存明文）
    status       VARCHAR(20)  NOT NULL DEFAULT 'active',       -- active / disabled
    expires_at   TIMESTAMPTZ,                       -- 过期时间；NULL = 永不过期
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ                        -- 最后使用时间（低频更新）
);

-- 索引：按租户查它的所有 Key
CREATE INDEX idx_api_keys_tenant_id ON api_keys(tenant_id);