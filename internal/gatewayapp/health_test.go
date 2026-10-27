package gatewayapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// 探针的语义一旦弄反，后果是相反的：
//   - /healthz 写得太严格（比如顺手查了 Redis）→ 依赖抖一下就被 K8s 重启
//   - /readyz 写得太宽松（比如永远 200）→ 关闭中的实例还在接流量，用户看到 502
//
// 所以这里把「什么时候是什么状态」逐条钉死。

func TestStateReadyLifecycle(t *testing.T) {
	s := NewState()

	if s.Ready() {
		t.Fatal("刚创建、还没初始化完成，不应就绪")
	}

	s.MarkReady()
	if !s.Ready() {
		t.Fatal("MarkReady 之后应当就绪")
	}

	s.MarkDraining()
	if s.Ready() {
		t.Fatal("MarkDraining 之后不应再就绪")
	}

	// Ready 已置位的情况下再 MarkDraining，仍然必须是不就绪。
	// 这条防的是「draining 判断被写成 || 或写反」。
	s2 := NewState()
	s2.MarkReady()
	s2.MarkDraining()
	if s2.Ready() {
		t.Fatal("ready && draining 必须判为不就绪")
	}
}

// /healthz 只回答「进程还活着吗」。无论初始化完成与否、是否在关闭中，都是 200。
// 如果这里返回 503，K8s 会把一个正在正常收尾的进程当成死掉直接 SIGKILL。
func TestHealthzAlwaysOK(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*State)
	}{
		{"刚启动未就绪", func(s *State) {}},
		{"正常服务中", func(s *State) { s.MarkReady() }},
		{"关闭中", func(s *State) { s.MarkReady(); s.MarkDraining() }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewState()
			tc.setup(s)

			rec := httptest.NewRecorder()
			s.Healthz(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("healthz 必须永远 200，实际 %d", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type 应为 application/json，实际 %q", got)
			}
			if body := strings.TrimSpace(rec.Body.String()); body != `{"status":"ok"}` {
				t.Fatalf("body 异常: %q", body)
			}
		})
	}
}

func TestReadyzStatusTransition(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(*State)
		wantCode  int
		wantBody  string
	}{
		{"未初始化完成", func(s *State) {}, http.StatusServiceUnavailable, `{"status":"draining"}`},
		{"正常服务中", func(s *State) { s.MarkReady() }, http.StatusOK, `{"status":"ok"}`},
		{"关闭中", func(s *State) { s.MarkReady(); s.MarkDraining() }, http.StatusServiceUnavailable, `{"status":"draining"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewState()
			tc.setup(s)

			rec := httptest.NewRecorder()
			s.Readyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			if rec.Code != tc.wantCode {
				t.Fatalf("状态码应为 %d，实际 %d", tc.wantCode, rec.Code)
			}
			if body := strings.TrimSpace(rec.Body.String()); body != tc.wantBody {
				t.Fatalf("body 应为 %q，实际 %q", tc.wantBody, body)
			}
		})
	}
}

// 读状态发生在每个探针请求的 goroutine 里，写状态发生在信号 goroutine 里。
// 用错了锁类型（比如普通 bool）在 -race 下会直接报数据竞争。
func TestStateConcurrentReadWrite(t *testing.T) {
	s := NewState()
	s.MarkReady()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				rec := httptest.NewRecorder()
				s.Readyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.MarkDraining()
	}()

	wg.Wait()

	if s.Ready() {
		t.Fatal("并发写入后应处于未就绪状态")
	}
}
