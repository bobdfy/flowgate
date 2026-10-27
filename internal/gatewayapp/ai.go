package gatewayapp

// 网关对 AI Gateway 的装配代码。

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bobdfy/flowgate/internal/ai"
	"github.com/bobdfy/flowgate/internal/middleware"
	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/netguard"
	"github.com/bobdfy/flowgate/internal/provider"
	"github.com/bobdfy/flowgate/internal/quota"
	"github.com/bobdfy/flowgate/internal/store/postgres"
	"go.yaml.in/yaml/v2"
)

// aiProviderConfig 是一个上游厂商的配置。
type aiProviderConfig struct {
	Name         string   // 厂商标识，日志和 /v1/models 的 owned_by 用
	Endpoint     string   // 根地址，不带路径
	UpstreamPath string   // 上游真实路径；空 = 用对外的 /v1/...
	APIKeys      []string // key 池
	RawAPIKeys   []string // 配置文件原文（含 ${ENV}），用于首次导入 DB
	Enabled      bool
	StreamUsage  bool // 上游支持 stream_options.include_usage
}

// aiModelConfig 是"对外模型名 → 某厂商的某模型"的映射。
type aiModelConfig struct {
	Name          string // 客户端请求体里写的模型名
	Provider      string // 引用 aiProviderConfig.Name
	UpstreamModel string // 发给上游的真实模型名
}

// aiConfig 是 AI Gateway 的完整配置。
type AIConfig struct {
	Enabled   bool
	Providers map[string]aiProviderConfig
	Models    []aiModelConfig
}

// yamlConfig 是 ai.yaml 的结构，和文件一一对应。
type yamlConfig struct {
	Providers []struct {
		Name         string   `yaml:"name"`
		Type         string   `yaml:"type"` // openai / claude / ... ；留空按 openai
		Endpoint     string   `yaml:"endpoint"`
		UpstreamPath string   `yaml:"upstream_path"`
		APIKeys      []string `yaml:"api_keys"`
		Enabled      bool     `yaml:"enabled"`
		StreamUsage  bool     `yaml:"stream_usage"`
	} `yaml:"providers"`

	Models []struct {
		Name          string `yaml:"name"`
		Provider      string `yaml:"provider"`
		UpstreamModel string `yaml:"upstream_model"`
	} `yaml:"models"`
}

// LoadAIConfig 读 ai.yaml 并转成运行时配置。
// 文件存在但格式错 → 返回 error，调用方记日志后降级
func LoadAIConfig(path string) (*AIConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}

	var yc yamlConfig
	if err := yaml.Unmarshal(raw, &yc); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}

	cfg := &AIConfig{
		Enabled:   true,
		Providers: make(map[string]aiProviderConfig, len(yc.Providers)),
	}

	for _, p := range yc.Providers {
		if p.Name == "" {
			return nil, fmt.Errorf("%s: provider 缺少 name 字段", path)
		}
		if p.Endpoint == "" {
			return nil, fmt.Errorf("%s: provider %q 缺少 endpoint", path, p.Name)
		}

		// 展开 ${ENV_VAR}：配置文件可以提交到 git，key 从环境变量来。
		keys := expandEnv(p.APIKeys)

		// enabled 和"有没有 key"都要满足。
		// 这样"配了但没设环境变量"会自动跳过，不用手动改 enabled。
		enabled := p.Enabled && len(keys) > 0

		cfg.Providers[strings.ToLower(p.Name)] = aiProviderConfig{
			Name:         p.Name,
			Endpoint:     p.Endpoint,
			UpstreamPath: p.UpstreamPath,
			APIKeys:      keys,
			RawAPIKeys:   p.APIKeys,
			Enabled:      enabled,
			StreamUsage:  p.StreamUsage,
		}

		if p.Enabled && !enabled {
			slog.Warn("ai_provider_no_key",
				"provider", p.Name,
				"reason", "配置里 enabled=true 但 api_keys 展开后为空(环境变量没设？)")
		}
	}

	for _, m := range yc.Models {
		if m.Name == "" || m.Provider == "" {
			return nil, fmt.Errorf("%s: model 缺少 name 或 provider 字段", path)
		}
		cfg.Models = append(cfg.Models, aiModelConfig{
			Name:          m.Name,
			Provider:      strings.ToLower(m.Provider),
			UpstreamModel: m.UpstreamModel,
		})
	}

	return cfg, nil
}

// expandEnv 把 ["${ZHIPU_API_KEY}"] 展开成 ["sk-xxx"]。
func expandEnv(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if v := strings.TrimSpace(os.ExpandEnv(s)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// aiRuntime 持有当前 AI handler，并在 DB 配置版本变化时热切换。
//
// 配置来源优先级：DB > ai.yaml。
// 启动时若 DB 为空，用 ai.yaml 做一次性种子；DB 查询失败则保留旧 handler。
type AIRuntime struct {
	store   *postgres.AIConfigStore
	tpm     quota.Limiter
	usage   *postgres.UsageStore // 可为 nil（不记录用量）
	clients *ai.Clients          // 复用同一个连接池：重建 handler 时不新建 Transport
	handler atomic.Pointer[ai.Handler]
	version atomic.Int64

	// 最近一次加载结果，供网关状态上报（/api/v1/runtime/ai）读取。
	statusMu sync.RWMutex
	status   model.AIRuntimeStatus
}

// NewAIRuntime 构建运行时：先用 ai.yaml 兜底，再尝试导入种子并切到 DB 配置。
func NewAIRuntime(ctx context.Context, cfg AIConfig, tpm quota.Limiter, store *postgres.AIConfigStore, usage *postgres.UsageStore, policy *netguard.Policy, t ai.Timeouts) *AIRuntime {
	rt := &AIRuntime{store: store, tpm: tpm, usage: usage, clients: ai.NewClients(policy, t)}

	rt.handler.Store(buildAIHandler(cfg, rt.tpm, rt.usage, rt.clients))
	rt.status.Enabled = cfg.Enabled
	if cfg.Enabled && len(cfg.Models) > 0 {
		now := time.Now().UTC()
		rt.status.LoadedAt = &now
		rt.status.ProviderCount = len(cfg.Providers)
		rt.status.ModelCount = len(cfg.Models)
	}

	if store == nil || !cfg.Enabled {
		return rt
	}

	if n, err := store.CountProviders(ctx); err != nil {
		slog.Warn("ai_config_db_unavailable", "err", err)
	} else if n == 0 {
		providers, models := aiSeedFromYAML(cfg)
		if len(providers) > 0 {
			seeded, err := store.Seed(ctx, providers, models)
			if err != nil {
				slog.Warn("ai_config_seed_failed", "err", err)
			} else if seeded {
				slog.Info("ai_config_seeded", "providers", len(providers), "models", len(models))
			}
		}
	}

	rt.reload(ctx)
	return rt
}

// reload 从 DB 拉一份快照并重建 handler。失败时保留当前 handler（fail-static）。
func (rt *AIRuntime) reload(ctx context.Context) {
	providers, models, version, err := rt.store.Snapshot(ctx)
	if err != nil {
		slog.Warn("ai_config_reload_failed", "err", err)
		rt.recordError(err)
		return
	}
	cfg := aiConfigFromDB(providers, models)
	rt.handler.Store(buildAIHandler(cfg, rt.tpm, rt.usage, rt.clients))
	rt.version.Store(version)
	rt.statusMu.Lock()
	now := time.Now().UTC()
	rt.status.Version = version
	rt.status.LoadedAt = &now
	rt.status.ProviderCount = len(providers)
	rt.status.ModelCount = len(models)
	rt.status.LastError = ""
	rt.status.LastErrorAt = nil
	rt.statusMu.Unlock()
	slog.Info("ai_config_reloaded",
		"version", version,
		"provider_count", len(providers),
		"model_count", len(models))
}

// recordError 记录最近一次加载失败；已生效的配置不受影响（fail-static）。
func (rt *AIRuntime) recordError(err error) {
	rt.statusMu.Lock()
	defer rt.statusMu.Unlock()
	now := time.Now().UTC()
	rt.status.LastError = err.Error()
	rt.status.LastErrorAt = &now
}

// Status 返回最近一次 AI 配置加载结果（值拷贝）。
func (rt *AIRuntime) Status() model.AIRuntimeStatus {
	rt.statusMu.RLock()
	defer rt.statusMu.RUnlock()
	return rt.status
}

// Start 每 3 秒查一次版本号，变了才重建 handler。
func (rt *AIRuntime) Start(ctx context.Context) {
	if rt.store == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				v, err := rt.store.Version(ctx)
				if err != nil {
					slog.Warn("ai_config_version_failed", "err", err)
					rt.recordError(err)
					continue
				}
				if v == rt.version.Load() {
					continue
				}
				rt.reload(ctx)
			}
		}
	}()
}

// Handler 返回当前生效的 AI handler（可能为 nil）。
func (rt *AIRuntime) Handler() *ai.Handler {
	return rt.handler.Load()
}

// aiConfigFromDB 把 DB 快照转成 buildAIHandler 认识的配置。
func aiConfigFromDB(providers []model.AIProvider, models []model.AIModel) AIConfig {
	cfg := AIConfig{
		Enabled:   true,
		Providers: make(map[string]aiProviderConfig, len(providers)),
	}
	for _, p := range providers {
		keys := make([]string, 0, 2)
		if p.APIKeyEnv != "" {
			if v := strings.TrimSpace(os.Getenv(p.APIKeyEnv)); v != "" {
				keys = append(keys, v)
			}
		}
		if p.APIKey != "" {
			keys = append(keys, p.APIKey)
		}
		cfg.Providers[strings.ToLower(p.Name)] = aiProviderConfig{
			Name:         p.Name,
			Endpoint:     p.Endpoint,
			UpstreamPath: p.UpstreamPath,
			APIKeys:      keys,
			Enabled:      p.Enabled && len(keys) > 0,
			StreamUsage:  p.StreamUsage,
		}
	}
	for _, m := range models {
		upstream := m.UpstreamModel
		if upstream == "" {
			upstream = m.Name
		}
		cfg.Models = append(cfg.Models, aiModelConfig{
			Name:          m.Name,
			Provider:      strings.ToLower(m.ProviderName),
			UpstreamModel: upstream,
		})
	}
	return cfg
}

var aiEnvNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// aiSeedFromYAML 把 ai.yaml 转成首次入库的 provider / model 记录。
//
// 只存环境变量名（${VAR} → api_key_env），字面量 key（本地假上游）才存 api_key。
func aiSeedFromYAML(cfg AIConfig) ([]model.AIProvider, []model.AIModel) {
	keys := make([]string, 0, len(cfg.Providers))
	for k := range cfg.Providers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	realName := make(map[string]string, len(keys)) // lower → 原始 name
	providers := make([]model.AIProvider, 0, len(keys))
	for _, k := range keys {
		p := cfg.Providers[k]
		sp := model.AIProvider{
			Name:         p.Name,
			Type:         "openai",
			Endpoint:     p.Endpoint,
			UpstreamPath: p.UpstreamPath,
			StreamUsage:  p.StreamUsage,
			Enabled:      p.Enabled,
		}
		for _, raw := range p.RawAPIKeys {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			if envName, ok := envPlaceholder(raw); ok {
				sp.APIKeyEnv = envName
			} else {
				sp.APIKey = raw
			}
			break
		}
		providers = append(providers, sp)
		realName[k] = p.Name
	}

	models := make([]model.AIModel, 0, len(cfg.Models))
	for _, m := range cfg.Models {
		provider, ok := realName[strings.ToLower(m.Provider)]
		if !ok {
			slog.Warn("ai_seed_model_skipped",
				"model", m.Name, "provider", m.Provider, "reason", "provider 不存在")
			continue
		}
		models = append(models, model.AIModel{
			Name:          m.Name,
			ProviderName:  provider,
			UpstreamModel: m.UpstreamModel,
			Enabled:       true,
		})
	}
	return providers, models
}

// envPlaceholder 识别 "${ZHIPU_API_KEY}" 这种写法，返回变量名。
func envPlaceholder(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "${") || !strings.HasSuffix(s, "}") {
		return "", false
	}
	name := s[2 : len(s)-1]
	if !aiEnvNameRe.MatchString(name) {
		return "", false
	}
	return name, true
}

// buildAIHandler 按配置构造 AI 处理器；没有可用 model 时返回 nil。
func buildAIHandler(cfg AIConfig, tpm quota.Limiter, usage *postgres.UsageStore, clients *ai.Clients) *ai.Handler {
	if !cfg.Enabled {
		slog.Info("ai_gateway_off", "hint", "去掉 -ai=false 可启用")
		return nil
	}

	entries := make(map[string]*ai.ProviderEntry, len(cfg.Models))

	for _, m := range cfg.Models {
		p, ok := cfg.Providers[m.Provider]
		if !ok {
			slog.Warn("ai_model_skipped",
				"model", m.Name, "reason", "引用了不存在的 provider", "provider", m.Provider)
			continue
		}
		if !p.Enabled {
			slog.Info("ai_model_skipped",
				"model", m.Name, "reason", "provider 未启用（没配 key）", "provider", p.Name)
			continue
		}
		if m.Name == "" || p.Endpoint == "" {
			slog.Warn("ai_model_skipped",
				"model", m.Name, "reason", "配置不完整", "endpoint", p.Endpoint)
			continue
		}

		// 阶段一：所有厂商都走 OpenAI 兼容适配器。
		// 以后接 Claude 这类协议不同的，这里按 p.Type 选不同的适配器。
		adapter := provider.NewOpenAIProvider(p.Name, p.APIKeys)

		entry := &ai.ProviderEntry{
			Name:          p.Name,
			Provider:      adapter,
			BaseURL:       p.Endpoint,
			UpstreamPath:  p.UpstreamPath,
			UpstreamModel: m.UpstreamModel,
			StreamUsage:   p.StreamUsage,
		}

		// 每个 entry 都要单独探 —— 以后挂不同适配器时能力可能不同。
		entry.Caps = ai.ProbeCapabilities(entry.Provider)

		entries[m.Name] = entry

		slog.Info("ai_model_registered",
			"model", m.Name,
			"provider", p.Name,
			"upstream_model", m.UpstreamModel,
			"endpoint", p.Endpoint,
			"upstream_path", p.UpstreamPath)
	}

	if len(entries) == 0 {
		slog.Warn("ai_gateway_disabled",
			"reason", "没有任何可用的 model（检查 API key 环境变量）")
		return nil
	}

	slog.Info("ai_gateway_enabled", "model_count", len(entries))
	h := ai.NewHandler(entries, clients)
	h.SetTPM(tpm)
	if usage != nil {
		h.SetUsageRecorder(usage)
	}
	return h
}

// AIRouter 按路径分流：命中的 AI 路径走当前 handler，其余交给 fallback。
//
// handler 用 getter 传入，这样配置热更新时无需重建中间件链。
//
//	RequestID → Logging → RequireAuth → RateLimit(租户) → RateLimit(key)
//	  →  分流 → AI / 普通代理
func AIRouter(get func() *ai.Handler, fallback http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if name, ok := ai.ApiNameFromPath(r.Method, r.URL.Path); ok {
			// AI 路径不在路由表里，标签用固定前缀 + API 名（枚举值，基数有界）
			middleware.SetRouteLabel(r, middleware.LabelAI+"/"+string(name))
			if h := get(); h != nil {
				h.ServeHTTP(w, r)
				return
			}
		}
		fallback.ServeHTTP(w, r)
	})
}

// IsAIRoute 报告某个请求是否命中 AI 路径。
func IsAIRoute(r *http.Request) bool {
	_, ok := ai.ApiNameFromPath(r.Method, r.URL.Path)
	return ok
}
