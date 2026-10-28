package middleware

import (
	"net/http"
	"strconv"
)

// DefaultMaxBodyBytes 是控制面单个请求体的上限：1 MiB。
//
// 管理配置里最大的字段是 allowed_models 之类的短列表，正常请求只有几 KB。
// 不设上限时 json.Decoder 会把整个 body 读进内存，一个 POST 几 GB 的客户端
// 就能把 admin 进程顶到 OOM —— 这是控制面最容易被忽略的一类拒绝服务。
const DefaultMaxBodyBytes int64 = 1 << 20

// LimitBody 限制请求体大小（maxBytes <= 0 表示不限制）。
//
// 两层防护：
//  1. Content-Length 已经超限时直接回 413，连读都不读；
//  2. 其余情况用 http.MaxBytesReader 包住 Body，防止客户端谎报长度
//     或用 chunked 一点点往里塞。
//
// 第 2 层超限时由读取方（json.Decoder）拿到 *http.MaxBytesError，
// 各 handler 目前统一回 400 invalid request body —— 语义上够用，
// 因为这种请求本来也无从判断字段是否合法。
func LimitBody(maxBytes int64, next http.Handler) http.Handler {
	if maxBytes <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxBytes {
			w.Header().Set("Connection", "close")
			http.Error(w, "request body too large (limit "+strconv.FormatInt(maxBytes, 10)+" bytes)",
				http.StatusRequestEntityTooLarge)
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}
