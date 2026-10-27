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
	"github.com/bobdfy/flowgate/internal/netguard"
)

type transportEntry struct {
	transport *http.Transport
	lastUsed  time.Time
}

// parseUpstream 解析上游地址，不带 scheme 时自动补 http://。
func parseUpstream(addr string) (*url.URL, error) {
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("非法上游地址: %q", addr)
	}
	// S2：数据面第二道防线，只允许 http/https。
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("非法上游 scheme: %q", u.Scheme)
	}
	return u, nil
}

// ProxyCache 按"上游地址 + 超时配置"缓存 Tranport，刷新时复用，避免反复新建 Transport。(缓存)
type ProxyCache struct {
	mu     sync.Mutex
	m      map[string]*transportEntry
	policy *netguard.Policy
}

// NewProxyCache 创建一个空的 ProxyCache。policy 为 nil 时默认拒绝所有内网地址。
func NewProxyCache(policy *netguard.Policy) *ProxyCache {
	return &ProxyCache{m: map[string]*transportEntry{}, policy: policy}
}

// 构建带超时的Tranport
func newTransport(svc model.Service, policy *netguard.Policy) *http.Transport {
	connect := time.Duration(svc.ConnectTimeoutMs) * time.Millisecond
	if connect <= 0 {
		connect = 3 * time.Second
	}

	// 连接池上限必须显式设置，且 ≥ 服务的 max_concurrency：
	// 否则并发一超上限，多出来的请求会被临时拨号、用完即关，
	// 产生连接抖动（连续压测逐轮劣化的根因）。
	// max_concurrency=0（不限流）时给一个兜底上限。
	maxPerHost := svc.MaxConcurrency
	if maxPerHost <= 0 {
		maxPerHost = 512
	}

	return &http.Transport{
		// 建连超时(TCP 三次握手)；DialControl 在解析之后、连接之前校验目标 IP，
		// 配置期的 DNS 检查被绕过（DNS rebinding）时由这里兜底。
		DialContext: policy.Dialer(connect).DialContext,

		// 空闲连接保活
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: time.Duration(svc.ResponseHeaderTimeoutMs) * time.Millisecond,

		MaxIdleConns:        maxPerHost,
		MaxIdleConnsPerHost: maxPerHost,

		MaxConnsPerHost: maxPerHost,
	}

}

// // 按 key(`地址|connect_timeout|responseHeader_timeout`)查缓存，命中返回，未命中调用 `newTransport` 构造并缓存
func transportKey(u *url.URL, svc model.Service) string {
	return fmt.Sprintf("%s|%d|%d|%d", u.String(),
		svc.ConnectTimeoutMs, svc.ResponseHeaderTimeoutMs, svc.MaxConcurrency)
}

func (c *ProxyCache) get(u *url.URL, svc model.Service) *http.Transport {
	key := transportKey(u, svc)
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[key]; ok {
		e.lastUsed = time.Now()
		return e.transport
	}
	t := newTransport(svc, c.policy)
	c.m[key] = &transportEntry{transport: t, lastUsed: time.Now()}
	return t
}

// 把入站请求改造后发一次给上游，返回上游响应。 只发一次，不做重试。
func forwardOnce(transport *http.Transport, req *http.Request, target *url.URL) (*http.Response, error) {
	outReq := req.Clone(req.Context())

	outReq.RequestURI = ""

	outReq.URL.Scheme = target.Scheme
	outReq.URL.Host = target.Host
	outReq.Host = target.Host

	removeByHopHeaders(outReq.Header)

	// 网关的鉴权凭据只对网关这一跳有效，绝不能流到上游。
	// 上游需要认证时由服务配置注入独立凭据
	outReq.Header.Del("Authorization")
	outReq.Header.Del("X-API-Key")

	// 覆盖客户端伪造的 X-Forwarded-For / X-Real-IP，统一由网关填真实来源。
	if ip, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		outReq.Header.Del("X-Forwarded-For")
		outReq.Header.Del("X-Real-IP")
		outReq.Header.Set("X-Forwarded-For", ip)
	}

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

// Sweep 回收超过 idleFor 未被使用的Transport
func (c *ProxyCache) Sweep(idleFor time.Duration) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-idleFor)
	n := 0
	for k, e := range c.m {
		if e.lastUsed.Before(cutoff) {
			e.transport.CloseIdleConnections()
			delete(c.m, k)
			n++
		}
	}
	return n
}
