// 网关对 AI Gateway 的装配代码。
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/bobdfy/flowgate/internal/ai"
	"github.com/bobdfy/flowgate/internal/provider"
	"go.yaml.in/yaml/v2"
)

// aiProviderConfig 是一个上游厂商的配置。
type aiProviderConfig struct {
	Name         string   // 厂商标识，日志和 /v1/models 的 owned_by 用
	Endpoint     string   // 根地址，不带路径
	UpstreamPath string   // 上游真实路径；空 = 用对外的 /v1/...
	APIKeys      []string // key 池（阶段一只用第一个非空的）
	Enabled      bool
}

// aiModelConfig 是"对外模型名 → 某厂商的某模型"的映射。
type aiModelConfig struct {
	Name          string // 客户端请求体里写的模型名
	Provider      string // 引用 aiProviderConfig.Name
	UpstreamModel string // 发给上游的真实模型名
}

// aiConfig 是 AI Gateway 的完整配置。
type aiConfig struct {
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
	} `yaml:"providers"`

	Models []struct {
		Name          string `yaml:"name"`
		Provider      string `yaml:"provider"`
		UpstreamModel string `yaml:"upstream_model"`
	} `yaml:"models"`
}

// loadAIConfig 读 ai.yaml 并转成运行时配置。
// 文件存在但格式错 → 返回 error，调用方记日志后降级
func loadAIConfig(path string) (*aiConfig, error) {
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

	cfg := &aiConfig{
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
			Enabled:      enabled,
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

// buildAIRouter 装配 AI 支路，返回一个按路径分流的 handler。
func buildAIRouter(cfg aiConfig, fallback http.Handler) http.Handler {
	return aiRouter(buildAIHandler(cfg), fallback)
}

// buildAIHandler 按配置构造 AI 处理器；没有可用 model 时返回 nil。
func buildAIHandler(cfg aiConfig) *ai.Handler {
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
	return ai.NewHandler(entries)
}

// aiRouter 按路径分流：命中的 AI 路径走 aiHandler，其余交给 fallback。
//
//	RequestID → Logging → RequireAuth → RateLimit(租户) → RateLimit(key)
//	  →  分流 → AI / 普通代理
func aiRouter(h *ai.Handler, fallback http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h != nil {
			if _, ok := ai.ApiNameFromPath(r.Method, r.URL.Path); ok {
				h.ServeHTTP(w, r)
				return
			}
		}
		fallback.ServeHTTP(w, r)
	})
}

// isAIRoute 报告某个请求是否命中 AI 路径。
func isAIRoute(r *http.Request) bool {
	_, ok := ai.ApiNameFromPath(r.Method, r.URL.Path)
	return ok
}
