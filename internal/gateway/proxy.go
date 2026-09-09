// Package gateway 提供网关数据面的核心逻辑：反向代理构造、路由表构建与请求分发。
package gateway

import (
	"fmt"
	"net"
	"net/http"
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

// ProxyCache 按"上游地址 + 超时配置"缓存 Tranport，刷新时复用，避免反复新建 Transport。(缓存)
type ProxyCache struct {
	mu sync.Mutex
	m  map[string]*http.Transport
}

// NewProxyCache 创建一个空的 ProxyCache。
func NewProxyCache() *ProxyCache {
	return &ProxyCache{m: map[string]*http.Transport{}}
}

// 构建带超时的Tranport
func newTransport(svc model.Service) *http.Transport {
	connect := time.Duration(svc.ConnectTimeoutMs) * time.Millisecond
	if connect <= 0 {
		connect = 3 * time.Second
	}

	return &http.Transport{
		DialContext: (&net.Dialer{Timeout: connect}).DialContext, // 建连超时(TCP 三次握手)
		// 空闲连接保活
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: time.Duration(svc.ResponseHeaderTimeoutMs) * time.Millisecond,
	}

}

// 按 key(`地址|connect_timeout`)查缓存，命中返回，未命中调用 `newTransport` 构造并缓存。
func (c *ProxyCache) get(u *url.URL, svc model.Service) *http.Transport {
	key := fmt.Sprintf("%s|%d|%d", u.String(), svc.ConnectTimeoutMs, svc.ResponseHeaderTimeoutMs)
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.m[key]; ok {
		return t
	}
	t := newTransport(svc)
	c.m[key] = t
	return t
}

// 执行一次上游请求, 返回完整的响应
// 调用方拿到 (resp, body, err) 后自行决定是否重试、何时 flush 给客户端。
//
// 为什么不直接用 ReverseProxy.ServeHTTP:
//
//	ReverseProxy 拿到响应后立刻流式写给客户端，写了就没法重试。
//	这里先 RoundTrip + 缓冲 body，把「重试决策」和「响应输出」解耦
func forwardOnce(transport *http.Transport, req *http.Request, target *url.URL) (*http.Response, error) {
	outReq := req.Clone(req.Context())

	outReq.RequestURI = ""

	outReq.URL.Scheme = target.Scheme
	outReq.URL.Host = target.Host
	outReq.Host = target.Host

	removeByHopHeaders(outReq.Header)

	resp, err := transport.RoundTrip(outReq)
	if err != nil {
		return nil, err
	}

	// 缓冲 body, 重试决策需要先拿到完整响应
	// body, err := io.ReadAll(resp.Body)
	// if err != nil {
	// 	return nil, nil, err
	// }
	// resp.Body.Close()

	return resp, nil
}

func removeByHopHeaders(h http.Header) {
	// 先处理 Connection 头里点名的 header（这些也是 hop-by-hop）
	for _, f := range h["Connection"] {
		for sf := range strings.SplitSeq(f, ",") {
			if sf = strings.TrimSpace(sf); sf != "" {
				h.Del(sf)
			}
		}
	}

	hopHeaders := []string{
		"Connection",
		"Proxy-Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"TE",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	}
	for _, key := range hopHeaders {
		h.Del(key)
	}
}

// copyHeader 把 src 的 header 拷到 dst（用 Add 追加，不覆盖 dst 已有的同名字段）。
func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
