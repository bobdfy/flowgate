-- 模型级并发上限（0 = 不限）。
--
-- 为什么需要：实例级并发护栏是所有模型共用一个池子。某个模型的慢流会一直占着名额
-- （名额跟着整个请求生命周期走，流式请求可能持续数十秒），把其他模型挤到 429。
-- 加上这一列之后，每个模型有自己的名额，互不抢占。
ALTER TABLE ai_models ADD COLUMN IF NOT EXISTS max_concurrency INT NOT NULL DEFAULT 0;
