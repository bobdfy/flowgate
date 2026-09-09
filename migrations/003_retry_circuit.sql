ALTER TABLE gateway_services
    -- 重试
    ADD COLUMN max_retries          INTEGER      NOT NULL DEFAULT 0,
    ADD COLUMN retry_on_status      VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN retry_backoff_ms     INTEGER      NOT NULL DEFAULT 100,
    -- 熔断
    ADD COLUMN cb_failure_threshold INTEGER      NOT NULL DEFAULT 5,
    ADD COLUMN cb_cooldown_ms       INTEGER      NOT NULL DEFAULT 10000,
    ADD COLUMN cb_half_open_limit   INTEGER      NOT NULL DEFAULT 1,
    -- 超时
    ADD COLUMN response_header_timeout_ms INTEGER NOT NULL DEFAULT 5000;