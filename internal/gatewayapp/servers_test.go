package gatewayapp

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// 这组测试守的是一个具体的坑：pprof 曾把地址写死成 127.0.0.1:6060，
// 容器里绑回环 → 宿主机端口映射永远打不通，-pprof 开了等于没开。
// 现在地址由 -pprof-addr 传进来，这里验证「传入哪个地址就监听哪个」。

// freeAddr 借一个系统分配的端口再用：先 Listen 拿到端口号，立刻关掉，
// 把地址交给 StartPProf。中间存在极小的被抢占窗口，所以下面的探测有重试。
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("取空闲端口失败: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// waitForPProf 轮询直到端点可访问，最多等 2 秒。
func waitForPProf(t *testing.T, addr string) *http.Response {
	t.Helper()
	url := fmt.Sprintf("http://%s/debug/pprof/", addr)
	var lastErr error
	for i := 0; i < 100; i++ {
		resp, err := http.Get(url)
		if err == nil {
			return resp
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pprof 端点 %s 不可访问: %v", addr, lastErr)
	return nil
}

// TestStartPProfListensOnGivenAddr 传入的地址必须真正生效，而不是仍绑默认端口。
func TestStartPProfListensOnGivenAddr(t *testing.T) {
	addr := freeAddr(t)
	StartPProf(addr)

	resp := waitForPProf(t, addr)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /debug/pprof/ 应为 200，实际 %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读响应失败: %v", err)
	}
	// 索引页会列出 goroutine / heap / profile 等条目，用来确认挂的是真 pprof
	for _, want := range []string{"goroutine", "heap", "profile"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("pprof 索引页缺少条目 %q", want)
		}
	}
}

// TestStartPProfServesGoroutineDump 排障最常用的一项：goroutine 实时堆栈。
// 它不是默认 mux 的空白页，需要 net/http/pprof 的注册确实生效。
func TestStartPProfServesGoroutineDump(t *testing.T) {
	addr := freeAddr(t)
	StartPProf(addr)

	waitForPProf(t, addr).Body.Close()

	resp, err := http.Get(fmt.Sprintf("http://%s/debug/pprof/goroutine?debug=1", addr))
	if err != nil {
		t.Fatalf("请求 goroutine 堆栈失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("goroutine 端点应为 200，实际 %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "goroutine") {
		t.Fatalf("goroutine 输出不像堆栈转储: %.120s", body)
	}
}
