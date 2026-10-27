package gateway

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bobdfy/flowgate/internal/loadbalance"
	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/overload"
)

// noNodeBalancer 永远挑不到节点，让 ServeHTTP 在读 body 之后立刻走到 503，
// 这样测请求体上限时不需要真的起一个上游。
type noNodeBalancer struct{}

func (noNodeBalancer) Pick() *loadbalance.Lease { return nil }

func newLimitTestBackend() *Backend {
	return &Backend{
		svc:   model.Service{Name: "svc-a"},
		lb:    noNodeBalancer{},
		guard: overload.NewGuard(10, overload.StrategyFailFast, 0, 0),
	}
}

// 超限必须回 413，而不是把巨型 body 整包读进内存再让上游报错。
func TestServeHTTPRejectsOversizedBody(t *testing.T) {
	body := bytes.Repeat([]byte("a"), maxProxyRequestBody+1)
	rec := httptest.NewRecorder()

	newLimitTestBackend().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body)))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超大请求体应回 413，实际 %d（body=%q）", rec.Code, rec.Body.String())
	}
	// body 没读完就回响应，这条连接不能再复用。
	if got := rec.Header().Get("Connection"); got != "close" {
		t.Fatalf("超限后应带 Connection: close，实际 %q", got)
	}
}

// 正向对照：没超限的请求体不能被误杀。
func TestServeHTTPAcceptsBodyUnderLimit(t *testing.T) {
	body := bytes.Repeat([]byte("a"), 1024)
	rec := httptest.NewRecorder()

	newLimitTestBackend().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body)))

	if rec.Code == http.StatusRequestEntityTooLarge {
		t.Fatal("未超限的请求体不该被拒")
	}
}
