package api

import (
	"testing"

	"github.com/bobdfy/flowgate/internal/model"
)

// 本文件是 internal/api 包的第一批测试，覆盖集中在 validate.go 的纯校验函数上。
//
// 为什么只测这些：Handler 直接持有 *postgres.XXXStore 具体类型（不是接口），
// 没有 DB 就无法端到端跑 handler。所以把「规则」抽成无副作用的纯函数再测 ——
// 这也是 validate.go 存在的第二个理由：让写路径的规则可以被单元测试覆盖。
//
// 这批用例本质上是回归测试：每一条都对着一个已经修掉的真实缺陷。

func TestValidateNodeForWrite(t *testing.T) {
	// 这条用例只关心「地址形状」，所以把整个 IP 空间放进白名单。
	// 内网拦截本身由下面的 TestValidateNodeAddress_Private 单独覆盖。
	// 地址一律用 IP 字面量：域名要真解析，会让用例依赖网络环境。
	t.Setenv("ALLOW_UPSTREAM_CIDRS", "0.0.0.0/0,::/0")

	tests := []struct {
		name    string
		node    model.Node
		wantErr bool
		hint    string
	}{
		{"正常 http 地址", model.Node{Address: "http://127.0.0.1:8080", Weight: 1}, false, ""},
		{"正常 https 地址", model.Node{Address: "https://8.8.8.8", Weight: 1}, false, ""},
		{"地址为空会被拒", model.Node{Address: "", Weight: 1}, true, "PUT 不带 body 时零值不能静默入库"},
		{"缺少 host 会被拒", model.Node{Address: "http://", Weight: 1}, true, ""},
		{"相对路径会被拒", model.Node{Address: "upstream:8080", Weight: 1}, true, "没有 scheme"},
		{"非 http scheme 会被拒", model.Node{Address: "file:///etc/passwd", Weight: 1}, true, ""},
		{"非法 URL 会被拒", model.Node{Address: "http://%zz", Weight: 1}, true, "url.Parse 直接报错"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := tt.node
			err := validateNodeForWrite(&n)
			if tt.wantErr && err == nil {
				t.Fatalf("期望报错但没有：%+v", n)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("期望通过但报错：%v", err)
			}
		})
	}
}

// TestValidateNodeForWrite_WeightFallback 兜住一个静默失效：
// weight=0 的节点在加权轮询里永远选不中，但 admin 上看起来一切正常（enabled=true）。
// 与其让用户排查"为什么这条节点没有流量"，不如在写路径直接归一化成 1。
func TestValidateNodeForWrite_WeightFallback(t *testing.T) {
	t.Setenv("ALLOW_UPSTREAM_CIDRS", "127.0.0.0/8")

	cases := []struct {
		in   int
		want int
	}{
		{0, 1},   // PUT body 里没带 weight
		{-5, 1},  // 手滑写成负数
		{3, 3},   // 正常值保持不动
		{10, 10}, // 正常值保持不动
	}
	for _, c := range cases {
		n := model.Node{Address: "http://127.0.0.1:8080", Weight: c.in}
		if err := validateNodeForWrite(&n); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n.Weight != c.want {
			t.Errorf("weight=%d → 得到 %d，期望 %d", c.in, n.Weight, c.want)
		}
	}
}

// TestValidateNodeAddress_Private 覆盖 SSRF 防护本身。
// ★ 关键点：这条规则 Create 和 Update 必须都生效 —— Update 漏校验曾经是可以绕过的。
func TestValidateNodeAddress_Private(t *testing.T) {
	// 白名单为空 = 拦截所有内网地址（生产默认）
	t.Setenv("ALLOW_UPSTREAM_CIDRS", "")

	privateAddrs := []string{
		"http://127.0.0.1:8080",        // 回环
		"http://localhost:8091",        // 回环（本机默认 mock 上游）
		"http://169.254.169.254",       // 云元数据服务，最典型的 SSRF 目标
		"http://10.0.0.5:9000",         // 私有网段
		"http://192.168.1.1",           // 私有网段
		"http://172.16.0.1",            // 私有网段
		"http://[::1]:8080",            // IPv6 回环
		"http://does-not-resolve.test", // 解析失败按私有处理：宁可拒绝
	}
	for _, addr := range privateAddrs {
		if err := validateNodeAddress(addr); err == nil {
			t.Errorf("内网地址 %s 应当被拒绝，但通过了校验", addr)
		}
	}

	// 显式白名单内的地址必须能配上，否则本地联调没法做。
	// 这里同时覆盖了 ::1 和 127.0.0.1：localhost 在双栈机器上会解析出两个地址，
	// 只放行其中一个依然会被拒——校验是「全部解析结果都允许才通过」。
	t.Setenv("ALLOW_UPSTREAM_CIDRS", "127.0.0.0/8,::1/128")
	if err := validateNodeAddress("http://localhost:8091"); err != nil {
		t.Errorf("白名单内的本地地址应当放行，但报错：%v", err)
	}

	// 白名单写错时必须报错而不是静默放行。
	t.Setenv("ALLOW_UPSTREAM_CIDRS", "10.0.0.0/33")
	if err := validateNodeAddress("http://8.8.8.8"); err == nil {
		t.Error("非法 CIDR 应当让校验失败")
	}
}

// TestValidateTenantForWrite 是 UpdateTenant 清空 status 那个缺陷的回归用例。
// 原缺陷：PUT 不带 status → status 写成空串 → 鉴权侧不认 → 名下所有 API Key 失效。
func TestValidateTenantForWrite(t *testing.T) {
	t.Run("不带 status 补默认值 active", func(t *testing.T) {
		tn := model.Tenant{Name: "acme", QPSLimit: 50}
		if err := validateTenantForWrite(&tn); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tn.Status != "active" {
			t.Fatalf("status 被填成 %q，期望 active（空串会让该租户所有 API Key 失效）", tn.Status)
		}
		if tn.QPSLimit != 50 {
			t.Errorf("显式指定的 QPSLimit=50 被覆盖成了 %d", tn.QPSLimit)
		}
	})

	t.Run("显式 disabled 保持不动", func(t *testing.T) {
		tn := model.Tenant{Name: "acme", Status: "disabled"}
		if err := validateTenantForWrite(&tn); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tn.Status != "disabled" {
			t.Fatalf("status=%q，期望保持 disabled（默认值不能反过来覆盖显式值）", tn.Status)
		}
	})

	t.Run("非法状态被拒", func(t *testing.T) {
		tn := model.Tenant{Name: "acme", Status: "ACTIVE"}
		if err := validateTenantForWrite(&tn); err == nil {
			t.Fatal("大写 ACTIVE 应当被拒：鉴权只认小写 active，写成别的值等于把租户锁死")
		}
	})

	t.Run("PUT 不带 name 会被拒", func(t *testing.T) {
		// 对应 validation 里第二个全量覆盖陷阱：
		// PUT {"status":"active"} 会把租户名清空，admin 列表里变成无名记录
		tn := model.Tenant{Status: "active"}
		if err := validateTenantForWrite(&tn); err == nil {
			t.Fatal("空 name 应当被拒")
		}
	})

	t.Run("QPSLimit 兜底", func(t *testing.T) {
		tn := model.Tenant{Name: "acme", QPSLimit: 0}
		if err := validateTenantForWrite(&tn); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// QPSLimit=0 会让令牌桶容量变 0，等于无限流地拒绝所有请求
		if tn.QPSLimit != 100 {
			t.Errorf("QPSLimit 期望兜底 100，得到 %d", tn.QPSLimit)
		}
	})
}

// TestValidateRouteForWrite 覆盖 PUT 全量覆盖造成的两个陷阱，
// 以及 store 层 SET 子句漏 service_id 后的语义对齐（service_id 现在显式必填）。
func TestValidateRouteForWrite(t *testing.T) {
	tests := []struct {
		name    string
		route   model.Route
		wantErr string
	}{
		{
			name:    "缺 name",
			route:   model.Route{ServiceID: 1, PathPattern: "/"},
			wantErr: "name",
		},
		{
			name:    "缺 service_id",
			route:   model.Route{Name: "root", PathPattern: "/"},
			wantErr: "service_id",
		},
		{
			name:    "缺 path_pattern",
			route:   model.Route{Name: "root", ServiceID: 1},
			wantErr: "path_pattern",
		},
		{
			name:  "合法路由",
			route: model.Route{Name: "root", ServiceID: 1, PathPattern: "/", PathMatchType: "prefix"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.route
			err := validateRouteForWrite(&r)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("期望通过但报错：%v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("应当报错（关键词 %q），但通过了", tt.wantErr)
			}
			if !contains(err.Error(), tt.wantErr) {
				t.Errorf("错误信息 %q 里应当包含 %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestValidateServiceForWrite 兼顾两件事：name 必填、治理参数零值兜底。
// applyServiceDefaults 的语义是"0 表示用默认值"，所以 Update 也必须走，
// 否则一次 PUT 就能把 cb_failure_threshold 写成 0（= 第一次失败就熔断）。
func TestValidateServiceForWrite(t *testing.T) {
	t.Run("空 name 被拒", func(t *testing.T) {
		svc := model.Service{}
		if err := validateServiceForWrite(&svc); err == nil {
			t.Fatal("PUT 不带 name 会把服务名写空，应当被拒")
		}
	})

	t.Run("零值治理参数被兜底", func(t *testing.T) {
		svc := model.Service{Name: "mock-svc"}
		if err := validateServiceForWrite(&svc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if svc.CBFailureThreshold != 5 {
			t.Errorf("CBFailureThreshold 期望兜底 5，得到 %d", svc.CBFailureThreshold)
		}
		if svc.CBCooldownMs != 10000 {
			t.Errorf("CBCooldownMs 期望兜底 10000，得到 %d", svc.CBCooldownMs)
		}
		if svc.CBHalfOpenLimit != 1 {
			t.Errorf("CBHalfOpenLimit 期望兜底 1，得到 %d", svc.CBHalfOpenLimit)
		}
		if svc.ResponseHeaderTimeoutMs != 5000 {
			t.Errorf("ResponseHeaderTimeoutMs 期望兜底 5000，得到 %d", svc.ResponseHeaderTimeoutMs)
		}
		if svc.RetryBackoffMs != 100 {
			t.Errorf("RetryBackoffMs 期望兜底 100，得到 %d", svc.RetryBackoffMs)
		}
	})

	t.Run("显式值不被覆盖", func(t *testing.T) {
		// max_retries=0（不重试）和 retry_on_status="" 是合法值，不能填默认
		svc := model.Service{Name: "svc", MaxRetries: 0, RetryOnStatus: "", CBFailureThreshold: 10}
		if err := validateServiceForWrite(&svc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if svc.CBFailureThreshold != 10 {
			t.Errorf("显式指定的 CBFailureThreshold=10 被覆盖成 %d", svc.CBFailureThreshold)
		}
		if svc.MaxRetries != 0 {
			t.Errorf("MaxRetries=0 是合法值（不重试），被改成了 %d", svc.MaxRetries)
		}
	})
}

// contains 用标准库手写包含判断，避免为此引入 strings 之外的东西。
func contains(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
