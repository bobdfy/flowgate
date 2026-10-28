package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bobdfy/flowgate/internal/errorlog"
)

type fakeLister struct {
	entries []errorlog.Entry
	err     error
}

func (f *fakeLister) List(context.Context) ([]errorlog.Entry, error) {
	return f.entries, f.err
}

func logsRequest(t *testing.T, h *LogsHandler, query string) (*httptest.ResponseRecorder, errorLogResponse) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/logs/errors"+query, nil))

	var body errorLogResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("解析响应失败: %v, body=%s", err, rec.Body.String())
		}
	}
	return rec, body
}

func sampleEntries() []errorlog.Entry {
	now := time.Now()
	return []errorlog.Entry{
		{TS: now, RequestID: "req-1", Route: "/v1/chat/completions", Status: 502, Kind: errorlog.KindUpstream, TenantID: 17},
		{TS: now.Add(-time.Minute), RequestID: "req-2", Route: "/v1/chat/completions", Status: 503, Kind: errorlog.KindCircuit, TenantID: 17},
		{TS: now.Add(-2 * time.Hour), RequestID: "req-3", Route: "/orders", Status: 401, Kind: errorlog.KindAuth},
	}
}

func TestListErrorsFilters(t *testing.T) {
	h := NewLogsHandler(&fakeLister{entries: sampleEntries()})

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"按分类", "?kind=circuit_open", []string{"req-2"}},
		{"按状态码", "?status=401", []string{"req-3"}},
		{"路由子串", "?route=chat", []string{"req-1", "req-2"}},
		{"请求 ID 精确匹配", "?request_id=req-1", []string{"req-1"}},
		{"按租户", "?tenant_id=17", []string{"req-1", "req-2"}},
		{"最近十分钟", "?since=10m", []string{"req-1", "req-2"}},
		{"无筛选", "", []string{"req-1", "req-2", "req-3"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec, body := logsRequest(t, h, c.query)
			if rec.Code != http.StatusOK {
				t.Fatalf("状态码应为 200，实际 %d: %s", rec.Code, rec.Body.String())
			}
			if len(body.Entries) != len(c.want) {
				t.Fatalf("应返回 %v，实际 %+v", c.want, body.Entries)
			}
			for i, id := range c.want {
				if body.Entries[i].RequestID != id {
					t.Fatalf("第 %d 条应为 %s，实际 %s（顺序必须保持最新在前）", i, id, body.Entries[i].RequestID)
				}
			}
			if body.Total != len(c.want) {
				t.Fatalf("total 应为 %d，实际 %d", len(c.want), body.Total)
			}
		})
	}
}

func TestListErrorsLimitKeepsTotal(t *testing.T) {
	h := NewLogsHandler(&fakeLister{entries: sampleEntries()})

	_, body := logsRequest(t, h, "?limit=1")
	if len(body.Entries) != 1 {
		t.Fatalf("limit=1 应只返回 1 条，实际 %d", len(body.Entries))
	}
	if body.Total != 3 {
		t.Fatalf("total 应是过滤后的总数 3，不受 limit 影响，实际 %d", body.Total)
	}
	if body.Capacity != int(errorlog.Cap) {
		t.Fatalf("capacity 应为 %d，实际 %d", errorlog.Cap, body.Capacity)
	}
}

func TestListErrorsRejectsBadQuery(t *testing.T) {
	h := NewLogsHandler(&fakeLister{})

	for _, q := range []string{"?status=abc", "?status=99", "?tenant_id=-1", "?since=x", "?limit=0"} {
		rec, _ := logsRequest(t, h, q)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 应返回 400，实际 %d", q, rec.Code)
		}
	}
}

func TestListErrorsWithoutRedis(t *testing.T) {
	rec, _ := logsRequest(t, NewLogsHandler(nil), "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("未配置 Redis 应返回 503，实际 %d", rec.Code)
	}
}

func TestListErrorsEmptyIsArray(t *testing.T) {
	rec, body := logsRequest(t, NewLogsHandler(&fakeLister{}), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d", rec.Code)
	}
	if body.Entries == nil {
		t.Fatal("空结果应返回 []，不能是 null")
	}
}
