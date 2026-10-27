package gatewayapp

import (
	"strings"
	"testing"
	"time"
)

// validConfig 是一份「校验应当通过」的基准配置。
// 每个用例从它出发只改一个字段，避免多个错误互相掩盖。
func validConfig() *Config {
	return &Config{
		Addr:               ":8090",
		Burst:              200,
		FailMode:           "fallback",
		TPMFailMode:        "fallback",
		TPMWindow:          time.Minute,
		MaxInflight:        1000,
		AllowUpstreamCIDRs: "", // 空 = 拒绝所有内网上游，是合法配置

		AIConnectTimeout:        5 * time.Second,
		AIResponseHeaderTimeout: 60 * time.Second,
		AIBufferedTimeout:       60 * time.Second,
		AIStreamIdleTimeout:     60 * time.Second,
		AIStreamMaxDuration:     0, // 0 = 不限总时长
	}
}

// 基准必须能过：如果这条挂了，其余用例的结果就没有参考价值。
func TestValidateAcceptsDefaults(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("默认配置应当通过校验，实际: %v", err)
	}
}

// 前四个分层超时取 0 或负数会让对应的保护静默失效，必须启动即失败。
// 错误信息里要带上参数名 —— 启动失败时用户得知道去改哪个 flag。
func TestValidateRejectsNonPositiveAITimeouts(t *testing.T) {
	fields := []struct {
		desc     string
		flagName string
		set      func(c *Config, d time.Duration)
	}{
		{"建连超时", "ai-connect-timeout", func(c *Config, d time.Duration) { c.AIConnectTimeout = d }},
		{"响应头超时", "ai-response-header-timeout", func(c *Config, d time.Duration) { c.AIResponseHeaderTimeout = d }},
		{"非流式总时长", "ai-buffered-timeout", func(c *Config, d time.Duration) { c.AIBufferedTimeout = d }},
		{"流式读空闲", "ai-stream-idle-timeout", func(c *Config, d time.Duration) { c.AIStreamIdleTimeout = d }},
	}

	for _, f := range fields {
		for _, d := range []time.Duration{0, -time.Second} {
			t.Run(f.desc+"/"+d.String(), func(t *testing.T) {
				c := validConfig()
				f.set(c, d)

				err := c.Validate()
				if err == nil {
					t.Fatalf("%s 取 %s 必须报错（否则保护会静默失效）", f.desc, d)
				}
				if !strings.Contains(err.Error(), f.flagName) {
					t.Fatalf("错误信息应包含参数名 %q，便于用户定位，实际: %v", f.flagName, err)
				}
			})
		}
	}
}

// 总时长上限和上面四个不一样：0 是「不限」，合法；只有负数要拒。
func TestValidateAIStreamMaxDuration(t *testing.T) {
	c := validConfig()
	c.AIStreamMaxDuration = 0
	if err := c.Validate(); err != nil {
		t.Fatalf("ai-stream-max-duration=0 表示不限，不该报错，实际: %v", err)
	}

	c = validConfig()
	c.AIStreamMaxDuration = -time.Second
	err := c.Validate()
	if err == nil {
		t.Fatal("ai-stream-max-duration 为负数必须报错")
	}
	if !strings.Contains(err.Error(), "ai-stream-max-duration") {
		t.Fatalf("错误信息应包含参数名，实际: %v", err)
	}
}
