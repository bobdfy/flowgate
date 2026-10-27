package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bobdfy/flowgate/internal/netguard"
	"github.com/bobdfy/flowgate/internal/observability"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// 这组测试针对 P1-1「AI 链路分层超时」。
// 分两层写：
//   1. 配置层 —— 传进去的 Timeouts 必须真的落到 Transport / Client 上（防止被默认值悄悄覆盖）；
//   2. 行为层 —— 每个超时真的会触发，而且不会互相误伤（长流不能被空闲计时器杀掉）。
// 用真实 httptest 上游而不是 mock Transport，是因为要测的是「阻塞的 Read 能不能被解除」，
// 这恰恰是 mock 表达不了的部分。

// allowLoopback 放行 127.0.0.1，否则 netguard 会在拨号期就把 httptest 上游拦掉，
// 测试会以「看起来超时了」的假象通过。
func allowLoopback(t *testing.T) *netguard.Policy {
	t.Helper()
	p, err := netguard.NewPolicy([]string{"127.0.0.1/32"})
	if err != nil {
		t.Fatalf("构建 SSRF 策略失败: %v", err)
	}
	return p
}

// 配置层：五个值必须各自落到该去的地方。
// 这里最容易出的问题是「参数解析了、也传进来了，但 NewClients 里用的是 DefaultTimeouts()」——
// 编译和运行都不报错，只是配置永远不生效。
func TestClientsWiresTimeoutsIntoTransport(t *testing.T) {
	tt := Timeouts{
		Connect:        time.Second,
		ResponseHeader: 2 * time.Second,
		Buffered:       3 * time.Second,
		StreamIdle:     4 * time.Second,
		StreamMax:      5 * time.Second,
	}
	c := NewClients(nil, tt)

	tr, ok := c.Stream.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Stream.Transport 应是 *http.Transport，实际 %T", c.Stream.Transport)
	}
	if tr.DialContext == nil {
		t.Fatal("拨号必须走 netguard，DialContext 不能为空")
	}
	if tr.ResponseHeaderTimeout != tt.ResponseHeader {
		t.Fatalf("ResponseHeaderTimeout 应为 %s，实际 %s", tt.ResponseHeader, tr.ResponseHeaderTimeout)
	}
	if c.Buffered.Timeout != tt.Buffered {
		t.Fatalf("非流式总时长应为 %s，实际 %s", tt.Buffered, c.Buffered.Timeout)
	}
	if c.Stream.Timeout != 0 {
		t.Fatalf("流式 client 不能有总超时（会掐断长流），实际 %s", c.Stream.Timeout)
	}

	// 连接池空闲回收和「流式读空闲」是两件事，不能跟着配置走。
	if tr.IdleConnTimeout != 90*time.Second {
		t.Fatalf("连接池空闲回收应固定 90s，实际 %s", tr.IdleConnTimeout)
	}
	if c.Stream.Transport != c.Buffered.Transport {
		t.Fatal("两个 client 必须共用一个 Transport，否则连接池会变成两份")
	}
	if c.timeouts == nil {
		t.Fatal("timeouts 没有保存，forward 里会 nil 解引用")
	}
	if *c.timeouts != tt {
		t.Fatalf("timeouts 保存的值不对: %+v", *c.timeouts)
	}
}

// 行为层：上游迟迟不给响应头，必须在 ResponseHeader 附近失败，
// 而不是一直挂在 ReadResponse 上。
func TestResponseHeaderTimeout(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()

	c := NewClients(allowLoopback(t), Timeouts{
		Connect:        time.Second,
		ResponseHeader: 50 * time.Millisecond,
		Buffered:       5 * time.Second, // 故意放大，确认失败来自响应头超时而不是总时长
		StreamIdle:     5 * time.Second,
	})
	h := NewHandler(nil, c)

	start := time.Now()
	_, err := h.forward(newAIRequest(t, context.Background()), entryFor(up.URL), ChatCompletions, nil, []byte(`{}`), false)
	if err == nil {
		t.Fatal("上游迟迟不给响应头，应当超时失败")
	}
	if d := time.Since(start); d > 300*time.Millisecond {
		t.Fatalf("应在 50ms 左右失败，实际等了 %s", d)
	}
}

// 行为层：非流式请求必须有总时长上界。
// 上游一点点滴数据可以把 ResponseHeader 拖过去，只有 Client.Timeout 能兜住。
func TestBufferedTotalTimeout(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x",`)
		w.(http.Flusher).Flush() // 先给头和一个残片，然后卡住
		time.Sleep(400 * time.Millisecond)
		_, _ = io.WriteString(w, `"ok":true}`)
	}))
	defer up.Close()

	c := NewClients(allowLoopback(t), Timeouts{
		Connect:        time.Second,
		ResponseHeader: 5 * time.Second,
		Buffered:       50 * time.Millisecond,
		StreamIdle:     5 * time.Second,
	})
	h := NewHandler(nil, c)

	// forward 只负责拿到响应头，body 是在 serveBuffered 里读的 ——
	// 总时长覆盖的正是「读完整个 body」，所以断言要放在读取上。
	resp, err := h.forward(newAIRequest(t, context.Background()), entryFor(up.URL), ChatCompletions, nil, []byte(`{}`), false)
	if err != nil {
		t.Fatalf("拿到响应头不该失败: %v", err)
	}
	defer resp.Body.Close()

	start := time.Now()
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatal("非流式请求超过总时长应当失败")
	}
	if d := time.Since(start); d > 300*time.Millisecond {
		t.Fatalf("应在 50ms 左右失败，实际等了 %s", d)
	}
}

// 行为层：流中途上游静默，必须在 StreamIdle 之后解除阻塞的 Read。
//
// 这里测的是本步骤最核心的机制：http.Client.Timeout 不能用来做这件事
// （它会把正常长流一起掐断），所以走 streamBody 的计时器 + ctx cancel。
func TestStreamIdleTimeoutOnSilence(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: hello\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done() // 之后一直沉默，直到网关把连接掐掉
	}))
	defer up.Close()

	c := NewClients(allowLoopback(t), Timeouts{
		Connect:        time.Second,
		ResponseHeader: time.Second,
		Buffered:       time.Second,
		StreamIdle:     100 * time.Millisecond,
	})
	h := NewHandler(nil, c)

	resp, err := h.forward(newAIRequest(t, context.Background()), entryFor(up.URL), ChatCompletions, nil, []byte(`{}`), true)
	if err != nil {
		t.Fatalf("建立流式连接不该失败: %v", err)
	}
	defer resp.Body.Close()

	sb, ok := resp.Body.(*streamBody)
	if !ok {
		t.Fatalf("流式 body 应被包成 *streamBody，实际 %T", resp.Body)
	}

	buf := make([]byte, 512)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatalf("第一帧应当读得到: %v", err)
	}

	// 上游沉默了，这次 Read 必须被 idle 计时器打断 —— 若计时器没生效会永久阻塞。
	done := make(chan error, 1)
	go func() {
		_, err := resp.Body.Read(buf)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("上游沉默后 Read 应当返回错误")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("读空闲超时没有生效，Read 一直阻塞（计时器没取消 ctx）")
	}

	if !sb.idleHit.Load() {
		t.Fatal("这次断开是上游沉默导致的，应归因为 idle_timeout")
	}
}

// 行为层：总时长上限。上游一直有数据（不会触发空闲超时），
// 但超过 StreamMax 后必须断开 —— 否则一个永不停嘴的上游能白嫖一个连接。
func TestStreamMaxDurationCutsLongStream(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			if _, err := io.WriteString(w, "data: tick\n\n"); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			time.Sleep(20 * time.Millisecond)
		}
	}))
	defer up.Close()

	c := NewClients(allowLoopback(t), Timeouts{
		Connect:        time.Second,
		ResponseHeader: time.Second,
		Buffered:       time.Second,
		StreamIdle:     time.Second, // 远大于总时长，确保不是空闲超时误判
		StreamMax:      100 * time.Millisecond,
	})
	h := NewHandler(nil, c)

	resp, err := h.forward(newAIRequest(t, context.Background()), entryFor(up.URL), ChatCompletions, nil, []byte(`{}`), true)
	if err != nil {
		t.Fatalf("建立流式连接不该失败: %v", err)
	}
	defer resp.Body.Close()

	sb := resp.Body.(*streamBody)

	start := time.Now()
	_, err = io.Copy(io.Discard, resp.Body)
	if err == nil {
		t.Fatal("超过流式总时长上限应当断开")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("应在 100ms 左右断开，实际 %s", d)
	}
	if sb.idleHit.Load() {
		t.Fatal("上游一直在吐数据，不该被归因为 idle_timeout")
	}
}

// 反向保护：只要帧间隔小于 StreamIdle，整条流跑多久都不能被杀。
// 没有这条，「空闲计时器不续命」这种 bug 会让所有正常长流在 60s 处无声截断，
// 而上面的超时测试全是绿的。
func TestStreamUnderIdleGapIsNotKilled(t *testing.T) {
	const (
		frames = 10
		gap    = 30 * time.Millisecond // 10 帧 → 约 300ms 总时长，远超 idle
	)

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < frames; i++ {
			if _, err := io.WriteString(w, "data: tick\n\n"); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			time.Sleep(gap)
		}
	}))
	defer up.Close()

	c := NewClients(allowLoopback(t), Timeouts{
		Connect:        time.Second,
		ResponseHeader: time.Second,
		Buffered:       time.Second,
		StreamIdle:     100 * time.Millisecond, // 大于帧间隔，小于总时长
	})
	h := NewHandler(nil, c)

	resp, err := h.forward(newAIRequest(t, context.Background()), entryFor(up.URL), ChatCompletions, nil, []byte(`{}`), true)
	if err != nil {
		t.Fatalf("建立流式连接不该失败: %v", err)
	}
	defer resp.Body.Close()

	n, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		t.Fatalf("持续有数据的流不该被空闲超时掐断（计时器没有续命）: %v", err)
	}
	if want := int64(frames * len("data: tick\n\n")); n != want {
		t.Fatalf("应读到 %d 字节，实际 %d", want, n)
	}
}

// 结束原因归因：口径错了，看板上的「超时率」就是假的。
func TestServeStreamTerminationReason(t *testing.T) {
	cases := []struct {
		name         string
		idleHit      bool
		cancelClient bool
		streamErr    error
		want         string
	}{
		{"上游沉默", true, false, errors.New("read: connection reset by peer"), "idle_timeout"},
		{"客户端断开", false, true, context.Canceled, "client_cancel"},
		{"超过总时长", false, false, context.DeadlineExceeded, "max_duration"},
		{"上游错误", false, false, errors.New("boom"), "upstream_error"},
		{"正常结束", false, false, nil, "normal"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := newStreamBody(io.NopCloser(strings.NewReader("")), 0, func() {})
			body.idleHit.Store(tc.idleHit)

			h := NewHandler(nil, NewClients(nil, DefaultTimeouts()))
			h.SetStreamingForwarder(func(w http.ResponseWriter, r *http.Request, up io.ReadCloser, sink StreamEventSink, model string) error {
				_, _ = io.Copy(io.Discard, up)
				return tc.streamErr
			})

			ctx, cancel := context.WithCancel(context.Background())
			if tc.cancelClient {
				cancel()
			} else {
				defer cancel()
			}

			req := newAIRequest(t, ctx)
			before := testutil.ToFloat64(observability.AIStreamTerminatedTotal.WithLabelValues(tc.want))

			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}
			h.serveStream(httptest.NewRecorder(), req, resp, "m", []byte(`{}`), nil, false, nil)

			if d := testutil.ToFloat64(observability.AIStreamTerminatedTotal.WithLabelValues(tc.want)) - before; d != 1 {
				t.Fatalf("reason=%s 的计数应 +1，实际 %v", tc.want, d)
			}
		})
	}
}

// newAIRequest 构造一个面向 ChatCompletions 的请求。
func newAIRequest(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))
	return req.WithContext(ctx)
}

func entryFor(baseURL string) *ProviderEntry {
	return &ProviderEntry{Name: "test", BaseURL: baseURL}
}
