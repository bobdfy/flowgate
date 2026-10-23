-- =====================================================
-- API Key 模型白名单：限制一把 key 只能调用指定模型。
-- 空数组 = 不限制（兼容已有 key）。
-- =====================================================
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS allowed_models TEXT[] NOT NULL DEFAULT '{}';
