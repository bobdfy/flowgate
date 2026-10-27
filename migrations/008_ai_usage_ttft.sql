-- 流式请求的首 token 耗时。非流式请求没有这个概念，保持 0。
ALTER TABLE ai_usage ADD COLUMN IF NOT EXISTS ttft_ms INT NOT NULL DEFAULT 0;
