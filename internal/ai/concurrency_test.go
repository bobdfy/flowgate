package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bobdfy/flowgate/internal/identity"
)

// 模型级并发这组测试要防的是四类错，每一类都「不报错、看不出」：
//  1. 名额不覆盖整条 SSE（在 forward 里就还了）→ 上限形同虚设
//  2. 总名额拿不到时忘了还租户名额 → 这个租户的名额越用越少，最后自己把自己锁死
//  3. 租户公平失效 → 一个租户吃干整个模型
//  4. 客户端断开后名额没还 → 跑一晚全部「超载」，其实没人用

func TestModelLimiterRejectsWhenModelFull(t *testing.T) {
	l := newModelLimiter("m", 2)

	r1, scope, err := l.acquire(context.Background(), 1)
	if err != nil || scope != "" {
		t.Fatalf("第 1 个名额应成功，scope=%q err=%v", scope, err)
	}
	r2, _, err := l.acquire(context.Background(), 2)
	if err != nil {
		t.Fatalf("第 2 个名额应成功: %v", err)
	}

	// limit=2 → 单租户名额 max(2/4,1)=1，所以第 3 个请求会先在租户层被拒。
	// 这里换一个新租户，确认拒绝来自「模型层」。
	_, scope, err = l.acquire(context.Background(), 3)
	if err == nil {
		t.Fatal("名额已满，应当拒绝")
	}
	if scope != "model" {
		t.Fatalf("应被 model 层拒绝，实际 %q", scope)
	}

	r1()
	r2()
}

// 单租户不能把一个模型吃干，但其他租户必须还能用。
func TestModelLimiterTenantFairness(t *testing.T) {
	l := newModelLimiter("m", 8) // 单租户上限 = 8 * 90% = 7

	releaseA := make([]func(), 0, 7)
	for i := 0; i < 7; i++ {
		r, _, err := l.acquire(context.Background(), 1)
		if err != nil {
			t.Fatalf("租户1 第 %d 个名额应成功: %v", i+1, err)
		}
		releaseA = append(releaseA, r)
	}

	if _, scope, err := l.acquire(context.Background(), 1); err == nil || scope != "tenant" {
		t.Fatalf("租户1 超出单租户名额应被 tenant 层拒绝，实际 scope=%q err=%v", scope, err)
	}

	// 租户1 已经占满自己的份额，但模型还剩 1 个名额，租户2 必须能用。
	rB, _, err := l.acquire(context.Background(), 2)
	if err != nil {
		t.Fatalf("租户2 不应受影响: %v", err)
	}
	rB()
	for _, r := range releaseA {
		r()
	}
}

// 模型层拿不到名额时，租户层必须先还回去 —— 否则该租户的名额永久 -1。
func TestModelLimiterReleasesTenantSlotOnModelReject(t *testing.T) {
	l := newModelLimiter("m", 2) // 单租户名额 1

	ra, _, _ := l.acquire(context.Background(), 1)
	rb, _, _ := l.acquire(context.Background(), 2)

	// 租户3 自己的名额是空的，但模型满了 → 应在 model 层被拒。
	if _, scope, err := l.acquire(context.Background(), 3); err == nil || scope != "model" {
		t.Fatalf("应被 model 层拒绝，实际 scope=%q err=%v", scope, err)
	}

	ra() // 让出一个模型名额

	// 如果租户3 的名额在上一步泄漏了，这里会因为 tenant 层满而被拒。
	r3, scope, err := l.acquire(context.Background(), 3)
	if err != nil {
		t.Fatalf("被 model 层拒绝后租户名额必须归还，这里应成功，实际 scope=%q err=%v", scope, err)
	}
	r3()
	rb()
}

func TestNewHandlerOnlyBuildsLimitsForLimitedModels(t *testing.T) {
	h := NewHandler(map[string]*ProviderEntry{
		"limited":   {Name: "a", MaxConcurrency: 4},
		"unlimited": {Name: "b", MaxConcurrency: 0},
		"nilentry":  nil,
	}, nil)

	if len(h.limits) != 1 {
		t.Fatalf("只有配了上限的模型才建名额，实际 %d 个", len(h.limits))
	}
	if _, ok := h.limits["limited"]; !ok {
		t.Fatal("limited 应有限额")
	}
}

func TestAcquireModelSlotWrites429(t *testing.T) {
	h := NewHandler(map[string]*ProviderEntry{
		"m": {Name: "a", MaxConcurrency: 1},
	}, nil)

	// 先占满
	first, ok := h.acquireModelSlot(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil), "m")
	if !ok {
		t.Fatal("第一个名额应成功")
	}

	rec := httptest.NewRecorder()
	if _, ok := h.acquireModelSlot(rec, httptest.NewRequest(http.MethodPost, "/", nil), "m"); ok {
		t.Fatal("名额已满，应拒绝")
	}

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("状态码应为 429，实际 %d", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After 应为 1，实际 %q", got)
	}
	// 必须是 OpenAI 错误结构，客户端 SDK 才能正常解析
	if body := rec.Body.String(); !strings.Contains(body, "rate_limit_error") {
		t.Fatalf("错误体应为 rate_limit_error，实际 %s", body)
	}

	first()
}

// 端到端：名额必须覆盖整条 SSE。
//
// 这条是最容易写错的地方 —— 如果把 Acquire 放进 forward()，
// 第一条流刚开始读的时候名额就已经还回去了，这里的第二条请求会成功，
// 而线上表现为「设了并发上限但完全没生效」。
func TestServeHTTPHoldsSlotForWholeStream(t *testing.T) {
	upstreamRelease := make(chan struct{})

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
		fl.Flush()
		<-upstreamRelease // 一直挂住，模拟长流
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	defer up.Close()

	h := NewHandler(map[string]*ProviderEntry{
		"m": {Name: "test", BaseURL: up.URL, MaxConcurrency: 1},
	}, NewClients(allowLoopback(t), DefaultTimeouts()))
	h.SetStreamingForwarder(streamProxy)

	body := `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`

	// 第一条流：必须在 goroutine 里跑，否则 tests 卡死。
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Errorf("第一条流不该被限流")
		}
	}()

	waitUntil(t, 2*time.Second, func() bool {
		return h.limits["m"].total.Stats().InFlight == 1
	})

	// 第二条：名额只有 1 个，必须 429
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("流进行中时第二个请求应为 429，实际 %d —— 名额很可能没有覆盖整条流", rec.Code)
	}

	close(upstreamRelease)
	wg.Wait() // ServeHTTP 返回 = 流已经结束 = defer 里的 release 已执行

	waitUntil(t, 2*time.Second, func() bool {
		return h.limits["m"].total.Stats().InFlight == 0
	})

	// 流结束后名额必须自己还回来
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	if rec.Code == http.StatusTooManyRequests {
		t.Fatal("流已结束，名额应当已归还")
	}
}

// 客户端中途断开：serveStream 读失败提前返回，defer 必须把名额还掉。
// 漏了这条，跑一晚上名额会被「已经没人要的流」占满。
func TestServeHTTPReleasesSlotOnClientDisconnect(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done() // 客户端断开后上游跟着结束
	}))
	defer up.Close()

	h := NewHandler(map[string]*ProviderEntry{
		"m": {Name: "test", BaseURL: up.URL, MaxConcurrency: 1},
	}, NewClients(allowLoopback(t), DefaultTimeouts()))

	body := `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)).WithContext(ctx)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}()

	waitUntil(t, 2*time.Second, func() bool {
		return h.limits["m"].total.Stats().InFlight == 1
	})

	cancel() // 客户端断开
	wg.Wait()

	waitUntil(t, 2*time.Second, func() bool {
		return h.limits["m"].total.Stats().InFlight == 0
	})
}

// 带身份时，租户名额按 TenantID 分桶。
func TestAcquireModelSlotUsesTenantFromContext(t *testing.T) {
	h := NewHandler(map[string]*ProviderEntry{
		"m": {Name: "a", MaxConcurrency: 8}, // 单租户 8 * 90% = 7
	}, nil)

	ctx := identity.WithIdentity(context.Background(), &identity.Identity{TenantID: 7})

	rel := make([]func(), 0, 7)
	for i := 0; i < 7; i++ {
		r, ok := h.acquireModelSlot(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx), "m")
		if !ok {
			t.Fatalf("租户7 第 %d 个名额应成功", i+1)
		}
		rel = append(rel, r)
	}

	rec := httptest.NewRecorder()
	if _, ok := h.acquireModelSlot(rec, httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx), "m"); ok {
		t.Fatal("租户7 超出自己的份额应被拒绝")
	}
	if !strings.Contains(rec.Body.String(), "tenant 级") {
		t.Fatalf("错误信息应点明是租户级限制，实际 %s", rec.Body.String())
	}

	// 另一个租户不受影响
	ctx2 := identity.WithIdentity(context.Background(), &identity.Identity{TenantID: 8})
	r8, ok := h.acquireModelSlot(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx2), "m")
	if !ok {
		t.Fatal("租户8 不应受租户7 影响")
	}
	r8()
	for _, r := range rel {
		r()
	}
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("等待条件超时")
}
