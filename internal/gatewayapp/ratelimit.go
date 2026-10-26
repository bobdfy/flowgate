package gatewayapp

// 限流 key 的提取：从鉴权写进 context 的身份里取租户和 Key 的维度。

import (
	"net/http"
	"strconv"

	"github.com/bobdfy/flowgate/internal/middleware"
)

// PairKey 一次给出两个限流维度：租户（粗，先判）+ Key（细，后判）。
//
// 顺序是硬约束：Redis 的 Lua 脚本靠返回值 1/2 区分是哪个维度超限，
// 所以必须在限流之前已经过 RequireAuth（身份在 context 里）。
func PairKey(r *http.Request) (string, int64, string, int64) {
	id, ok := middleware.FromContext(r.Context())
	if !ok {
		// 理论上到不了这里（鉴权在限流外层）；真到了就按最宽松的兜底，
		// 不要用空 key —— 空 key 会让所有匿名请求共用同一个桶。
		return "anonymous", 100, "anonymous", 100
	}
	return "tenant:" + strconv.FormatInt(id.TenantID, 10), id.TenantQPS,
		"key:" + strconv.FormatInt(id.KeyID, 10), id.KeyQPS
}
