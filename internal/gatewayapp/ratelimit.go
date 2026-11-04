package gatewayapp

import (
	"net/http"
	"strconv"

	"github.com/bobdfy/flowgate/internal/middleware"
)

// PairKey 一次给出两个限流维度：租户（粗，先判）+ Key（细，后判）。
func PairKey(r *http.Request) (string, int64, string, int64) {
	id, ok := middleware.FromContext(r.Context())
	if !ok {
		// 不要用空 key —— 空 key 会让所有匿名请求共用同一个桶。
		return "anonymous", 100, "anonymous", 100
	}
	return "tenant:" + strconv.FormatInt(id.TenantID, 10), id.TenantQPS,
		"key:" + strconv.FormatInt(id.KeyID, 10), id.KeyQPS
}
