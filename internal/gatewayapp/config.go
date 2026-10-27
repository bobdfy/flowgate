package gatewayapp

// 网关进程的命令行参数：定义和校验都在这里，cmd/gateway/main.go 只负责调用。

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bobdfy/flowgate/internal/netguard"
)

// Config 是网关的启动参数。
type Config struct {
	Addr         string        // 主服务监听地址
	AIEnabled    bool          // 是否启用 AI Gateway(/v1/* 路径)
	AIConfigPath string        // AI 配置文件路径；文件不存在 → AI 不启用(不报错)
	Burst        float64       // 令牌桶容量，允许的最大突发请求数
	FailMode     string        // Redis 故障时的策略: closed / fallback / open
	TPMFailMode  string        // TPM在redis故障时的策略： closed/open/fallback
	AuthCacheTTL time.Duration // 鉴权结果进程内缓存 TTL（>0 启用；0 关闭）
	TPMWindow    time.Duration // TPM 配额窗口长度（如 1m / 5m / 1h）
	PProf        bool          // 启用 pprof 调试端点（固定 127.0.0.1:6060）
	MetricsAddr  string        // Prometheus 指标监听地址（默认仅本机）

	AllowUpstreamCIDRs string // 允许拨号的内网网段（逗号分隔）。默认为空等于「内网上游全部封死」，本地联调时必须显式写进来。

	MaxInflight int // 实例级在途请求上限

	AIConnectTimeout        time.Duration
	AIResponseHeaderTimeout time.Duration
	AIBufferedTimeout       time.Duration
	AIStreamIdleTimeout     time.Duration
	AIStreamMaxDuration     time.Duration
}

// ParseFlags 定义并解析命令行参数。只能在 main 里调用一次。
func ParseFlags() *Config {
	c := &Config{}

	flag.StringVar(&c.Addr, "addr", ":8090", "监听地址")

	flag.BoolVar(&c.AIEnabled, "ai", true, "是否启用 AI Gateway(/v1/* 路径)")

	flag.StringVar(&c.AIConfigPath, "ai-config", "ai.yaml", "AI Gateway 配置文件路径")

	flag.Float64Var(&c.Burst, "burst", 200, "令牌桶容量，允许的最大突发请求数")

	flag.StringVar(&c.FailMode, "fail-mode", "fallback", "Redis 故障时的策略:closed / fallback / open")

	flag.DurationVar(&c.AuthCacheTTL, "auth-cache-ttl", 30*time.Second, "鉴权结果进程内缓存 TTL（>0 启用；0 关闭）")

	flag.DurationVar(&c.TPMWindow, "tpm-window", time.Minute, "TPM 配额窗口长度（如 1m / 5m / 1h）")

	flag.BoolVar(&c.PProf, "pprof", false, "启用 pprof 调试端点(默认关闭, 仅 127.0.0.1)")

	flag.StringVar(&c.MetricsAddr, "metrics-addr", "127.0.0.1:9090", "Prometheus 指标监听地址（默认仅本机）")

	flag.StringVar(&c.TPMFailMode, "tpm-fail-mode", "fallback", "TPM 在 redis 故障时的策略: closed/fallback/open")

	// 默认值取自环境变量，这样容器只配一份 ALLOW_UPSTREAM_CIDRS 即可
	// （admin 走配置期校验，gateway 走拨号期校验，两边规则要一致）。
	flag.StringVar(&c.AllowUpstreamCIDRs, "allow-upstream-cidrs", os.Getenv("ALLOW_UPSTREAM_CIDRS"),
		"允许访问的内网网段，逗号分隔（如 172.16.0.0/12,127.0.0.1/32）；默认空=禁止所有内网地址")

	flag.IntVar(&c.MaxInflight, "max-inflight", 1000, "实例级在途请求上限（0 表示不限制，仅用于压测对照）")

	flag.DurationVar(&c.AIConnectTimeout, "ai-connect-timeout", 5*time.Second, "必须要大于0")

	flag.DurationVar(&c.AIResponseHeaderTimeout, "ai-response-header-timeout", 60*time.Second, "必须要大于0")

	flag.DurationVar(&c.AIBufferedTimeout, "ai-buffered-timeout", 60*time.Second, "必须要大于0")

	flag.DurationVar(&c.AIStreamIdleTimeout, "ai-stream-idle-timeout", 60*time.Second, "必须要大于0")

	flag.DurationVar(&c.AIStreamMaxDuration, "ai-stream-max-duration", 0, ">= 0")

	flag.Parse()
	return c
}

// Validate 拒绝明显不合法的参数组合，让进程在启动阶段就失败，而不是运行中行为诡异。
func (c *Config) Validate() error {
	if c.Burst <= 0 {
		return fmt.Errorf("burst 必须大于 0，当前 %v", c.Burst)
	}
	if c.TPMWindow < time.Second {
		return fmt.Errorf("tpm-window 不得小于 1s，当前 %s", c.TPMWindow)
	}

	switch c.TPMFailMode {
	case "closed", "fallback", "open":
	default:
		return fmt.Errorf("tpm-fail-mode 只能是 closed / fallback / open，当前 %q", c.TPMFailMode)
	}

	if c.MaxInflight < 0 {
		return fmt.Errorf("max-inflight 不能为负，当前 %d", c.MaxInflight)
	}

	// 白名单写错必须启动即失败：静默忽略会让线上「看起来配了但实际全拦」。
	if _, err := c.UpstreamPolicy(); err != nil {
		return fmt.Errorf("allow-upstream-cidrs 非法: %w", err)
	}

	// AI 分层超时：0 或负数会让对应的保护静默失效
	// （ResponseHeaderTimeout=0 → 无限等头；Client.Timeout=0 → 非流式可被慢滴流永久占住；
	//   StreamIdle<=0 → 不建计时器，读空闲保护直接消失）。
	// 这类错误必须启动阶段就报出来，而不是等线上连接堆积才发现。
	for _, it := range []struct {
		name string
		val  time.Duration
	}{
		{"ai-connect-timeout", c.AIConnectTimeout},
		{"ai-response-header-timeout", c.AIResponseHeaderTimeout},
		{"ai-buffered-timeout", c.AIBufferedTimeout},
		{"ai-stream-idle-timeout", c.AIStreamIdleTimeout},
	} {
		if it.val <= 0 {
			return fmt.Errorf("%s 必须大于 0，当前 %s", it.name, it.val)
		}
	}
	// 0 = 不限总时长，是合法值；负数没有意义。
	if c.AIStreamMaxDuration < 0 {
		return fmt.Errorf("ai-stream-max-duration 不能为负（0 表示不限），当前 %s", c.AIStreamMaxDuration)
	}

	return nil
}

// UpstreamPolicy 构造拨号期的 SSRF 策略。空配置 = 拒绝所有内网地址。
func (c *Config) UpstreamPolicy() (*netguard.Policy, error) {
	return netguard.NewPolicy(strings.Split(c.AllowUpstreamCIDRs, ","))
}
