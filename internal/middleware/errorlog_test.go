package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bobdfy/flowgate/internal/errorlog"
	"github.com/bobdfy/flowgate/internal/model"
)

// 这组测试要防的错：
//  1. 把 404 / 400 也记进去 → 扫描器噪声把真正的错误挤出 500 条窗口
//  2. 流式响应已经写了 200 却没记录 → 用户看到「一切正常」，其实流断了
//  3. 归因被状态码覆盖 → 熔断和普通过载混在一起，没法定位

type entrySink struct{ entries []errorlog.Entry }

func (s *entrySink) Record(e errorlog.Entry) { s.entries = append(s.entries, e) }

// 这里刻意复用 logging_test.go 里的路由标签与方法（/orders + GET）：
// 指标是包级全局的，如果这里引入新标签，那些「标签取值有界」的断言会被干扰。
func serveWithSink(sink *entrySink, status int, mutate func(*http.Request)) *httptest.ResponseRecorder {
	h := RequestID(LoggingWithErrors(sink, "gw-1", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetRouteLabel(r, "/orders")
		if mutate != nil {
			mutate(r)
		}
		w.WriteHeader(status)
	})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/orders", nil))
	return rec
}

func TestLoggingRecordsServerError(t *testing.T) {
	sink := &entrySink{}
	serveWithSink(sink, http.StatusBadGateway, nil)

	if len(sink.entries) != 1 {
		t.Fatalf("502 应记录 1 条，实际 %d", len(sink.entries))
	}
	e := sink.entries[0]
	if e.Kind != errorlog.KindUpstream {
		t.Fatalf("502 应归为 upstream_error，实际 %s", e.Kind)
	}
	if e.Instance != "gw-1" || e.Route != "/orders" || e.Status != http.StatusBadGateway {
		t.Fatalf("字段不完整：%+v", e)
	}
	if e.RequestID == "" {
		t.Fatal("应带上 request_id，否则没法去日志里搜详情")
	}
}

func TestLoggingSkipsClientErrors(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusRequestEntityTooLarge,
	} {
		sink := &entrySink{}
		serveWithSink(sink, status, nil)
		if len(sink.entries) != 0 {
			t.Fatalf("%d 是客户端错误，不该进错误看板，实际记了 %d 条", status, len(sink.entries))
		}
	}
}

func TestLoggingRecordsGatekeeperRejections(t *testing.T) {
	cases := []struct {
		status int
		want   errorlog.Kind
	}{
		{http.StatusUnauthorized, errorlog.KindAuth},
		{http.StatusForbidden, errorlog.KindAuth},
		{http.StatusTooManyRequests, errorlog.KindOverload},
		{http.StatusServiceUnavailable, errorlog.KindOverload},
	}
	for _, c := range cases {
		sink := &entrySink{}
		serveWithSink(sink, c.status, nil)
		if len(sink.entries) != 1 {
			t.Fatalf("%d 应记录 1 条，实际 %d", c.status, len(sink.entries))
		}
		if got := sink.entries[0].Kind; got != c.want {
			t.Fatalf("%d 应归为 %s，实际 %s", c.status, c.want, got)
		}
	}
}

// 流式响应已经写出 200 后中途断开：状态码看不出来，必须靠内层显式归因。
func TestLoggingRecordsExplicitReasonEvenOn200(t *testing.T) {
	sink := &entrySink{}
	serveWithSink(sink, http.StatusOK, func(r *http.Request) {
		errorlog.Set(r.Context(), errorlog.KindStream, "upstream_error: EOF")
	})

	if len(sink.entries) != 1 {
		t.Fatalf("显式归因的请求必须记录，实际 %d 条", len(sink.entries))
	}
	e := sink.entries[0]
	if e.Kind != errorlog.KindStream {
		t.Fatalf("归因应保持 stream_terminated，实际 %s", e.Kind)
	}
	if e.Status != http.StatusOK {
		t.Fatalf("状态码应如实记录 200，实际 %d", e.Status)
	}
	if e.Detail != "upstream_error: EOF" {
		t.Fatalf("detail 应带上原因，实际 %q", e.Detail)
	}
}

// 内层归因优先于状态码：熔断返回的 503 不该被记成普通过载。
func TestLoggingExplicitReasonOverridesStatus(t *testing.T) {
	sink := &entrySink{}
	serveWithSink(sink, http.StatusServiceUnavailable, func(r *http.Request) {
		errorlog.Set(r.Context(), errorlog.KindCircuit, "provider deepseek 熔断中")
	})

	if len(sink.entries) != 1 {
		t.Fatalf("应记录 1 条，实际 %d", len(sink.entries))
	}
	if got := sink.entries[0].Kind; got != errorlog.KindCircuit {
		t.Fatalf("归因应为 circuit_open，实际 %s", got)
	}
}

func TestLoggingCarriesIdentity(t *testing.T) {
	sink := &entrySink{}
	h := LoggingWithErrors(sink, "gw-1", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetRouteLabel(r, "/orders")
		// 模拟 RequireAuth：身份是在内层新建的 request 上写入的，
		// 外层 Logging 手上的 request 看不到 —— 必须靠快照。
		r = r.WithContext(setIdentity(r.Context(), &model.APIKey{ID: 41, TenantID: 17}))
		errorlog.Set(r.Context(), errorlog.KindUpstream, "boom")
		w.WriteHeader(http.StatusBadGateway)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orders", nil))

	if len(sink.entries) != 1 {
		t.Fatalf("应记录 1 条，实际 %d", len(sink.entries))
	}
	e := sink.entries[0]
	if e.TenantID != 17 || e.KeyID != 41 {
		t.Fatalf("应带上租户与 Key 的数值 ID，实际 tenant=%d key=%d", e.TenantID, e.KeyID)
	}
}

// 未启用错误看板时（sink 为 nil），Logging 行为不变。
func TestLoggingWithoutSinkStillServes(t *testing.T) {
	h := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("响应应原样透传，实际 %d", rec.Code)
	}
}
