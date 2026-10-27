-- =====================================================
-- AI Gateway 配置表：模型/provider 从 ai.yaml 迁到 DB，admin 可在线管理。
-- gateway 轮询 ai_config_version 感知变更并热切换 handler。
-- =====================================================

-- 1. ai_providers —— 上游厂商
--    密钥默认只存环境变量名（api_key_env），避免明文落库；
--    api_key 仅用于本地假上游（mock/ollama），生产不要用。
CREATE TABLE IF NOT EXISTS ai_providers (
    id            BIGSERIAL PRIMARY KEY,
    name          VARCHAR(64)  NOT NULL UNIQUE,
    type          VARCHAR(32)  NOT NULL DEFAULT 'openai',
    endpoint      VARCHAR(512) NOT NULL,
    upstream_path VARCHAR(255) NOT NULL DEFAULT '',
    api_key_env   VARCHAR(128) NOT NULL DEFAULT '',
    api_key       VARCHAR(512) NOT NULL DEFAULT '',
    stream_usage  BOOLEAN      NOT NULL DEFAULT false,
    enabled       BOOLEAN      NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 2. ai_models —— 对外模型别名 → 上游真实模型
CREATE TABLE IF NOT EXISTS ai_models (
    name           VARCHAR(64) PRIMARY KEY,
    provider_name  VARCHAR(64) NOT NULL
        REFERENCES ai_providers(name) ON UPDATE CASCADE ON DELETE CASCADE,
    upstream_model VARCHAR(128) NOT NULL DEFAULT '',
    enabled        BOOLEAN     NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 3. ai_config_version —— 单行版本号，任何写操作 +1
CREATE TABLE IF NOT EXISTS ai_config_version (
    id      INTEGER PRIMARY KEY CHECK (id = 1),
    version BIGINT NOT NULL DEFAULT 0
);

INSERT INTO ai_config_version (id, version) VALUES (1, 0)
ON CONFLICT (id) DO NOTHING;
