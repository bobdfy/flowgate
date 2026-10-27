package ai

import (
	"encoding/json"
	"testing"
)

func TestSSEPayload(t *testing.T) {
	cases := []struct {
		name  string
		event string
		want  string
	}{
		{"单行", `data: {"a":1}`, `{"a":1}`},
		{"CRLF", "data: {\"a\":1}\r\n", `{"a":1}`},
		{"多条 data 行", "data: {\"a\":\ndata: 1}", "{\"a\":\n1}"},
		{"DONE", "data: [DONE]", ""},
		{"注释行", ": ping", ""},
	}
	for _, c := range cases {
		if got := string(ssePayload([]byte(c.event))); got != c.want {
			t.Fatalf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestStreamUsageObserve(t *testing.T) {
	body := []byte(`{"model":"x","messages":[{"role":"user","content":"hi"}]}`)
	u := newStreamUsage(body, false)
	if u.promptTokens != 4 { // "hi" 2 字节 → 0，再加每消息 4 个格式 token
		t.Fatalf("promptTokens=%d want 4", u.promptTokens)
	}

	u.observe([]byte(`data: {"choices":[{"delta":{"content":"hello"}}]}`))
	u.observe([]byte(`data: {"choices":[{"delta":{"content":"world"}}]}`))
	if u.completionBytes != 10 {
		t.Fatalf("completionBytes=%d want 10", u.completionBytes)
	}
	if u.seen {
		t.Fatal("没有 usage 事件，seen 不应为 true")
	}
	if got, want := u.estimated(), int64(4+10/4); got != want {
		t.Fatalf("estimated=%d want %d", got, want)
	}
	if got := u.actual(); got != u.estimated() {
		t.Fatalf("actual=%d want estimated=%d", got, u.estimated())
	}

	u.observe([]byte(`data: {"choices":[],"usage":{"total_tokens":42}}`))
	if !u.seen || u.total != 42 {
		t.Fatalf("seen=%v total=%d", u.seen, u.total)
	}
	if got := u.actual(); got != 42 {
		t.Fatalf("actual=%d want 42", got)
	}
	if u.estimated() == 42 {
		t.Fatal("估算值应保持按内容计算，供偏差对比")
	}
}

func TestStreamUsageStrip(t *testing.T) {
	body := []byte(`{"model":"x","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	u := newStreamUsage(body, true)
	sink := u.sink()

	usageEvent := []byte(`data: {"choices":[],"usage":{"total_tokens":9}}`)
	if got := sink(usageEvent, false); got != nil {
		t.Fatalf("网关自己索要的 usage 帧应被吞掉，得到 %q", got)
	}
	if !u.seen || u.total != 9 {
		t.Fatalf("吞掉事件也要记账: seen=%v total=%d", u.seen, u.total)
	}

	contentEvent := []byte(`data: {"choices":[{"delta":{"content":"hi"}}]}`)
	if got := sink(contentEvent, false); got == nil {
		t.Fatal("普通内容帧不能被吞")
	}
}

func TestEnsureStreamUsage(t *testing.T) {
	got, err := ensureStreamUsage([]byte(`{"model":"x","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !wantsStreamUsage(got) {
		t.Fatalf("注入后应带 include_usage: %s", got)
	}

	// 保留原有 stream_options 字段
	got, err = ensureStreamUsage([]byte(`{"model":"x","stream_options":{"foo":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		StreamOptions struct {
			Foo          int  `json:"foo"`
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
	}
	if err := json.Unmarshal(got, &p); err != nil {
		t.Fatal(err)
	}
	if p.StreamOptions.Foo != 1 || !p.StreamOptions.IncludeUsage {
		t.Fatalf("原有字段被丢了: %s", got)
	}

	// 非法 body 返回错误
	if _, err := ensureStreamUsage([]byte(`not json`)); err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
}

func TestEstimatePromptTokens(t *testing.T) {
	// 多模态：只累计 text 部分
	body := []byte(`{"messages":[{"content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"x"}}]}]}`)
	if got, want := estimatePromptTokens(body), int64(5/4+4); got != want {
		t.Fatalf("multimodal got %d want %d", got, want)
	}

	// 没有 messages → 退回整包长度
	body2 := []byte(`{"model":"x"}`)
	if got, want := estimatePromptTokens(body2), int64(len(body2))/4; got != want {
		t.Fatalf("fallback got %d want %d", got, want)
	}
}
