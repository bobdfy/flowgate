// respond.go 放「网关自己拒绝请求」时的响应策略。
//
//	它能同时服务 AI 和普通两条链路，靠的是调用方注入的 isAI 谓词 ——
//	和 RateLimit(limiter, keyFn, next) 把"按什么 key 限流"当参数传进来
//	是同一个模式：机制在库里，策略由调用方给。
package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// OpenAIErrorResponder 返回一个 ErrorResponder：AI 路径回 OpenAI 兼容的错误结构，其余路径回纯文本。
// 普通代理的调用方看 HTTP 状态码就够了；但 AI 的调用方是 OpenAI SDK ——它按 {"error":{"message":...}} 解析响应，拿到纯文本会抛出一个看不出原因的解析错误（用户看到的是 SDK 自己的报错，而不是真正的原因）。
func OpenAIErrorResponder(isAI func(*http.Request) bool) ErrorResponder {
	return func(w http.ResponseWriter, r *http.Request, status int, msg string) {
		if isAI != nil && isAI(r) {
			writeOpenAIError(w, status, msg)
			return
		}
		http.Error(w, msg, status)
	}
}

// writeOpenAIError 按 OpenAI 的错误结构回一个 JSON body。
func writeOpenAIError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// 和 http.Error 保持一致：这个 body 不该被任何中间层嗅探成别的类型。
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)

	body := map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    openAIErrorType(status),
			"code":    status,
		},
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// 响应已经开始写了，改不了状态码，只能记一笔。
		slog.Warn("write_openai_error_failed", "err", err)
	}
}

// openAIErrorType 把 HTTP 状态码映射成 OpenAI 风格的错误类型字符串。
//
// 这个字段 SDK 只用来分类展示，填错不影响解析，但不能不填 —— 它是必填。
func openAIErrorType(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "authentication_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusNotFound:
		return "not_found_error"
	default:
		return "gateway_error"
	}
}
