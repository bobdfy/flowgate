package ai

import (
	"fmt"
	"strings"
)

// ApiName 是内部规范名，格式为 {vendor}/{version}/{apitype}。
// 例：openai/v1/chatcompletions。
// 对外暴露的是 OpenAI 兼容路径，但对内必须能表达
type ApiName string

// 对外暴露的 OpenAI 接口规范名。
// 注意：常量值就是规范名本身（{vendor}/{version}/{apitype}），
const (
	// ChatCompletions 对应 POST /v1/chat/completions，对话补全。
	ChatCompletions ApiName = "openai/v1/chatcompletions"

	// Models 对应 GET /v1/models，模型列表查询。
	Models ApiName = "openai/v1/models"
)

// aiPath 是 规范名 → 对外 HTTP 路径。
// 这里存的是「路径」（以 / 开头），不是规范名。
var aiPath = map[ApiName]string{
	ChatCompletions: "/v1/chat/completions",
	Models:          "/v1/models",
}

// aiRoutes 是 "method + path" → 规范名 的映射表（唯一事实来源）。
var aiRoutes = map[string]ApiName{
	"POST /v1/chat/completions": ChatCompletions,
	"GET /v1/models":            Models,
}

// Path 返回该规范名对外暴露的 HTTP 路径。
//
// 转发到上游时也用这个路径 —— 它是"路径拼装"的唯一来源。
// 手拼字符串（BaseURL + "/chat/completions"）是这类代码最常见的 bug，
func (a ApiName) Path() string {
	return aiPath[a]
}

// Vendor 返回规范名里的厂商段（{vendor}）。
func (a ApiName) Vendor() string {
	if i := strings.IndexByte(string(a), '/'); i > 0 {
		return string(a)[:i]
	}
	return string(a)
}

// Valid 判断该规范名是否是 FlowGate 已实现的接口。
func (a ApiName) Valid() bool {
	_, ok := aiPath[a]
	return ok
}

// ApiNameFromPath 把 HTTP 方法 + 路径映射成内部规范名。
// 未注册的路径返回 ("", false)。
//
// 调用方拿到 false 必须回 404，绝不能 fallthrough 到普通 API 路由 ——
// 否则 AI 路径会被当成业务路径代理出去，那是个安全洞。
func ApiNameFromPath(method, path string) (ApiName, bool) {
	// 容忍结尾斜杠：/v1/models/ 与 /v1/models 等价。
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}
	name, ok := aiRoutes[method+" "+path]
	return name, ok
}

// AIRoute 描述一个已注册的 AI 接口。
type AIRoute struct {
	Method string
	Path   string
	Name   ApiName
}

// RegisteredRoutes 返回所有已注册的 AI 接口，供 cmd/gateway 统一挂路由。
//
// 为什么要有它：让"路由注册"和"路径映射表"共用同一份事实来源。
func RegisteredRoutes() []AIRoute {
	// 防御：aiPath 里的每个接口都必须能在 aiRoutes 里找到对应的 method+path，
	// 否则说明加接口时漏了其中一张表。
	if len(aiPath) != len(aiRoutes) {
		panic(fmt.Sprintf("ai_name 表不一致: aiPath=%d 条, aiRoutes=%d 条",
			len(aiPath), len(aiRoutes)))
	}
	// 按固定顺序输出，保证结果稳定可测。
	// 不要直接 range aiRoutes —— Go 的 map 遍历顺序随机，
	order := []ApiName{ChatCompletions, Models}
	out := make([]AIRoute, 0, len(aiRoutes))
	for _, name := range order {
		for key, v := range aiRoutes {
			if v != name {
				continue
			}
			method, path, _ := strings.Cut(key, " ")
			out = append(out, AIRoute{Method: method, Path: path, Name: name})
		}
	}
	return out
}
