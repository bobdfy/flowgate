package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// 这组用例守的是「配置大小写」在匹配热路径上的两个后果：
//   - methods 写成小写 → 与 req.Method 比不中，路由形同不存在；
//   - match_type 写成 "Exact" → 精确匹配退化成前缀匹配，悄悄兜住子路径。
//
// 写接口已经会归一化，但库里可能留着修复之前的数据，而且快照是 JSON 反序列化出来的，
// 谁都没保证过大小写，所以路由器自己也要归一化。

func TestParseMethodsUppercasesAndTrims(t *testing.T) {
	got := parseMethods(" get , POST,,")
	if !got[http.MethodGet] || !got[http.MethodPost] || len(got) != 2 {
		t.Fatalf("parseMethods 结果不对: %v", got)
	}

	if m := parseMethods("   "); m != nil {
		t.Fatalf("空白 methods 应表示不限方法，得到 %v", m)
	}
}

func TestRouteEntryMatchesLegacyLowercaseMethods(t *testing.T) {
	// 模拟历史数据：库里存的是 "get"
	e := &routeEntry{pattern: "/api", methods: parseMethods("get")}

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	if !e.matches(req) {
		t.Fatal("methods=\"get\" 的老路由应当匹配 GET 请求")
	}
}

func TestRouteEntryMatchesExactIgnoresCase(t *testing.T) {
	e := &routeEntry{pattern: "/api", matchType: normalizePathMatchType("Exact")}

	if !e.matches(httptest.NewRequest(http.MethodGet, "/api", nil)) {
		t.Fatal("精确匹配的路由应当命中 /api 本身")
	}
	if e.matches(httptest.NewRequest(http.MethodGet, "/api/users", nil)) {
		t.Fatal("精确匹配的路由不该命中子路径 —— 否则说明 match_type 被静默降级成了 prefix")
	}
}
