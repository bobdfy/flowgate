// Package netguard 负责 SSRF 防护：判断某个目标地址是否允许网关去连接。
//
// ★ 为什么要分成「解析期」和「拨号期」两层：
// 配置期只检查一次（admin 保存节点时），但 DNS 是可变的 ——
// 检查时 example.com 解析到公网 IP，网关真正拨号时可能已经指向 127.0.0.1
// （DNS rebinding）。所以最终防线必须放在连接建立的那一刻，
// 校验的就是马上就要连的那个 IP，中间没有可乘之机。
//
// 本包不碰业务，只做「IP 是否允许」这一件事，admin 和 gateway 共用同一份规则。
package netguard

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

// extraBlocked 是标准库 IsPrivate 没覆盖、但同样不能拨的网段。

var extraBlocked = []*net.IPNet{
	mustCIDR("0.0.0.0/8"),     // IsUnspecified 只认 0.0.0.0 本身，0.0.0.1 之类的同样不可路由
	mustCIDR("100.64.0.0/10"), // 运营商级 NAT（CGNAT），部分云厂商拿它当内部网络。
	mustCIDR("64:ff9b::/96"),  // NAT64，可把内网 IPv4 映射成 IPv6 地址
}

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic("netguard: 非法内置网段 " + s)
	}
	return n
}

// Blocked 判断单个 IP 是否属于禁止拨号的范围。nil 视为禁止。
func Blocked(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// 统一成 4 字节表示，后面的网段比较不必关心 IP 是 4 字节还是 16 字节存的。
	// 标准库的 IsPrivate / IsLoopback 内部也会做同样的转换。
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}

	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}

	for _, n := range extraBlocked {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Policy 是「默认禁止内网 + 显式白名单」的组合。
// 白名单用于本地联调 / 内网部署：把确实需要访问的内网网段写进去，
// 而不是用一个开关把整类地址全部放开。
type Policy struct {
	allowed []*net.IPNet
}

// NewPolicy 从 CIDR 列表构造策略，也接受不带掩码的单个 IP。
// 空列表表示不放行任何内网地址（生产默认）。
func NewPolicy(cidrs []string) (*Policy, error) {
	p := &Policy{}
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		// 允许写成单个 IP，省得每一处都补 /32。
		if !strings.Contains(raw, "/") {
			ip := net.ParseIP(raw)
			if ip == nil {
				return nil, fmt.Errorf("非法 IP/CIDR: %q", raw)
			}
			bits := 128
			if v4 := ip.To4(); v4 != nil {
				ip = v4
				bits = 32
			}
			p.allowed = append(p.allowed, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}

		ip, n, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, fmt.Errorf("非法 CIDR %q: %w", raw, err)
		}
		// 统一用网络地址，避免 10.0.0.5/8 这种写法带来的歧义。
		n.IP = ip.Mask(n.Mask)
		p.allowed = append(p.allowed, n)
	}
	return p, nil
}

// FromEnv 从环境变量读逗号分隔的 CIDR 列表构造策略。变量为空 → 默认拒绝内网。
func FromEnv(name string) (*Policy, error) {
	return NewPolicy(strings.Split(os.Getenv(name), ","))
}

// Allowed 返回白名单网段（只读，用于启动日志）。
func (p *Policy) Allowed() []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.allowed))
	for _, n := range p.allowed {
		out = append(out, n.String())
	}
	return out
}

// Blocked 判断 IP 是否被拒绝：白名单命中优先放行，其余走默认规则。
func (p *Policy) Blocked(ip net.IP) bool {
	if p != nil && ip != nil {
		cand := ip
		if v4 := cand.To4(); v4 != nil {
			cand = v4
		}
		for _, n := range p.allowed {
			if n.Contains(cand) {
				return false
			}
		}
	}
	return Blocked(ip)
}

// CheckHost 解析域名并校验**所有**解析结果。
//
// 必须校验全部而不是任意一个：攻击者可以让域名同时返回一个公网 IP 和一个内网 IP，
// 只检查首个结果就可能放过内网那个。解析失败按拒绝处理。
func (p *Policy) CheckHost(ctx context.Context, host string) error {
	if host == "" {
		return fmt.Errorf("目标地址为空")
	}
	if ip := net.ParseIP(host); ip != nil {
		if p.Blocked(ip) {
			return fmt.Errorf("目标是内网地址: %s", ip)
		}
		return nil
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		// 解析不出来就拒绝：宁可误伤，也不放过无法确认的目标。
		return fmt.Errorf("无法解析 %s: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("无法解析 %s: 没有 A/AAAA 记录", host)
	}
	for _, a := range ips {
		if p.Blocked(a.IP) {
			return fmt.Errorf("%s 解析到内网地址 %s", host, a.IP)
		}
	}
	return nil
}

// DialControl 供 net.Dialer.Control 使用：在 TCP 连接建立之前拿到最终 IP 并校验。
//
// 这是 SSRF 的最后一道防线 —— DNS rebinding 在这里失效，因为
// Control 拿到的地址就是即将连接的地址，不存在「检查完再解析」的窗口。
func (p *Policy) DialControl(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("netguard: 非法拨号地址 %q: %w", address, err)
	}
	// IPv6 可能带 zone（fe80::1%eth0），去掉再解析。
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("netguard: 拨号地址不是 IP: %q", address)
	}
	if p.Blocked(ip) {
		return fmt.Errorf("netguard: 拒绝连接内网地址 %s", ip)
	}
	return nil
}

// Dialer 返回带 SSRF 校验的拨号器，直接赋给 http.Transport.DialContext 即可。
func (p *Policy) Dialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{
		Timeout: timeout,
		Control: p.DialControl,
	}
}
