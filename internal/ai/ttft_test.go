package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/observability"
	dto "github.com/prometheus/client_model/go"
)

// TTFT（首 token 耗时）这组测试的重点是「口径」：
// 起点必须是网关开始处理请求的时刻，而不是响应头已经回来之后的某一刻。
// 口径错了指标照样有数，只是偏乐观 —— 这是最难靠肉眼发现的一类错。

func histogramSum(t *testing.T, model string) float64 {
	t.Helper()
	h, ok := observability.AIStreamTTFT.WithLabelValues(model).(interface {
		Write(*dto.Metric) error
	})
	if !ok {
		t.Fatal("AIStreamTTFT 不是可写出的 Histogram")
	}
	m := &dto.Metric{}
	if err := h.Write(m); err != nil {
		t.Fatalf("读取直方图失败: %v", err)
	}
	return m.GetHistogram().GetSampleSum()
}

// ttftStart 读不到值时必须返回零值，而不是 time.Now()。
// 返回 now 会让「没设置起点」的调用方凭空得到一个 0 延迟，掩盖问题。
func TestTTFTStartFromContext(t *testing.T) {
	if got := ttftStart(context.Background()); !got.IsZero() {
		t.Fatalf("没有起点时应返回零值，实际 %v", got)
	}

	want := time.Unix(1000, 0)
	ctx := context.WithValue(context.Background(), ttftStartKey{}, want)
	if got := ttftStart(ctx); !got.Equal(want) {
		t.Fatalf("应读到写入的起点 %v，实际 %v", want, got)
	}
}

// 首帧时刻只能记一次。若每帧都覆盖，TTFT 会退化成「最后一帧 - 起点」，
// 长流会显示成一个巨大的首字延迟。
func TestStreamUsageRecordsOnlyFirstEvent(t *testing.T) {
	u := newStreamUsage([]byte(`{}`), false)
	sink := u.sink()

	sink([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n"), false)
	first := u.firstEventAt
	if first.IsZero() {
		t.Fatal("首帧之后应当记录时刻")
	}

	time.Sleep(20 * time.Millisecond)
	sink([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n"), false)

	if !u.firstEventAt.Equal(first) {
		t.Fatalf("首帧时刻被后续帧覆盖了：%v → %v", first, u.firstEventAt)
	}
}

// 首帧（role-only）、usage 帧、[DONE] 都不算首 token。
// 只有 delta.content / delta.reasoning_content 出现时，用户才算真的看到了东西。
func TestEventHasContent(t *testing.T) {
	cases := []struct {
		name  string
		event string
		want  bool
	}{
		{"正文", `data: {"choices":[{"delta":{"content":"a"}}]}`, true},
		{"推理正文", `data: {"choices":[{"delta":{"reasoning_content":"想"}}]}`, true},
		{"角色帧", `data: {"choices":[{"delta":{"role":"assistant"}}]}`, false},
		{"空 delta", `data: {"choices":[{"delta":{}}]}`, false},
		{"空 choices", `data: {"choices":[]}`, false},
		{"纯 usage", `data: {"choices":[],"usage":{"total_tokens":9}}`, false},
		{"[DONE]", `data: [DONE]`, false},
		{"非 JSON", `data: not-json`, false},
		{"空事件", "", false},
	}
	for _, c := range cases {
		if got := eventHasContent([]byte(c.event)); got != c.want {
			t.Errorf("%s: eventHasContent=%v want %v", c.name, got, c.want)
		}
	}
}

// 上游先发 role 帧、隔一段时间才发正文：TTFT 应该只由正文帧决定。
func TestSinkStartsTTFTOnlyOnContent(t *testing.T) {
	u := newStreamUsage([]byte(`{}`), false)
	sink := u.sink()

	sink([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"), false)
	if !u.firstEventAt.IsZero() {
		t.Fatalf("role 帧不该停表，实际记成了 %v", u.firstEventAt)
	}

	time.Sleep(30 * time.Millisecond)
	start := time.Now()
	sink([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n"), false)
	if u.firstEventAt.IsZero() {
		t.Fatal("正文帧到达后应当停表")
	}
	if u.firstEventAt.Before(start) {
		t.Fatalf("停表时刻 %v 早于正文帧到达时刻 %v", u.firstEventAt, start)
	}
}

// 端到端口径：ctx 里的起点设在 200ms 前，上游立刻吐一帧带正文的事件，
// 那么这次观测到的 TTFT 应该接近 200ms 而不是接近 0。
func TestStreamProxyTTFTIncludesQueueTime(t *testing.T) {
	const model = "ttft-queue-fixture"
	before := histogramSum(t, model)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), ttftStartKey{},
		time.Now().Add(-200*time.Millisecond)))

	upstream := io.NopCloser(strings.NewReader(`data: {"choices":[{"delta":{"content":"x"}}]}` + "\n\n"))
	if err := streamProxy(httptest.NewRecorder(), req, upstream,
		func(event []byte, isLast bool) []byte { return event }, model); err != nil {
		t.Fatalf("转发失败: %v", err)
	}

	delta := histogramSum(t, model) - before
	if delta < 0.15 {
		t.Fatalf("TTFT 应把等待时间算进去（约 0.2s），实际只记了 %.3fs —— 口径又退回了「响应头之后」", delta)
	}
	if delta > 1.0 {
		t.Fatalf("TTFT 异常偏大: %.3fs", delta)
	}
}

// 上游只发了 role 帧就结束（例如立刻被截断）：没有正文就不该产生 TTFT 样本，
// 否则「没有任何输出」的请求会以极小的 TTFT 混进 p50。
func TestStreamProxyNoTTFTForRoleOnlyStream(t *testing.T) {
	const model = "ttft-role-only-fixture"
	before := histogramSum(t, model)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), ttftStartKey{}, time.Now()))

	upstream := io.NopCloser(strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"))
	if err := streamProxy(httptest.NewRecorder(), req, upstream,
		func(event []byte, isLast bool) []byte { return event }, model); err != nil {
		t.Fatalf("转发失败: %v", err)
	}

	if delta := histogramSum(t, model) - before; delta != 0 {
		t.Fatalf("没有正文的流不该记 TTFT，实际记了 %.3fs", delta)
	}
}

// serveStream 负责把首帧时刻换算成毫秒写进用量流水。
func TestServeStreamFillsTTFT(t *testing.T) {
	h := NewHandler(nil, NewClients(nil, DefaultTimeouts()))
	h.SetStreamingForwarder(func(w http.ResponseWriter, r *http.Request, up io.ReadCloser, sink StreamEventSink, model string) error {
		time.Sleep(30 * time.Millisecond)
		sink([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"), false)
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))
	req = req.WithContext(context.WithValue(req.Context(), ttftStartKey{},
		time.Now().Add(-100*time.Millisecond)))

	rec := &model.AIUsage{}
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}

	h.serveStream(httptest.NewRecorder(), req, resp, "m", []byte(`{}`), nil, false, rec)

	if rec.TTFTMS < 100 {
		t.Fatalf("起点在 100ms 前 + 转发器睡 30ms，TTFT 应 ≥ 100ms，实际 %dms", rec.TTFTMS)
	}
	if rec.TTFTMS > 500 {
		t.Fatalf("TTFT 明显偏大: %dms", rec.TTFTMS)
	}
}

// 两种没有首帧来源的情况都必须保持 0，而不是写出一个假的耗时：
//  1. 转发器一个事件都没吐（上游立刻断开）
//  2. ctx 里没有起点（非流式路径或旧调用方）
func TestServeStreamLeavesTTFTZero(t *testing.T) {
	cases := []struct {
		name      string
		withStart bool
		emitEvent bool
	}{
		{"上游没有吐事件", true, false},
		{"ctx 里没有起点", false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler(nil, NewClients(nil, DefaultTimeouts()))
			h.SetStreamingForwarder(func(w http.ResponseWriter, r *http.Request, up io.ReadCloser, sink StreamEventSink, model string) error {
				if tc.emitEvent {
					// 用带正文的帧：确保 0 值只可能来自「没有起点」，
					// 而不是恰好被「没有正文」这个原因掩盖过去。
					sink([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"), false)
				}
				return nil
			})

			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))
			if tc.withStart {
				req = req.WithContext(context.WithValue(req.Context(), ttftStartKey{}, time.Now()))
			}

			rec := &model.AIUsage{}
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}

			h.serveStream(httptest.NewRecorder(), req, resp, "m", []byte(`{}`), nil, false, rec)

			if rec.TTFTMS != 0 {
				t.Fatalf("没有可信起点时不该写值，实际 %dms", rec.TTFTMS)
			}
		})
	}
}
