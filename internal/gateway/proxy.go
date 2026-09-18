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

// 第 1 段：这个函数做什么
//   —— 把入站请求改造后发一次给上游，返回上游响应。★ 只发一次，不做重试。

// 第 2 段：为什么不用 httputil.ReverseProxy
//   —— ReverseProxy 拿到响应就立刻 stream 给客户端，写了就没法重试。
//      这里只做 RoundTrip，把「响应输出」留给调用方，
//      这样「重试决策」和「响应输出」才能解耦。

// 第 3 段：★ body 的所有权（最该写清的一点）
//   —— 本函数不读 resp.Body。读它、丢弃它、关闭它的责任都在调用方。
//      另外调用方必须在每轮重试前重新包 req.Body（bytes.NewReader），
//      因为 RoundTrip 会把 req.Body 读空，第二次发就是空体。
//      这两件事都在 Backend.ServeHTTP 里做。

// 第 4 段：对请求做了哪些改造
//   —— Clone、清 RequestURI、改写 Scheme/Host、删逐跳头。
//      ★ 注意 Host 同时改了 outReq.Host 和 outReq.URL.Host ——
//        只改一个会让上游收到错的 Host 头（影响虚拟主机路由）。

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

	return resp, nil
}

// removeByHopHeaders 删除请求头中逐跳（hop-by-hop）的字段，防止它们被代理转发到上游。
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
