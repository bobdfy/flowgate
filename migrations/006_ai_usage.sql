-- AI 请求 token 用量流水。
--
-- 每完成一次 /v1/chat/completions 请求写一行（异步批量写入，不在请求路径上）。
-- 不建外键：租户 / Key 删除后历史用量仍要保留。
CREATE TABLE IF NOT EXISTS ai_usage (
    id                BIGSERIAL   PRIMARY KEY,
    tenant_id         BIGINT      NOT NULL,
    key_id            BIGINT      NOT NULL,
    model             TEXT        NOT NULL,
    prompt_tokens     BIGINT      NOT NULL DEFAULT 0,
    completion_tokens BIGINT      NOT NULL DEFAULT 0,
    total_tokens      BIGINT      NOT NULL DEFAULT 0,
    estimated         BOOLEAN     NOT NULL DEFAULT FALSE, -- 上游没给 usage 时为估算值
    status_code       INT         NOT NULL DEFAULT 0,
    streaming         BOOLEAN     NOT NULL DEFAULT FALSE,
    duration_ms       INT         NOT NULL DEFAULT 0,
    request_id        TEXT        NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ai_usage_tenant_time ON ai_usage (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_usage_key_time    ON ai_usage (key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_usage_model_time  ON ai_usage (model, created_at DESC);
