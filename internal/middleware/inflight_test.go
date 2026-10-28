package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bobdfy/flowgate/internal/errorlog"
	"github.com/bobdfy/flowgate/internal/observability"
	"github.com/bobdfy/flowgate/internal/overload"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// 名额占满时，后续请求必须在入口被拒，且完全不能进入业务处理。
func TestInflightRejectsWhenFull(t *testing.T) {
	guard := overload.NewGuard(1, overload.StrategyFailFast, 0, 0)

	var served atomic.Int32
	entered := make(chan struct{})
	unblock := make(chan struct{})

	h := Inflight(guard, "instance")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Add(1)
		close(entered)
		<-unblock
		w.WriteHeader(http.StatusOK)
	}))

	first := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		first <- rec
	}()
	<-entered // 第一个请求已占住名额

	// 拒绝发生在鉴权与路由之前，没有内层会替它写标签。
	// 这里手动装持有者（不包 Logging，避免把测试流量写进全局指标），
	// 断言拒绝路径自己写的是 overload，而不是留空让外层兜底成 unmatched。
	rejectedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	rejectedReq = rejectedReq.WithContext(withRouteLabel(rejectedReq.Context()))
	rejectedReq = rejectedReq.WithContext(errorlog.WithHolder(rejectedReq.Context()))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, rejectedReq)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("满员时应快速失败，期望 503，实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), string(overload.ReasonFailFast)) {
		t.Fatalf("响应体应带上拒绝原因, got %q", rec.Body.String())
	}
	if got := served.Load(); got != 1 {
		t.Fatalf("被拒请求不应进入业务处理，业务函数被调用 %d 次", got)
	}
	if got := RouteLabel(rejectedReq.Context()); got != LabelOverload {
		t.Fatalf("过载拒绝的路由标签应为 %q，实际 %q", LabelOverload, got)
	}
	// 错误看板要能看出是过载以及当时的在途数，而不是只有一句「503」。
	kind, detail, ok := errorlog.Get(rejectedReq.Context())
	if !ok || kind != errorlog.KindOverload {
		t.Fatalf("应写入 overload 归因，实际 ok=%v kind=%q", ok, kind)
	}
	if !strings.Contains(detail, string(overload.ReasonFailFast)) || !strings.Contains(detail, "inflight") {
		t.Fatalf("归因 detail 应带原因与在途数，实际 %q", detail)
	}

	close(unblock)
	if got := <-first; got.Code != http.StatusOK {
		t.Fatalf("占着名额的请求应正常完成，实际 %d", got.Code)
	}
}

// 放行的请求不能被 inflight 顺手贴上 overload：
// 它的路由标签由更内层的 Router 决定，提前写死会把真实路由名抹掉。
func TestInflightDoesNotLabelAcceptedRequests(t *testing.T) {
	guard := overload.NewGuard(1, overload.StrategyFailFast, 0, 0)
	h := Inflight(guard, "instance")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(withRouteLabel(req.Context()))
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got := RouteLabel(req.Context()); got != "" {
		t.Fatalf("放行的请求不应被 inflight 写标签，实际 %q", got)
	}
}

// 流式响应在整个生命周期内都占着名额：
// 首帧发出后名额不能提前释放，否则长流可以绕过并发上限。
func TestInflightHoldsSlotForWholeStream(t *testing.T) {
	guard := overload.NewGuard(1, overload.StrategyFailFast, 0, 0)

	chunkSent := make(chan int, 8)
	unblock := make(chan struct{})

	h := Inflight(guard, "instance")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(w)
		for i := 0; i < 3; i++ {
			_, _ = w.Write([]byte("data: {}\n\n"))
			_ = rc.Flush()
			chunkSent <- i
			if i == 1 {
				<-unblock // 卡在第二帧之后，模拟还在生成中的长流
			}
		}
	}))

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
		close(done)
	}()

	<-chunkSent // 第 0 帧已发给客户端
	<-chunkSent // 第 1 帧已发给客户端

	// 流已经开始输出，但还没结束 —— 名额必须仍然被占着。
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("流未结束时名额应仍被占用，期望 503，实际 %d", rec.Code)
	}

	close(unblock)
	<-done
	<-chunkSent // 第 2 帧

	// 流结束后名额释放，新请求可以进来。
	after := httptest.NewRecorder()
	h.ServeHTTP(after, httptest.NewRequest(http.MethodGet, "/", nil))
	if after.Code != http.StatusOK {
		t.Fatalf("流结束后名额应已释放，期望 200，实际 %d", after.Code)
	}
}

// 实例级在途数指标要跟着名额走，否则面板上永远是 0。
func TestInflightGaugeTracksSlots(t *testing.T) {
	guard := overload.NewGuard(4, overload.StrategyFailFast, 0, 0)

	before := testutil.ToFloat64(observability.InstanceInflight)
	var inside float64

	h := Inflight(guard, "instance")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inside = testutil.ToFloat64(observability.InstanceInflight)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if inside != before+1 {
		t.Fatalf("请求处理中在途数应为 %.0f，实际 %.0f", before+1, inside)
	}
	if after := testutil.ToFloat64(observability.InstanceInflight); after != before {
		t.Fatalf("请求结束后在途数应回到 %.0f，实际 %.0f", before, after)
	}
}
