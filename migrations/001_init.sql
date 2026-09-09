-- =====================================================
-- FlowGate 初始化迁移：V1 核心表
-- 对应《FlowGate_项目规划书_聚焦实施版.md》第 9 节数据结构
-- =====================================================

-- 1. gateway_services —— 上游服务
--    一组提供相同功能的实例的统称，例如 user-service。
CREATE TABLE gateway_services (
    id                 BIGSERIAL PRIMARY KEY,                     -- 服务 ID
    name               VARCHAR(255) NOT NULL UNIQUE,              -- 服务名，全局唯一
    protocol           VARCHAR(20)  NOT NULL DEFAULT 'http',      -- 协议：http / grpc
    connect_timeout_ms INTEGER      NOT NULL DEFAULT 3000,        -- 建连超时（毫秒）
    request_timeout_ms INTEGER      NOT NULL DEFAULT 10000,       -- 请求总超时（毫秒）
    enabled            BOOLEAN      NOT NULL DEFAULT true,        -- 是否启用
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    -- 重试
    max_retries          INTEGER      NOT NULL DEFAULT 0,
    retry_on_status      VARCHAR(255) NOT NULL DEFAULT '',
    retry_backoff_ms     INTEGER      NOT NULL DEFAULT 100,
    -- 熔断
    cb_failure_threshold INTEGER      NOT NULL DEFAULT 5,
    cb_cooldown_ms       INTEGER      NOT NULL DEFAULT 10000,
    cb_half_open_limit   INTEGER      NOT NULL DEFAULT 1,
    -- 超时
    response_header_timeout_ms INTEGER NOT NULL DEFAULT 5000
);

-- 2. upstream_nodes —— 上游实例
--    一个服务下的具体地址，一个服务可以有多个实例。
CREATE TABLE upstream_nodes (
    id           BIGSERIAL PRIMARY KEY,
    service_id   BIGINT       NOT NULL REFERENCES gateway_services(id) ON DELETE CASCADE, -- 属于哪个服务
    address      VARCHAR(255) NOT NULL,                -- 地址，如 localhost:8091
    weight       INTEGER      NOT NULL DEFAULT 1,      -- 权重
    enabled      BOOLEAN      NOT NULL DEFAULT true,   -- 是否启用
    health_status VARCHAR(20) NOT NULL DEFAULT 'unknown', -- 健康状态 :unknown/healthy/unhealthy
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 3. gateway_routes —— 路由
--    决定哪个路径转发到哪个服务。
CREATE TABLE gateway_routes (
    id              BIGSERIAL PRIMARY KEY,
    service_id      BIGINT       NOT NULL REFERENCES gateway_services(id), -- 转发到哪个服务
    name            VARCHAR(255) NOT NULL,          -- 路由名
    path_pattern    VARCHAR(255) NOT NULL,          -- 路径匹配，如 /api/
    path_match_type VARCHAR(20)  NOT NULL DEFAULT 'prefix', -- 匹配类型：prefix / exact
    methods         VARCHAR(255) NOT NULL DEFAULT 'GET,POST,PUT,DELETE',  -- 允许的方法，逗号分隔
    enabled         BOOLEAN      NOT NULL DEFAULT true,     -- 是否启用
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    host VARCHAR(255) NOT NULL DEFAULT ''
);

-- 4. route_versions —— 配置版本
--    配置的"版本化"，每次发布生成一个新版本，可回滚。
CREATE TABLE route_versions (
    id           BIGSERIAL PRIMARY KEY,
    version      INTEGER      NOT NULL UNIQUE,          -- 版本号，递增
    status       VARCHAR(20)  NOT NULL DEFAULT 'draft', -- 状态：draft（草稿）/ published（已发布）
    config_hash  VARCHAR(64),                           -- 整版配置的哈希，用于校验一致性
    published_at TIMESTAMPTZ,                           -- 发布时间（草稿时为空）
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 5. route_version_items —— 版本条目
--    冻结某个版本里的资源快照，保证发布后不受后续草稿修改影响。
CREATE TABLE route_version_items (
    id                BIGSERIAL PRIMARY KEY,
    version_id        BIGINT      NOT NULL REFERENCES route_versions(id) ON DELETE CASCADE, -- 属于哪个版本
    resource_type     VARCHAR(20) NOT NULL,     -- 资源类型：service / node / route
    resource_id       BIGINT      NOT NULL,     -- 资源 ID
    resource_snapshot JSONB       NOT NULL      -- 该资源的完整快照（JSON）
);

-- 索引：加速按服务查实例、按版本查条目
CREATE INDEX idx_upstream_nodes_service_id ON upstream_nodes(service_id);
CREATE INDEX idx_gateway_routes_service_id ON gateway_routes(service_id);
CREATE INDEX idx_route_version_items_version_id ON route_version_items(version_id);
CREATE INDEX idx_route_versions_status_version ON route_versions(status, version);
