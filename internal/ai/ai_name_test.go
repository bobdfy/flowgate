package ai

import "testing"

func TestApiNameFromPath(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   ApiName
		ok     bool
	}{
		{"POST", "/v1/chat/completions", ChatCompletions, true},
		{"GET", "/v1/models", Models, true},

		// 结尾斜杠要容忍
		{"POST", "/v1/chat/completions/", ChatCompletions, true},
		{"GET", "/v1/models/", Models, true},

		// 以下都必须失败：方法不匹配 / 路径不认识 / 只认半边
		{"GET", "/v1/chat/completions", "", false},
		{"POST", "/v1/models", "", false},
		{"POST", "/v1/chat", "", false},
		{"POST", "/v1/chat/completion", "", false},
		{"POST", "/api/v1/chat/completions", "", false},
		{"GET", "/", "", false},
	}
	for _, c := range cases {
		got, ok := ApiNameFromPath(c.method, c.path)
		if ok != c.ok || got != c.want {
			t.Errorf("ApiNameFromPath(%q, %q) = (%q, %v), 期望 (%q, %v)",
				c.method, c.path, got, ok, c.want, c.ok)
		}
	}
}

// TestApiNamePath 钉死「规范名 ≠ HTTP 路径」这条不变量。
//
// ★ 这是最容易被写错、且写错不报错的地方：
//
//	aiPath 表如果误存成规范名自己（"openai/v1/chatcompletions"），
//	Path() 就会返回它，而 Path() 被用来拼上游地址 ——
//	拼出畸形 URL 只会在运行时表现为"连不上上游"，极难定位。
func TestApiNamePath(t *testing.T) {
	cases := []struct {
		name ApiName
		path string
	}{
		{ChatCompletions, "/v1/chat/completions"},
		{Models, "/v1/models"},
	}
	for _, c := range cases {
		if got := c.name.Path(); got != c.path {
			t.Errorf("%q.Path() = %q, 期望 %q", c.name, got, c.path)
		}
	}
}

// TestApiNameVendor 钉死 Vendor() 的切片下标。
// ★ 写成 string(a)[:1] 只会返回第一个字节，"openai" 会变成 "o"。
func TestApiNameVendor(t *testing.T) {
	if got := ChatCompletions.Vendor(); got != "openai" {
		t.Errorf("ChatCompletions.Vendor() = %q, 期望 \"openai\"", got)
	}
	if got := Models.Vendor(); got != "openai" {
		t.Errorf("Models.Vendor() = %q, 期望 \"openai\"", got)
	}
}

// TestRegisteredRoutesRoundTrip 保证路由表和 Path() 不会漂移：
// 注册出去的每个路由，自己回来时都必须能映射回同一个规范名，
// 且规范名的 Path() 必须等于注册的路径。
func TestRegisteredRoutesRoundTrip(t *testing.T) {
	routes := RegisteredRoutes()
	if len(routes) != 2 {
		t.Fatalf("期望 2 条路由，实际 %d 条", len(routes))
	}

	for _, r := range routes {
		if !r.Name.Valid() {
			t.Errorf("路由 %s %s 的规范名 %q 无效", r.Method, r.Path, r.Name)
		}
		if got := r.Name.Path(); got != r.Path {
			t.Errorf("规范名 %q 的 Path() = %q，但注册的路径是 %q",
				r.Name, got, r.Path)
		}
		got, ok := ApiNameFromPath(r.Method, r.Path)
		if !ok || got != r.Name {
			t.Errorf("路由 %s %s 往返失败：得到 (%q, %v)", r.Method, r.Path, got, ok)
		}
	}
}
