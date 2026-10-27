package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// labelValues 从默认 Gatherer 里取出某个指标的某个标签出现过的所有取值。
func labelValues(t *testing.T, metricName, labelName string) map[string]bool {
	t.Helper()

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("采集指标失败: %v", err)
	}

	out := map[string]bool{}
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == labelName {
					out[lp.GetValue()] = true
				}
			}
		}
	}
	return out
}

// 随机路径不得产生新的指标序列：标签必须来自路由模板，未匹配的统一记 unmatched。
func TestRequestMetricsPathLabelsAreBounded(t *testing.T) {
	// 模拟「命中路由」的 handler：内层把固定模板写进标签
	routed := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetRouteLabel(r, "/orders")
		w.WriteHeader(http.StatusOK)
	}))
	for i := 0; i < 300; i++ {
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/orders/%d/items?x=%d", i, i), nil)
		routed.ServeHTTP(httptest.NewRecorder(), r)
	}

	// 模拟「没有任何路由匹配」：内层不写标签，Logging 兜底成 unmatched
	unrouted := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	for i := 0; i < 300; i++ {
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/random-%d", i), nil)
		unrouted.ServeHTTP(httptest.NewRecorder(), r)
	}

	got := labelValues(t, "flowgate_requests_total", "route")
	want := map[string]bool{"/orders": true, LabelUnmatched: true}
	if len(got) != len(want) {
		t.Fatalf("route 标签取值应只有 %v, 实际 %v", want, got)
	}
	for k := range want {
		if !got[k] {
			t.Fatalf("缺少 route 标签 %q, 实际 %v", k, got)
		}
	}
}

// 客户端随便发的自定义方法不能撑大 method 标签。
func TestRequestMetricsMethodLabelIsBounded(t *testing.T) {
	h := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetRouteLabel(r, "/things")
		w.WriteHeader(http.StatusOK)
	}))

	for _, m := range []string{"GET", "FOOBAR", "PURGE", "X-CUSTOM"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(m, "/things", nil))
	}

	got := labelValues(t, "flowgate_requests_total", "method")
	want := map[string]bool{http.MethodGet: true, "OTHER": true}
	if len(got) != len(want) {
		t.Fatalf("method 标签应只有 %v, 实际 %v", want, got)
	}
	for k := range want {
		if !got[k] {
			t.Fatalf("缺少 method 标签 %q, 实际 %v", k, got)
		}
	}
}
