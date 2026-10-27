package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

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

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("满员时应快速失败，期望 503，实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), string(overload.ReasonFailFast)) {
		t.Fatalf("响应体应带上拒绝原因, got %q", rec.Body.String())
	}
	if got := served.Load(); got != 1 {
		t.Fatalf("被拒请求不应进入业务处理，业务函数被调用 %d 次", got)
	}

	close(unblock)
	if got := <-first; got.Code != http.StatusOK {
		t.Fatalf("占着名额的请求应正常完成，实际 %d", got.Code)
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
