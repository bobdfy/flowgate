package middleware

import (
	"net/http"
	"testing"
)

func TestAPIKeyFromRequest(t *testing.T) {
	cases := []struct {
		name   string
		header map[string]string
		want   string
	}{
		{"X-API-Key", map[string]string{"X-API-Key": "fg-1"}, "fg-1"},
		{"Bearer", map[string]string{"Authorization": "Bearer fg-2"}, "fg-2"},
		{"小写 bearer", map[string]string{"Authorization": "bearer fg-3"}, "fg-3"},
		{"两端空白", map[string]string{"Authorization": "Bearer   fg-4  "}, "fg-4"},
		{"X-API-Key 优先", map[string]string{
			"X-API-Key":     "fg-5",
			"Authorization": "Bearer fg-6",
		}, "fg-5"},
		{"只有 Bearer 前缀", map[string]string{"Authorization": "Bearer "}, ""},
		{"其它认证方式", map[string]string{"Authorization": "Basic Zm9v"}, ""},
		{"都没有", map[string]string{}, ""},
		{"X-API-Key 全空白", map[string]string{"X-API-Key": "   "}, ""},
	}

	for _, c := range cases {
		r, _ := http.NewRequest(http.MethodGet, "/v1/models", nil)
		for k, v := range c.header {
			r.Header.Set(k, v)
		}
		if got := apiKeyFromRequest(r); got != c.want {
			t.Fatalf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
