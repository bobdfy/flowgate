package gatewayapp

// 网关进程的命令行参数：定义和校验都在这里，cmd/gateway/main.go 只负责调用。

import (
	"flag"
	"fmt"
	"time"
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
}

// ParseFlags 定义并解析命令行参数。只能在 main 里调用一次。
func ParseFlags() *Config {
	c := &Config{}

	flag.StringVar(&c.Addr, "addr", ":8090", "监听地址")

	flag.BoolVar(&c.AIEnabled, "ai", true, "是否启用 AI Gateway(/v1/* 路径)")

	flag.StringVar(&c.AIConfigPath, "ai-config", "ai.yaml", "AI Gateway 配置文件路径")

	flag.Float64Var(&c.Burst, "burst", 200, "令牌桶容量，允许的最大突发请求数")

	flag.StringVar(&c.FailMode, "fail-mode", "fallback", "Redis 故障时的策略:closed / fallback / open")

	flag.DurationVar(&c.AuthCacheTTL, "auth-cache-ttl", 0, "鉴权结果进程内缓存 TTL（>0 启用；0 关闭）")

	flag.DurationVar(&c.TPMWindow, "tpm-window", time.Minute, "TPM 配额窗口长度（如 1m / 5m / 1h）")

	flag.BoolVar(&c.PProf, "pprof", false, "启用 pprof 调试端点(默认关闭, 仅 127.0.0.1)")

	flag.StringVar(&c.MetricsAddr, "metrics-addr", "127.0.0.1:9090", "Prometheus 指标监听地址（默认仅本机）")

	flag.StringVar(&c.TPMFailMode, "tpm-fail-mode", "fallback", "TPM 在 redis 故障时的策略: closed/fallback/open")

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
	return nil
}
