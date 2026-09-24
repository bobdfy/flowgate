package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOpenAIErrorResponder 验证"网关自己拒绝请求"时按链路选 body 格式。
//
// 为什么值得测：这段逻辑以前住在 cmd/gateway（main 包）里，
// 要起整个网关才能验 —— 实际结果就是它一直没被单测覆盖过。
func TestOpenAIErrorResponder(t *testing.T) {
	isAI := func(r *http.Request) bool { return r.URL.Path == "/v1/chat/completions" }

	tests := []struct {
		name        string
		responder   ErrorResponder
		path        string
		status      int
		wantCT      string
		wantJSON    bool
		wantMessage string
	}{
		{
			name:        "AI 路径回 OpenAI 错误结构",
			responder:   OpenAIErrorResponder(isAI),
			path:        "/v1/chat/completions",
			status:      http.StatusUnauthorized,
			wantCT:      "application/json",
			wantJSON:    true,
			wantMessage: "缺少 X-API-Key 请求头",
		},
		{
			name:        "普通路径回纯文本",
			responder:   OpenAIErrorResponder(isAI),
			path:        "/api/v1/services",
			status:      http.StatusUnauthorized,
			wantCT:      "text/plain",
			wantJSON:    false,
			wantMessage: "缺少 X-API-Key 请求头",
		},
		{
			name:        "429 在 AI 路径也是 JSON",
			responder:   OpenAIErrorResponder(isAI),
			path:        "/v1/chat/completions",
			status:      http.StatusTooManyRequests,
			wantCT:      "application/json",
			wantJSON:    true,
			wantMessage: "Too Many Requests",
		},
		{
			name:        "429 在普通路径是纯文本",
			responder:   OpenAIErrorResponder(isAI),
			path:        "/api/v1/services",
			status:      http.StatusTooManyRequests,
			wantCT:      "text/plain",
			wantJSON:    false,
			wantMessage: "Too Many Requests",
		},
		{
			// ★ 传 nil 时不能 panic —— 调用方可能没配 isAI。
			// 语义上等价于"所有请求都走纯文本"。
			name:        "isAI 为 nil 时退化为纯文本",
			responder:   OpenAIErrorResponder(nil),
			path:        "/v1/chat/completions",
			status:      http.StatusUnauthorized,
			wantCT:      "text/plain",
			wantJSON:    false,
			wantMessage: "缺少 X-API-Key 请求头",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)

			tc.responder(rec, req, tc.status, tc.wantMessage)

			res := rec.Result()
			defer res.Body.Close()

			// ★ 状态码必须原样透传 —— responder 只换 body，不改状态码。
			if res.StatusCode != tc.status {
				t.Errorf("状态码 = %d, 期望 %d", res.StatusCode, tc.status)
			}

			ct := res.Header.Get("Content-Type")
			if !strings.Contains(ct, tc.wantCT) {
				t.Errorf("Content-Type = %q, 期望包含 %q", ct, tc.wantCT)
			}

			body := rec.Body.String()

			if !tc.wantJSON {
				// 纯文本：不该长得像 JSON
				if strings.HasPrefix(strings.TrimSpace(body), "{") {
					t.Errorf("期望纯文本，实际是 JSON: %q", body)
				}
				if !strings.Contains(body, tc.wantMessage) {
					t.Errorf("body 里没有 %q: %q", tc.wantMessage, body)
				}
				return
			}

			// ★ JSON 分支：必须能按 OpenAI 的结构解析出来。
			// 只断言"是合法 JSON"不够 —— SDK 要的是 error.message 这个路径。
			var parsed struct {
				Error struct {
					Message string `json:"message"`
					Type    string `json:"type"`
					Code    int    `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(body), &parsed); err != nil {
				t.Fatalf("body 不是合法 JSON: %v\nbody: %s", err, body)
			}
			if parsed.Error.Message != tc.wantMessage {
				t.Errorf("error.message = %q, 期望 %q", parsed.Error.Message, tc.wantMessage)
			}
			if parsed.Error.Code != tc.status {
				t.Errorf("error.code = %d, 期望 %d", parsed.Error.Code, tc.status)
			}
			if parsed.Error.Type == "" {
				t.Error("error.type 为空 —— 它是 OpenAI 结构的必填字段")
			}
		})
	}
}

// TestOpenAIErrorType 钉住状态码到 type 的映射。
//
// 为什么不比较 map 而是逐条断言：map 比较在新增状态码时不会失败，
// 逐条断言能保证"这个状态码到底映射成什么"是被显式确认过的。
func TestOpenAIErrorType(t *testing.T) {
	cases := map[int]string{
		http.StatusUnauthorized:        "authentication_error",
		http.StatusForbidden:           "authentication_error",
		http.StatusTooManyRequests:     "rate_limit_error",
		http.StatusNotFound:            "not_found_error",
		http.StatusInternalServerError: "gateway_error",
	}
	for status, want := range cases {
		if got := openAIErrorType(status); got != want {
			t.Errorf("openAIErrorType(%d) = %q, 期望 %q", status, got, want)
		}
	}
}
