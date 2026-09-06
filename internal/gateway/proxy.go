// Package gateway 提供网关数据面的核心逻辑：反向代理构造、路由表构建与请求分发。
package gateway

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bobdfy/flowgate/internal/model"
)

// parseUpstream 解析上游地址，不带 scheme 时自动补 http://。
func parseUpstream(addr string) (*url.URL, error) {
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("非法上游地址: %q", addr)
	}
	return u, nil
}

// ProxyCache 按"上游地址 + 超时配置"缓存 proxy，刷新时复用，避免反复新建 Transport。(缓存)
type ProxyCache struct {
	mu sync.Mutex
	m  map[string]*httputil.ReverseProxy
}

// NewProxyCache 创建一个空的 ProxyCache。
func NewProxyCache() *ProxyCache {
	return &ProxyCache{m: map[string]*httputil.ReverseProxy{}}
}

// get 返回与"地址 + 超时"对应的 proxy，不存在则新建并缓存。
func (c *ProxyCache) get(u *url.URL, svc model.Service) *httputil.ReverseProxy {
	key := fmt.Sprintf("%s|%d|%d", u.String(), svc.ConnectTimeoutMs, svc.RequestTimeoutMs)
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.m[key]; ok {
		return p
	}
	p := newProxy(u, svc)
	c.m[key] = p
	return p
}

// newProxy 为一个上游地址构造反向代理。
// 它封装了"单次转发"所需的连接与超时控制，后续版本的重试/熔断将在此之外包一层治理逻辑。
func newProxy(target *url.URL, svc model.Service) *httputil.ReverseProxy {
	connect := time.Duration(svc.ConnectTimeoutMs) * time.Millisecond
	if connect <= 0 {
		connect = 3 * time.Second
	}

	respHeader := time.Duration(svc.RequestTimeoutMs) * time.Millisecond
	if respHeader <= 0 {
		respHeader = 10 * time.Second
	}

	// 底层连接与超时控制
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: connect}).DialContext, // 建连超时
		ResponseHeaderTimeout: respHeader,                                  // 等待响应头超时
		IdleConnTimeout:       90 * time.Second,
	}

	return &httputil.ReverseProxy{
		// Director 修改请求目标：把请求重定向到上游地址
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
		},
		Transport: transport,
		// 上游出错（连接失败、超时等）时的兜底处理
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("代理错误: %v", err)
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
		},
	}
}
