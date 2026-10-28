package gatewayapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bobdfy/flowgate/internal/model"
)

// max_concurrency 要穿过三条路径才能到 handler：
//   ai.yaml → AIConfig → DB（种子）
//   DB → AIConfig（热加载）
// 任何一条漏掉，配置都会静默变成 0（不限），表现为「设了上限但完全没生效」。
// 这种错不会报错、不会打日志，只能靠测试发现。

func TestLoadAIConfigParsesMaxConcurrency(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ai.yaml")
	yaml := `
providers:
  - name: mockai
    type: openai
    endpoint: http://localhost:8095
    api_keys: ["sk-mock"]
    enabled: true
models:
  - name: flowgate-mock
    provider: mockai
    upstream_model: mock-gpt
    max_concurrency: 4
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadAIConfig(path)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(cfg.Models) != 1 {
		t.Fatalf("models = %d", len(cfg.Models))
	}
	if got := cfg.Models[0].MaxConcurrency; got != 4 {
		t.Fatalf("max_concurrency 应为 4，实际 %d", got)
	}

	// 种子必须把它带进 DB，否则首启之后 DB 里永远是 0。
	_, models := aiSeedFromYAML(*cfg)
	if len(models) != 1 {
		t.Fatalf("种子 models = %d", len(models))
	}
	if got := models[0].MaxConcurrency; got != 4 {
		t.Fatalf("种子里 max_concurrency 应为 4，实际 %d", got)
	}
}

// 负数必须在加载阶段就失败：放行的话护栏会被当成「不限」，
// 或者更糟 —— 在某个分支里算出一个负数容量，行为完全不可预期。
func TestLoadAIConfigRejectsNegativeMaxConcurrency(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ai.yaml")
	yaml := `
providers:
  - name: mockai
    type: openai
    endpoint: http://localhost:8095
    api_keys: ["sk-mock"]
models:
  - name: flowgate-mock
    provider: mockai
    max_concurrency: -1
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadAIConfig(path)
	if err == nil {
		t.Fatal("max_concurrency 为负应当报错")
	}
	if !strings.Contains(err.Error(), "max_concurrency") {
		t.Fatalf("报错信息应点明是哪个字段，实际: %v", err)
	}
}

// DB 是配置的事实来源，热加载路径必须带上这一列。
func TestAIConfigFromDBKeepsMaxConcurrency(t *testing.T) {
	providers := []model.AIProvider{
		{Name: "p1", Endpoint: "http://a", APIKey: "sk-local", Enabled: true},
	}
	models := []model.AIModel{
		{Name: "limited", ProviderName: "p1", MaxConcurrency: 32, Enabled: true},
		{Name: "unlimited", ProviderName: "p1", MaxConcurrency: 0, Enabled: true},
	}

	cfg := aiConfigFromDB(providers, models)

	got := map[string]int{}
	for _, m := range cfg.Models {
		got[m.Name] = m.MaxConcurrency
	}
	if got["limited"] != 32 {
		t.Fatalf("limited 的 max_concurrency 应为 32，实际 %d", got["limited"])
	}
	if got["unlimited"] != 0 {
		t.Fatalf("unlimited 应为 0，实际 %d", got["unlimited"])
	}
}
