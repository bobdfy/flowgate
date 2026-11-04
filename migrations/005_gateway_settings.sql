-- 运行时可配置的网关参数。
--
-- 目前只有 TPM 固定窗口长度：管理面写入，gateway 每 3 秒轮询并热切换。
-- 不预置数据行：没有行 = 还未通过控制台配置，gateway 用启动参数 -tpm-window 做初始值并回填。
CREATE TABLE IF NOT EXISTS gateway_settings (
    id                 BOOLEAN     PRIMARY KEY DEFAULT TRUE CHECK (id),
    tpm_window_seconds INTEGER     NOT NULL CHECK (tpm_window_seconds BETWEEN 1 AND 604800),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
