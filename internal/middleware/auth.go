package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/bobdfy/flowgate/internal/identity"
	"github.com/bobdfy/flowgate/internal/store/postgres"
)

// Authenticator 鉴权器, 依赖 APIKeyStore 查库。
// 注意它留在 middleware 而不是 identity 包：
// 它是个 HTTP 中间件，还依赖 *postgres.APIKeyStore，
// 放进叶子包会把数据库依赖带进去，避环的意义就没了。
type Authenticator struct {
	keys *postgres.APIKeyStore
}

// NewAuthenticator 创建一个鉴权器。
func NewAuthenticator(keys *postgres.APIKeyStore) *Authenticator {
	return &Authenticator{
		keys: keys,
	}
}

// RequireAuth 返回鉴权中间件。
// 流程：白名单跳过 → 取 X-API-Key（空则 401）→ hash 查库 → nil 则 401 → 注入 context 放行。
//
// 只影响"失败的 body 长什么样"，
// 状态码与判定逻辑完全不变。
func (a *Authenticator) RequireAuth(next http.Handler, responder ErrorResponder) http.Handler {
	fail := func(w http.ResponseWriter, r *http.Request, status int, msg string) {
		if responder != nil {
			responder(w, r, status, msg)
			return
		}
		http.Error(w, http.StatusText(status), status)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		key := r.Header.Get("X-API-Key")
		if key == "" {
			slog.Warn("auth_missing_key", "path", r.URL.Path)
			fail(w, r, http.StatusUnauthorized, "缺少 X-API-Key 请求头")
			return
		}
		k, err := a.keys.GetByHash(r.Context(), HashKey(key))
		if err != nil {
			slog.Error("auth_lookup_failed", "err", err)
			fail(w, r, http.StatusInternalServerError, "鉴权查询失败")
			return
		}
		if k == nil {
			slog.Warn("auth_invalid_key", "path", r.URL.Path)
			fail(w, r, http.StatusUnauthorized, "API Key 无效或已停用")
			return
		}

		// 把身份写进 context，下游（限流 / AI 网关 / 用量记录）从这里取。
		// 用 identity.WithIdentity 而不是直接 context.WithValue：
		// context key 是 identity 包的私有类型，只有它自己能写。
		ctx := identity.WithIdentity(r.Context(), &identity.Identity{
			TenantID:  k.TenantID,
			KeyID:     k.ID,
			TenantQPS: k.TenantQPSLimit,
			KeyQPS:    k.QPSLimit,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// FromContext 从 context 取身份；没鉴权过返回 (nil, false)。
//
// 保留成转发函数是为了让调用方（cmd/gateway 的限流 keyFn）不用改 import，
// 同时把"身份在哪个包"这件事收在 middleware 内部。
func FromContext(ctx context.Context) (*identity.Identity, bool) {
	return identity.FromContext(ctx)
}

// HashKey 返回 key 的 SHA256 十六进制哈希。
func HashKey(key string) string {
	return identity.HashKey(key)
}
