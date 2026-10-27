package gatewayapp

import (
	"testing"

	"github.com/bobdfy/flowgate/internal/model"
)

func TestAISeedFromYAML(t *testing.T) {
	cfg := AIConfig{
		Enabled: true,
		Providers: map[string]aiProviderConfig{
			"zhipu": {
				Name:         "zhipu",
				Endpoint:     "https://open.bigmodel.cn/api/paas/v4",
				UpstreamPath: "/chat/completions",
				RawAPIKeys:   []string{"${ZHIPU_API_KEY}"},
				Enabled:      true,
				StreamUsage:  true,
			},
			"mockai": {
				Name:        "mockai",
				Endpoint:    "http://localhost:8095",
				RawAPIKeys:  []string{"sk-mock"},
				Enabled:     true,
				StreamUsage: true,
			},
		},
		Models: []aiModelConfig{
			{Name: "flowgate-chat", Provider: "zhipu", UpstreamModel: "glm-5.3"},
			{Name: "flowgate-mock", Provider: "mockai", UpstreamModel: "mock-gpt"},
			{Name: "orphan", Provider: "nope", UpstreamModel: "x"},
		},
	}

	providers, models := aiSeedFromYAML(cfg)
	if len(providers) != 2 {
		t.Fatalf("providers = %d, want 2", len(providers))
	}
	if len(models) != 2 {
		t.Fatalf("models = %d, want 2 (孤儿模型应被跳过)", len(models))
	}

	byName := map[string]model.AIProvider{}
	for _, p := range providers {
		byName[p.Name] = p
	}
	if got := byName["zhipu"].APIKeyEnv; got != "ZHIPU_API_KEY" {
		t.Fatalf("zhipu api_key_env = %q", got)
	}
	if got := byName["zhipu"].APIKey; got != "" {
		t.Fatalf("zhipu 不应存明文 key, got %q", got)
	}
	if got := byName["mockai"].APIKey; got != "sk-mock" {
		t.Fatalf("mockai api_key = %q", got)
	}
	if got := byName["mockai"].APIKeyEnv; got != "" {
		t.Fatalf("mockai api_key_env = %q", got)
	}

	for _, m := range models {
		if m.Name == "orphan" {
			t.Fatal("引用不存在 provider 的模型应被跳过")
		}
	}
}

func TestAIConfigFromDB(t *testing.T) {
	t.Setenv("E2E_AI_KEY", "sk-env")

	providers := []model.AIProvider{
		{Name: "withEnv", Endpoint: "http://a", APIKeyEnv: "E2E_AI_KEY", Enabled: true},
		{Name: "withLiteral", Endpoint: "http://b", APIKey: "sk-local", Enabled: true},
		{Name: "noKey", Endpoint: "http://c", Enabled: true},
	}
	models := []model.AIModel{
		{Name: "m1", ProviderName: "withEnv", UpstreamModel: ""},
		{Name: "m2", ProviderName: "withLiteral", UpstreamModel: "real"},
	}

	cfg := aiConfigFromDB(providers, models)
	if !cfg.Enabled {
		t.Fatal("DB 配置应启用")
	}
	if got := cfg.Providers["withenv"].APIKeys; len(got) != 1 || got[0] != "sk-env" {
		t.Fatalf("withEnv keys = %v", got)
	}
	if got := cfg.Providers["withliteral"].APIKeys; len(got) != 1 || got[0] != "sk-local" {
		t.Fatalf("withLiteral keys = %v", got)
	}
	if cfg.Providers["nokey"].Enabled {
		t.Fatal("没有 key 的 provider 不应启用")
	}
	if len(cfg.Models) != 2 {
		t.Fatalf("models = %d", len(cfg.Models))
	}
	if cfg.Models[0].UpstreamModel != "m1" {
		t.Fatalf("空 upstream_model 应回落为别名, got %q", cfg.Models[0].UpstreamModel)
	}
}

func TestEnvPlaceholder(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"${ZHIPU_API_KEY}", "ZHIPU_API_KEY", true},
		{" ${A1} ", "A1", true},
		{"${1BAD}", "", false},
		{"$ZHIPU_API_KEY", "", false},
		{"sk-literal", "", false},
	}
	for _, c := range cases {
		got, ok := envPlaceholder(c.in)
		if got != c.want || ok != c.ok {
			t.Fatalf("envPlaceholder(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
