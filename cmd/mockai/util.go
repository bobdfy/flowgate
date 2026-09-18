package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// mockAnswer 是假上游固定的回答内容。
const mockAnswer = "你好，我是 mockai 假上游。"

// maxBody 是读请求体的上限。和网关那边的 16 MiB 对齐即可。
const maxBody = 16 << 20

func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(io.LimitReader(r.Body, maxBody))
}

// promptOf 从请求体里把用户说的话拼起来，用于估算 prompt_tokens
func promptOf(body []byte) string {
	var probe struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return ""
	}
	var sbu strings.Builder
	for _, m := range probe.Messages {
		sbu.WriteString(m.Content)
	}
	return sbu.String()
}

// estimateTokens 按" 字符串 / 4 " 估算Token
func estimateTokens(s string) int {
	n := len(s) / 4
	if n < 1 {
		n = 1
	}
	return n
}

// writeJSON 把 v 序列化成 JSON 写入响应。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeOpenAIError 返回 OpenAI 风格的错误体。
//
// 结构和 internal/ai 的 writeError 一致，方便肉眼对比两边格式。
func writeOpenAIError(w http.ResponseWriter, status int, errType, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"message": msg,
			"type":    errType,
		},
	})
}

// 文件末尾加这个函数
// nowUnix 返回当前 Unix 时间戳（OpenAI 响应的 created 字段）。
func nowUnix() int64 {
	return time.Now().Unix()
}
