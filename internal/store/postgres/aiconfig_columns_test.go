package postgres

import (
	"testing"
)

// aiModelCols 和 usageColumns 是同一类隐患：SQL 字符串常量，Go 编译器看不了里面写了什么。
// 漏掉一列（比如 max_concurrency）编译、vet、全部通过，只有真查库时才发现值串位或报错。
//
// 这个测试把列名和顺序钉死，同时提醒：改这里必须同步改三处 rows.Scan ——
// Snapshot()、ListModels()、以及 Create/Update 的 INSERT/UPDATE 语句。
func TestAIModelColumnsArePinned(t *testing.T) {
	want := []string{
		"name", "provider_name", "upstream_model", "max_concurrency",
		"enabled", "created_at", "updated_at",
	}

	got := splitColumns(aiModelCols)

	if len(got) != len(want) {
		t.Fatalf("列数应为 %d，实际 %d：\n%v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 列应为 %q，实际 %q\n完整列表: %v\n"+
				"注意：改这里的同时必须改 Snapshot() 和 ListModels() 里 rows.Scan 的接收顺序",
				i+1, want[i], got[i], got)
		}
	}
}
