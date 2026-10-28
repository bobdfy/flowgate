package ai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bobdfy/flowgate/internal/loadbalance"
)

// 这组测试要防的错，都是「看起来正常、实际失效」的那类：
//  1. 4xx（密钥错、参数错）被当成上游故障 → 一个配错的 key 会把整个 provider 熔断
//  2. 客户端取消被当成上游故障 → 用户多刷新几次就把上游熔断掉（P0-12）
//  3. 半开态放行多条试探 → 恢复瞬间全量打回，上游二次雪崩
//  4. 熔断前发出的旧请求迟到成功，把半开试探误判为「已恢复」

func TestBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	b := newBreaker("p")

	for i := 0; i < breakerFailureThreshold; i++ {
		l, ok := b.acquire()
		if !ok {
			t.Fatalf("第 %d 次不该被熔断", i+1)
		}
		l.Done(loadbalance.OutcomeFailure)
	}

	if _, ok := b.acquire(); ok {
		t.Fatal("连续失败达到阈值后应熔断")
	}
}

// 成功一次就把连续失败清零 —— 熔断看的是「连续」失败，不是累计。
func TestBreakerSuccessResetsFailureCount(t *testing.T) {
	b := newBreaker("p")

	for range breakerFailureThreshold - 1 {
		l, _ := b.acquire()
		l.Done(loadbalance.OutcomeFailure)
	}

	l, _ := b.acquire()
	l.Done(loadbalance.OutcomeSuccess)

	for range breakerFailureThreshold - 1 {
		l, _ := b.acquire()
		l.Done(loadbalance.OutcomeFailure)
	}

	if _, ok := b.acquire(); !ok {
		t.Fatal("中间有成功，累计失败不该触发熔断")
	}
}

// 中性结果（4xx、客户端取消）不参与计数。
func TestBreakerNeutralDoesNotCount(t *testing.T) {
	b := newBreaker("p")

	for i := 0; i < breakerFailureThreshold*3; i++ {
		l, ok := b.acquire()
		if !ok {
			t.Fatal("中性结果不该触发熔断")
		}
		l.Done(loadbalance.OutcomeNeutral)
	}
}

// 冷却到期后只放一条试探，其余请求继续快速失败。
func TestBreakerHalfOpenAllowsSingleProbe(t *testing.T) {
	b := newBreaker("p")
	tripBreaker(b)

	if _, ok := b.acquire(); ok {
		t.Fatal("冷却期内不该放行")
	}

	expireCooldown(b)

	probe, ok := b.acquire()
	if !ok {
		t.Fatal("冷却到期后应放一条试探")
	}
	if _, ok := b.acquire(); ok {
		t.Fatal("试探还没结算时，第二条不应放行")
	}

	probe.Done(loadbalance.OutcomeSuccess)
	if _, ok := b.acquire(); !ok {
		t.Fatal("试探成功后应恢复放行")
	}
}

// 试探失败 → 重新打开，冷却重新计时。
func TestBreakerProbeFailureReopens(t *testing.T) {
	b := newBreaker("p")
	tripBreaker(b)
	expireCooldown(b)

	probe, ok := b.acquire()
	if !ok {
		t.Fatal("应放一条试探")
	}
	probe.Done(loadbalance.OutcomeFailure)

	if _, ok := b.acquire(); ok {
		t.Fatal("试探失败后应重新熔断")
	}
}

// 熔断前发出的旧请求迟到成功，不能把半开态判成已恢复。
func TestBreakerStaleResultCannotCloseHalfOpen(t *testing.T) {
	b := newBreaker("p")

	stale, ok := b.acquire() // 熔断前发出，尚未结算
	if !ok {
		t.Fatal("closed 态应放行")
	}

	tripBreaker(b)
	expireCooldown(b)

	probe, ok := b.acquire()
	if !ok {
		t.Fatal("冷却到期应放一条试探")
	}

	stale.Done(loadbalance.OutcomeSuccess) // 迟到的成功

	b.mu.Lock()
	state := b.state
	inflight := b.halfOpenInFlight
	b.mu.Unlock()

	if state != loadbalance.StateHalfOpen {
		t.Fatalf("旧请求的成功不应改变半开态，实际 %s", state)
	}
	if inflight != 1 {
		t.Fatalf("试探名额应仍被占用，实际 %d", inflight)
	}

	probe.Done(loadbalance.OutcomeSuccess)
}

// 4xx 是我们的错，5xx/429/408 才是上游的错。
func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		code int
		want loadbalance.Outcome
	}{
		{200, loadbalance.OutcomeSuccess},
		{201, loadbalance.OutcomeSuccess},
		{400, loadbalance.OutcomeNeutral},
		{401, loadbalance.OutcomeNeutral},
		{404, loadbalance.OutcomeNeutral},
		{408, loadbalance.OutcomeFailure},
		{429, loadbalance.OutcomeFailure},
		{500, loadbalance.OutcomeFailure},
		{503, loadbalance.OutcomeFailure},
	}
	for _, c := range cases {
		if got := classifyStatus(c.code); got != c.want {
			t.Errorf("classifyStatus(%d) = %v, want %v", c.code, got, c.want)
		}
	}
}

// 客户端取消不能算上游故障。
func TestClassifyForwardError(t *testing.T) {
	if got := classifyForwardError(context.Canceled); got != loadbalance.OutcomeNeutral {
		t.Fatalf("客户端取消应为中性，实际 %v", got)
	}
	if got := classifyForwardError(context.DeadlineExceeded); got != loadbalance.OutcomeFailure {
		t.Fatalf("超时应为失败，实际 %v", got)
	}
	if got := classifyForwardError(errors.New("connection refused")); got != loadbalance.OutcomeFailure {
		t.Fatalf("连接失败应为失败，实际 %v", got)
	}
}

// 熔断期间返回 503 + Retry-After，且错误体是 OpenAI 结构。
func TestAcquireBreakerSlotWrites503(t *testing.T) {
	h := NewHandler(map[string]*ProviderEntry{
		"m": {Name: "p"},
	}, nil)
	tripBreaker(h.breakers["p"])

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if _, ok := h.acquireBreakerSlot(rec, req, h.providers["m"]); ok {
		t.Fatal("熔断中应拒绝")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码应为 503，实际 %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("应带 Retry-After")
	}
	if body := rec.Body.String(); !strings.Contains(body, "upstream") {
		t.Fatalf("错误体应说明上游不可用，实际 %s", body)
	}
}

// 端到端：上游连续返回 500，达到阈值后网关不再请求上游，直接 503。
func TestServeHTTPOpensBreakerOnUpstream5xx(t *testing.T) {
	var hits atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer up.Close()

	h := NewHandler(map[string]*ProviderEntry{
		"m": {Name: "p", BaseURL: up.URL},
	}, NewClients(allowLoopback(t), DefaultTimeouts()))

	body := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	for i := 0; i < breakerFailureThreshold; i++ {
		h.ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	}

	before := hits.Load()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("熔断后应返回 503，实际 %d", rec.Code)
	}
	if hits.Load() != before {
		t.Fatal("熔断后不应再打到上游")
	}
}

// 端到端：4xx 不累积失败，永远不会熔断。
func TestServeHTTPDoesNotOpenBreakerOn4xx(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer up.Close()

	h := NewHandler(map[string]*ProviderEntry{
		"m": {Name: "p", BaseURL: up.URL},
	}, NewClients(allowLoopback(t), DefaultTimeouts()))

	body := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	for i := 0; i < breakerFailureThreshold*3; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec,
			httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
		if rec.Code == http.StatusServiceUnavailable {
			t.Fatalf("第 %d 次请求被熔断了 —— 4xx 不应计入上游故障", i+1)
		}
	}
}

// tripBreaker 直接把熔断器打到 Open，省去制造失败的过程。
func tripBreaker(b *breaker) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tripLocked(loadbalance.StateClosed, "test")
}

// expireCooldown 把冷却结束时间挪到过去，模拟「等够了」。
func expireCooldown(b *breaker) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.openUntil = time.Now().Add(-time.Second)
}
