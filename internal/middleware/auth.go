package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"

	"github.com/bobdfy/flowgate/internal/store/postgres"
)

// identityKey 是 context 的 key 类型（自定义类型，避免和其他包用 string 撞车）。
type identityKey struct{}

// Identity 表示一次请求的已认证身份。
type Identity struct {
	TenantID  int64
	KeyID     int64
	TenantQPS int64 // 租户级限额
	KeyQPS    int64 // key 级限额
}

// Authenticator 鉴权器, 依赖APIkeyStore 查库
type Authenticator struct {
	keys *postgres.APIKeyStore
}

// NewAuthenticator 创建一个鉴权器。
func NewAuthenticator(keys *postgres.APIKeyStore) *Authenticator {
	return &Authenticator{
		keys: keys,
	}
}

// RequireAuth返回鉴权中间件。
// 流程：白名单跳过 → 取 X-API-Key（空则 401）→ hash 查库 → nil 则 401 → 注入 context 放行。
func (a *Authenticator) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		key := r.Header.Get("X-API-Key")
		if key == "" {
			slog.Warn("auth_missing_key", "path", r.URL.Path)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		k, err := a.keys.GetByHash(r.Context(), HashKey(key))
		if err != nil {
			slog.Error("auth_lookup_failed", "err", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if k == nil {
			slog.Warn("auth_invalid_key", "path", r.URL.Path)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// 把身份写进 context，下游（限流）从这里取
		ctx := context.WithValue(r.Context(), identityKey{}, &Identity{
			TenantID:  k.TenantID,
			KeyID:     k.ID,
			TenantQPS: k.TenantQPSLimit,
			KeyQPS:    k.QPSLimit,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// FromContext 从 context 取身份；没鉴权过返回 (nil, false)。供限流中间件用。
func FromContext(ctx context.Context) (*Identity, bool) {
	v, ok := ctx.Value(identityKey{}).(*Identity)
	return v, ok
}

// HashKey 返回 key 的 SHA256 十六进制哈希（与 admin 发放 key 时同一算法）。
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
