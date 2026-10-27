package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bobdfy/flowgate/internal/loadbalance"
	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/netguard"
	"github.com/bobdfy/flowgate/internal/observability"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// 这一组是「端到端」测试：从配置快照 BuildRoutes，到 Router 匹配、两层护栏、
// Backend 转发，全部走真实路径，只把上游换成 httptest。
// 这些场景用 shell 造不稳（并发、取消、熔断计数），所以放在 Go 里。

// buildTable 按线上的方式搭路由表：传 JSON 快照给 BuildRoutes，
// 而不是手工 new Backend —— 否则测的就不是线上的装配逻辑了。
func buildTable(t *testing.T, svc model.Service, route model.Route, upstreamAddr string) RouteTable {
	t.Helper()

	policy, err := netguard.NewPolicy([]string{"127.0.0.1/32"}) // httptest 就监听回环，必须放行
	if err != nil {
		t.Fatalf("构建 SSRF 策略失败: %v", err)
	}

	node := model.Node{ID: 100, ServiceID: svc.ID, Address: upstreamAddr, Weight: 1, Enabled: true}

	table, err := BuildRoutes([]model.VersionItem{
		snapshot(t, "service", svc.ID, svc),
		snapshot(t, "node", node.ID, node),
		snapshot(t, "route", route.ID, route),
	}, NewProxyCache(policy), map[int64]*loadbalance.NodePool{})
	if err != nil {
		t.Fatalf("BuildRoutes 失败: %v", err)
	}
	if len(table) != 1 {
		t.Fatalf("应构建出 1 条路由，实际 %d 条", len(table))
	}
	return table
}

func snapshot(t *testing.T, kind string, id int64, v any) model.VersionItem {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化 %s 快照失败: %v", kind, err)
	}
	return model.VersionItem{ResourceType: kind, ResourceID: id, ResourceSnapshot: b}
}

// blockedUpstream 起一个「进来先卡住，等测试放行」的上游。
// 返回的 entered 用于确认第一个请求真的占住名额了 —— 靠 sleep 猜时间会 flaky。
func blockedUpstream(t *testing.T) (addr string, entered <-chan struct{}, release func()) {
	t.Helper()

	enteredCh := make(chan struct{}, 1)
	releaseCh := make(chan struct{})
	var once sync.Once

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case enteredCh <- struct{}{}:
		default:
		}
		<-releaseCh
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(up.Close)

	stop := func() { once.Do(func() { close(releaseCh) }) }
	t.Cleanup(stop) // LIFO：先放行上游，再关 server，否则 up.Close 会一直等

	return up.URL, enteredCh, stop
}

// D4 路由级并发护栏（Bulkhead）：名额满了必须立刻回 503，
// 而且绝不能继续往下走 —— 这是「隔离」的全部意义。
func TestRouteBulkheadRejectsSecondRequest(t *testing.T) {
	addr, entered, release := blockedUpstream(t)

	svc := model.Service{ID: 1, Name: "svc-d4", RequestTimeoutMs: 5000, ResponseHeaderTimeoutMs: 5000}
	route := model.Route{
		ID: 1, ServiceID: 1, PathPattern: "/", PathMatchType: "prefix",
		Enabled: true, MaxConcurrency: 1, // 路由级只放 1 个
	}

	r := NewRouter()
	r.Swap(buildTable(t, svc, route, addr))

	// 第一个请求占住唯一的名额
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/slow", nil))
	}()
	<-entered

	before := testutil.ToFloat64(observability.BulkheadRejectedTotal.WithLabelValues("/", "fail_fast"))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/slow", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("名额已满应立刻回 503，实际 %d（body=%q）", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "overload") {
		t.Fatalf("响应体应说明是过载，实际 %q", got)
	}
	if d := testutil.ToFloat64(observability.BulkheadRejectedTotal.WithLabelValues("/", "fail_fast")) - before; d != 1 {
		t.Fatalf("路由级拒绝计数应 +1，实际 %v", d)
	}

	release()
	wg.Wait()
}

// C2 幂等重试：GET 遇到 502 换节点/退避重试，POST 不能重试
// （非幂等请求重放，上游可能已经扣了钱/建了单）。
func TestRetryOnlyForIdempotentMethods(t *testing.T) {
	var attempts atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()

	svc := model.Service{
		ID: 2, Name: "svc-c2",
		MaxRetries: 2, RetryOnStatus: "502", RetryBackoffMs: 1,
		RequestTimeoutMs: 5000, ResponseHeaderTimeoutMs: 5000,
	}
	route := model.Route{ID: 2, ServiceID: 2, PathPattern: "/", PathMatchType: "prefix", Enabled: true}

	r := NewRouter()
	r.Swap(buildTable(t, svc, route, up.URL))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET 前两次 502 后应重试成功，实际 %d", rec.Code)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("GET 应打 3 次上游，实际 %d 次", got)
	}

	attempts.Store(0)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("POST 不该重试，应原样返回 502，实际 %d", rec.Code)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("POST 只该打 1 次上游，实际 %d 次", got)
	}
}

// C3 熔断：连续失败到阈值后节点被摘掉，后续请求快速失败，
// 不再每次都往上打 —— 上游已经挂了还继续打，只会拖垮自己。
func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	var hits atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer up.Close()

	svc := model.Service{
		ID: 3, Name: "svc-c3",
		MaxRetries: 0, CBFailureThreshold: 2, CBCooldownMs: 60000, CBHalfOpenLimit: 1,
		RequestTimeoutMs: 5000, ResponseHeaderTimeoutMs: 5000,
	}
	route := model.Route{ID: 3, ServiceID: 3, PathPattern: "/", PathMatchType: "prefix", Enabled: true}

	r := NewRouter()
	r.Swap(buildTable(t, svc, route, up.URL))

	for i := 1; i <= 2; i++ {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("第 %d 次应原样返回 502，实际 %d", i, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("熔断后应快速失败 503，实际 %d", rec.Code)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("熔断后不该再打上游，应共 2 次，实际 %d 次", got)
	}
}

// D5 客户端断开不算「我们拒了你」：排队期间客户端自己走了，
// 过载拒绝计数不能涨 —— 否则用户网络差会被误判成系统容量不足。
func TestClientCancelWhileQueuedIsNotAnOverloadRejection(t *testing.T) {
	addr, entered, release := blockedUpstream(t)

	svc := model.Service{
		ID: 4, Name: "svc-d5",
		MaxConcurrency: 1, OverloadStrategy: "wait", QueueTimeoutMs: 500, // 排队上限 500ms
		RequestTimeoutMs: 5000, ResponseHeaderTimeoutMs: 5000,
	}
	route := model.Route{ID: 4, ServiceID: 4, PathPattern: "/", PathMatchType: "prefix", Enabled: true}

	r := NewRouter()
	r.Swap(buildTable(t, svc, route, addr))

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/slow", nil))
	}()
	<-entered // 名额已被占住

	before := testutil.ToFloat64(observability.OverloadRejectedTotal.WithLabelValues("svc-d5", "queue_timeout"))

	// 排队 100ms 后客户端主动断开（早于 500ms 的排队上限）
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/slow", nil).WithContext(ctx))

	if d := testutil.ToFloat64(observability.OverloadRejectedTotal.WithLabelValues("svc-d5", "queue_timeout")) - before; d != 0 {
		t.Fatalf("客户端自己断开不该记过载，实际拒绝计数 +%v", d)
	}
	if rec.Code == http.StatusServiceUnavailable {
		t.Fatalf("客户端已断开时不该再回 503，实际 %d", rec.Code)
	}

	// 放行第一个请求；名额必须被正确归还，后续请求还能正常服务。
	// 计数被多占的话，这里会一直排队直到超时。
	release()
	wg.Wait()

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/slow", nil))
	if rec.Code == http.StatusServiceUnavailable {
		t.Fatalf("取消的等待者不该占着名额，实际 %d（body=%q）", rec.Code, rec.Body.String())
	}
}
