package api

import (
	"fmt"
	"net/url"
	"os"

	"github.com/bobdfy/flowgate/internal/model"
)

// 本文件集中放置「写操作」的入参校验与默认值填充。
//
// ★ 为什么必须抽成一份实现：Create 和 Update 是两条独立的代码路径。
// 只要有一边漏了校验，就等价于留了一条「先合规地创建、再改成不合规」的绕过路径。
// 典型例子：CreateNode 拦了内网地址，但 UpdateNode 没拦，
// 那么 POST 一个合规地址、再 PUT 成 http://169.254.169.254 就能打到云元数据服务。
// 所以规则只有一份实现，Create 和 Update 都调用它。
//
// ★ 第二个背景：PUT 在本项目里是「全量覆盖」语义（body 里没带的字段就是零值），
// 而 Go 的零值（空串 / 0 / false）和「调用方没传」是分不出来的。
// 因此写路径必须显式校验「业务上不允许为零值的字段」，否则
// PUT {"enabled":true} 这种看起来无害的请求会把 host/path_pattern/name 全清空。

// validateNodeForWrite 校验实例的写请求，Create / Update 共用。
//
// 规则：
//   - address 必填、必须是带 host 的完整 URL、只允许 http/https、非内网
//   - weight <= 0 兜底为 1（weight=0 会让节点永远选不中，属于静默失效）
func validateNodeForWrite(node *model.Node) error {
	if err := validateNodeAddress(node.Address); err != nil {
		return err
	}
	if node.Weight <= 0 {
		node.Weight = 1
	}
	return nil
}

// validateNodeAddress 校验单个上游地址，返回可直接回给调用方的错误描述。
func validateNodeAddress(addr string) error {
	if addr == "" {
		return fmt.Errorf("address is required")
	}
	// 地址必须带 scheme（gateway 的 parseUpstream 只兜底，这里要求完整 URL）
	u, err := url.Parse(addr)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("address must be a full URL, e.g. http://localhost:8091")
	}
	// 只允许 http/https，并拦内网/回环地址，防止 SSRF
	// （本地联调可用 ALLOW_PRIVATE_UPSTREAM=1 放开）。
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("address scheme must be http or https")
	}
	if os.Getenv("ALLOW_PRIVATE_UPSTREAM") != "1" && isPrivateHost(u.Hostname()) {
		return fmt.Errorf("address points to a private/loopback host")
	}
	return nil
}

// validateTenantForWrite 校验租户的写请求并填充默认值，Create / Update 共用。
//
// 这里修掉的是一个真实缺陷：UpdateTenant 原先不做默认值填充，
// 而 Update 的 SQL 会写入 status 字段，于是 PUT 一个不带 status 的 body
// 会把租户状态写成空串 —— 鉴权侧判定 status != "active" 一律拒绝，
// 结果是「改一下租户名字，它名下所有 API Key 全部失效」。
func validateTenantForWrite(t *model.Tenant) error {
	if t.Name == "" {
		return fmt.Errorf("name is required")
	}
	if t.Status == "" {
		t.Status = "active"
	}
	if t.Status != "active" && t.Status != "disabled" {
		return fmt.Errorf("status must be active or disabled")
	}
	if t.QPSLimit <= 0 {
		t.QPSLimit = 100
	}
	return nil
}

// validateRouteForWrite 校验路由的写请求，Create / Update 共用。
//
// service_id 为什么要校验：store 层的 Update SQL 原先根本不更新 service_id
// （SET 子句里漏了这一列），改了 service_id 返回 200 但库里纹丝不动。
// 这里把它变成显式必填，配合 store 层补上的 service_id 列，语义才闭环。
func validateRouteForWrite(route *model.Route) error {
	if route.Name == "" {
		return fmt.Errorf("name is required")
	}
	if route.ServiceID <= 0 {
		return fmt.Errorf("service_id is required")
	}
	if route.PathPattern == "" {
		return fmt.Errorf("path_pattern is required")
	}
	return nil
}

// validateServiceForWrite 校验服务的写请求并填充默认值，Create / Update 共用。
func validateServiceForWrite(svc *model.Service) error {
	if svc.Name == "" {
		return fmt.Errorf("name is required")
	}
	applyServiceDefaults(svc)
	return nil
}
