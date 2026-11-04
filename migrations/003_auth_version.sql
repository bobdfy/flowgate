-- 鉴权配置版本号：api_keys / tenants 的写操作会 +1。
-- gateway 轮询该版本号，变化时清空进程内鉴权缓存，
-- 让 allowed_models、限额、停用状态等改动在 3 秒内生效，而不是等缓存 TTL 过期。
CREATE TABLE IF NOT EXISTS auth_version (
    id         BOOLEAN     PRIMARY KEY DEFAULT TRUE CHECK (id),
    version    BIGINT      NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO auth_version (id, version) VALUES (TRUE, 1)
ON CONFLICT (id) DO NOTHING;
