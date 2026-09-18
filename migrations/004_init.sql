-- 并发保护
ALTER TABLE gateway_services ADD COLUMN max_concurrency   INTEGER     NOT NULL DEFAULT 0;
ALTER TABLE gateway_services ADD COLUMN queue_timeout_ms  INTEGER     NOT NULL DEFAULT 0;
ALTER TABLE gateway_services ADD COLUMN overload_strategy VARCHAR(20) NOT NULL DEFAULT 'fail-fast';

-- Bulkhead：按路由限并发，让一条慢路由打不满整个服务
ALTER TABLE gateway_routes ADD COLUMN max_concurrency INTEGER NOT NULL DEFAULT 0;