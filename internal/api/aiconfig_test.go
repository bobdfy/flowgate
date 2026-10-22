package api

import (
	"strings"
	"testing"

	"github.com/bobdfy/flowgate/internal/model"
)

func TestApplyAIProviderRequest(t *testing.T) {
	cases := []struct {
		name    string
		req     aiProviderRequest
		wantErr string
		check   func(t *testing.T, p model.AIProvider)
	}{
		{
			name: "最小合法配置补默认值",
			req:  aiProviderRequest{Name: "zhipu", Endpoint: "https://open.bigmodel.cn/api/paas/v4"},
			check: func(t *testing.T, p model.AIProvider) {
				if p.Type != "openai" {
					t.Fatalf("type = %q, want openai", p.Type)
				}
				if !p.Enabled {
					t.Fatal("enabled 默认应为 true")
				}
			},
		},
		{
			name: "endpoint 去掉尾部斜杠",
			req:  aiProviderRequest{Name: "p", Endpoint: "http://localhost:8095/"},
			check: func(t *testing.T, p model.AIProvider) {
				if p.Endpoint != "http://localhost:8095" {
					t.Fatalf("endpoint = %q", p.Endpoint)
				}
			},
		},
		{
			name: "显式停用",
			req:  aiProviderRequest{Name: "p", Endpoint: "http://localhost:8095", Enabled: boolPtr(false)},
			check: func(t *testing.T, p model.AIProvider) {
				if p.Enabled {
					t.Fatal("enabled 应为 false")
				}
			},
		},
		{name: "缺少 name", req: aiProviderRequest{Endpoint: "http://x"}, wantErr: "name is required"},
		{name: "name 含空格", req: aiProviderRequest{Name: "bad name", Endpoint: "http://x"}, wantErr: "name may only contain"},
		{name: "type 不支持", req: aiProviderRequest{Name: "p", Type: "claude", Endpoint: "http://x"}, wantErr: "type must be openai"},
		{name: "缺少 endpoint", req: aiProviderRequest{Name: "p"}, wantErr: "endpoint is required"},
		{name: "endpoint 非 http", req: aiProviderRequest{Name: "p", Endpoint: "ftp://x"}, wantErr: "full http/https URL"},
		{name: "upstream_path 不以 / 开头", req: aiProviderRequest{Name: "p", Endpoint: "http://x", UpstreamPath: "chat"}, wantErr: "upstream_path must start with /"},
		{name: "非法环境变量名", req: aiProviderRequest{Name: "p", Endpoint: "http://x", APIKeyEnv: "1KEY"}, wantErr: "valid environment variable name"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var p model.AIProvider
			err := applyAIProviderRequest(&p, c.req)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want contains %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if c.check != nil {
				c.check(t, p)
			}
		})
	}
}

func TestApplyAIModelRequest(t *testing.T) {
	cases := []struct {
		name    string
		req     aiModelRequest
		wantErr string
		check   func(t *testing.T, m model.AIModel)
	}{
		{
			name: "合法配置",
			req:  aiModelRequest{Name: "flowgate-chat", ProviderName: "zhipu", UpstreamModel: "glm-5.3"},
			check: func(t *testing.T, m model.AIModel) {
				if !m.Enabled {
					t.Fatal("enabled 默认应为 true")
				}
			},
		},
		{
			name: "允许点号别名",
			req:  aiModelRequest{Name: "gpt-4o-mini", ProviderName: "p"},
			check: func(t *testing.T, m model.AIModel) {
				if m.Name != "gpt-4o-mini" {
					t.Fatalf("name = %q", m.Name)
				}
			},
		},
		{name: "缺少 name", req: aiModelRequest{ProviderName: "p"}, wantErr: "name is required"},
		{name: "别名含斜杠", req: aiModelRequest{Name: "a/b", ProviderName: "p"}, wantErr: "name may only contain"},
		{name: "缺少 provider", req: aiModelRequest{Name: "m"}, wantErr: "provider_name is required"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var m model.AIModel
			err := applyAIModelRequest(&m, c.req)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want contains %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if c.check != nil {
				c.check(t, m)
			}
		})
	}
}

func boolPtr(v bool) *bool { return &v }
