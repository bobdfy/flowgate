// AI 假上游, 监听 :8095
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
)

func main() {
	addr := flag.String("addr", ":8095", "监听地址")
	flag.Parse()

	// 创建一个自定义的日志记录器
	logger := log.New(os.Stdout, "mockai", log.LstdFlags|log.Lmicroseconds)

	modes := newModeStore()

	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/chat/completions", chatHandler(modes, logger))
	mux.HandleFunc("GET /v1/models", modelsHandler())

	// 故障注入：切换上游行为模式
	mux.HandleFunc("POST /__mode", modeHandler(modes))
	mux.HandleFunc("GET /__mode", modeGetHandler(modes))

	logger.Printf("listening on %s (mode=%s)", *addr, modes.get())
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("mockai 启动失败: %v", err)
	}
}

// chatReq 是客户端请求体里我们需要"看懂"的字段。
//
// 只声明这两个 —— 其余字段（temperature / tools / stream_options……）
// 我们不解析，但请求体原文要留着回显给排障看。
type chatReq struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

// chatHandler 处理 POST /v1/chat/completions。
func chatHandler(modes *modeStore, logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. 读请求体
		body, err := readBody(r)
		if err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "读取请求体失败: "+err.Error())
			return
		}

		var req chatReq
		if err := json.Unmarshal(body, &req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "请求体不是合法 JSON: "+err.Error())
			return
		}

		mode := modes.get()

		// 2. ★ 把收到的 Authorization 打出来。
		//
		// 这是验证"网关有没有正确换 key"的唯一手段 ——
		// 没有这条日志，你无法区分"网关发了对的 key"和"网关根本没发 key"。
		auth := r.Header.Get("Authorization")
		if auth == "" {
			auth = "(空)"
		}
		logger.Printf("mode=%s model=%q stream=%v auth=%q content_type=%q body_len=%d",
			mode, req.Model, req.Stream, auth, r.Header.Get("Content-Type"), len(body))

		// 3. 按模式分支
		switch mode {
		case modeError:
			// 固定 500 + OpenAI 错误体。
			// 用来验证网关"原样透传上游错误"，而不是伪装成 200 或改成 502。
			writeOpenAIError(w, http.StatusInternalServerError, "server_error", "mockai 注入的 500")
			return

		case modeRateLimit:
			// 固定 429 + Retry-After。
			// 用来验证网关透传上游限流（以后做 key failover 时也用它）。
			w.Header().Set("Retry-After", "3")
			writeOpenAIError(w, http.StatusTooManyRequests, "rate_limit_error", "mockai 注入的 429")
			return
		}

		// 以下都是正常路径。stream 字段决定走流式还是非流式。
		if req.Stream {
			handleStream(w, r, modes, logger, req.Model, body)
			return
		}
		handleBuffered(w, req.Model, body)
	}
}

// handleBuffered 返回一个非流式的 OpenAI Chat Completions 响应。
func handleBuffered(w http.ResponseWriter, model string, reqBody []byte) {
	prompt := promptOf(reqBody)
	answer := mockAnswer

	payload := chatResponse{
		ID:      "chatcmpl-mockai-1",
		Object:  "chat.completion",
		Created: nowUnix(),
		Model:   model,
		Choices: []choice{{
			Index:        0,
			Message:      message{Role: "assistant", Content: answer},
			FinishReason: "stop",
		}},
		Usage: usage{
			PromptTokens:     estimateTokens(prompt),
			CompletionTokens: estimateTokens(answer),
			TotalTokens:      estimateTokens(prompt) + estimateTokens(answer),
		},
	}

	writeJSON(w, http.StatusOK, payload)
}

// modelsHandler 处理 GET /v1/models。
//
// 返回的是"上游认为自己的模型名" —— 故意和网关对外的模型名不同，
// 这样能验证"网关有没有把上游的模型名泄露给客户端"。
func modelsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "mock-gpt", "object": "model", "owned_by": "mockai"},
				{"id": "mock-gpt-mini", "object": "model", "owned_by": "mockai"},
			},
		})
	}
}

// OpenAI 响应结构（只声明我们用到的字段）
type chatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

type choice struct {
	Index        int     `json:"index"`
	Message      message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
