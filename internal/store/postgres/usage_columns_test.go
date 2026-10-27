package postgres

import (
	"strings"
	"testing"
)

// usageColumns 是一个 SQL 字符串常量，Go 编译器看不懂里面写了什么。
// 列名打错（比如 ttft_ms 写成 ttfs_ms）编译、vet 全部通过，
// 只有真正查询时才由 Postgres 报错 —— 而这条路径平时没人走，能藏很久。
//
// 这个测试把列名和顺序钉死：
//   - 打错字 → 直接失败
//   - 顺序变了 → Scan 的接收顺序也得同步改，否则值会静默串位
func TestUsageColumnsArePinned(t *testing.T) {
	want := []string{
		"id", "tenant_id", "key_id", "model",
		"prompt_tokens", "completion_tokens", "total_tokens", "estimated",
		"status_code", "streaming", "duration_ms", "ttft_ms", "request_id", "created_at",
	}

	got := splitColumns(usageColumns)

	if len(got) != len(want) {
		t.Fatalf("列数应为 %d，实际 %d：\n%v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 列应为 %q，实际 %q\n完整列表: %v\n"+
				"注意：改这里的同时必须改 List() 里 rows.Scan 的接收顺序",
				i+1, want[i], got[i], got)
		}
	}
}

// splitColumns 把 SQL 片段拆成列名：去掉换行/制表符/多余空格。
func splitColumns(s string) []string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
