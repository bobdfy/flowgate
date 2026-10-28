package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bobdfy/flowgate/internal/store/postgres"
)

// 回滚到空快照必须被明确拒绝，而且理由要能传到调用方。
// 这条用例盯的是状态码映射：409（这次操作不可执行）而不是 500（服务端故障），
// 因为 500 会让使用者以为重试有用。
func TestWriteRollbackErrorEmptySnapshot(t *testing.T) {
	// store 层会包一层上下文，handler 必须靠 errors.Is 识别，不能只比裸错误。
	err := fmt.Errorf("rollback to version id=7: %w", postgres.ErrEmptySnapshot)

	rec := httptest.NewRecorder()
	writeRollbackError(rec, err)

	if rec.Code != http.StatusConflict {
		t.Fatalf("空快照应回 409，实际 %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "条目") {
		t.Fatalf("响应里应说明拒绝原因，实际 %q", body)
	}
}

func TestWriteRollbackErrorOther(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRollbackError(rec, fmt.Errorf("db down"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("其他错误应回 500，实际 %d", rec.Code)
	}
	// 内部错误细节不能回给客户端（可能是 SQL / 连接串片段）。
	if body := rec.Body.String(); strings.Contains(body, "db down") {
		t.Fatalf("内部错误细节不该出现在响应里: %q", body)
	}
}
