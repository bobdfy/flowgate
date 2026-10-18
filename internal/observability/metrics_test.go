package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestPGPoolMetricsRegistered 验证连接池观测指标确实注册进了默认 Gatherer。
//
// 为什么值得为"注册"写一条测试：
//  1. prometheus.MustRegister 在指标名重复或命名非法时会 panic —— 而它跑在 init() 里，
//     一旦 panic 就是进程起不来。这条用例把"启动即崩"提前到测试阶段暴露。
//  2. 没有数据库的环境里，无法真的起 gateway 去 curl :9090/metrics；
//     这条用例是唯一能自动验证「指标会出现在 /metrics 上」的手段。
func TestPGPoolMetricsRegistered(t *testing.T) {
	want := []string{
		"flowgate_pgxpool_acquired_conns",
		"flowgate_pgxpool_idle_conns",
		"flowgate_pgxpool_total_conns",
		"flowgate_pgxpool_constructing_conns",
		"flowgate_pgxpool_max_conns",
		"flowgate_pgxpool_acquires_total",
		"flowgate_pgxpool_empty_acquires_total",
		"flowgate_pgxpool_canceled_acquires_total",
		"flowgate_pgxpool_new_conns_total",
		"flowgate_pgxpool_destroyed_lifetime_total",
		"flowgate_pgxpool_destroyed_idle_total",
		"flowgate_pgxpool_acquire_seconds_total",
		"flowgate_pgxpool_empty_acquire_wait_seconds_total",
	}

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("采集指标失败（说明有指标定义有问题）: %v", err)
	}

	present := map[string]bool{}
	for _, mf := range mfs {
		present[mf.GetName()] = true
	}

	for _, name := range want {
		if !present[name] {
			t.Errorf("指标 %s 没有注册，压测时看不到连接池水位", name)
		}
	}
}
