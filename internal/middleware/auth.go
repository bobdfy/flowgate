package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bobdfy/flowgate/internal/identity"
	"github.com/bobdfy/flowgate/internal/store/postgres"
)

// Authenticator 鉴权器, 依赖 APIKeyStore 查库。
// 内建进程内 TTL 缓存：hash → APIKey（含负缓存）
type Authenticator struct {
	keys  *postgres.APIKeyStore
	cache *authCache
}

// NewAuthenticator 创建一个鉴权器（默认关闭缓存，保持旧行为）。
func NewAuthenticator(keys *postgres.APIKeyStore) *Authenticator {
	return &Authenticator{
		keys: keys,
		// ttl=0 → 缓存禁用，每请求都查库。
		cache: newAuthCache(0),
	}
}

// NewAuthenticatorWithCache 创建一个带鉴权缓存的鉴权器。
// ttl 是缓存有效期；<=0 时退化为 NewAuthenticator（缓存禁用）
func NewAuthenticatorWithCache(keys *postgres.APIKeyStore, ttl time.Duration) *Authenticator {
	return &Authenticator{
		keys:  keys,
		cache: newAuthCache(ttl),
	}
}

// apiKeyFromRequest 取客户端 key：优先 X-API-Key，其次 Authorization: Bearer。
//
// 后者是为了兼容 OpenAI SDK / agent 这类只会发 Bearer 的客户端；
// Authorization 本身不在转发白名单里，因此不会泄露给上游。
func apiKeyFromRequest(r *http.Request) string {
	if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
		return key
	}
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
		return strings.TrimSpace(auth[len(prefix):])
	}
	return ""
}

// RequireAuth 返回鉴权中间件。
// 流程：白名单跳过 → 取 X-API-Key / Authorization Bearer（空则 401）
// → hash 查库 → nil 则 401 → 注入 context 放行。
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

		key := apiKeyFromRequest(r)
		if key == "" {
			slog.Warn("auth_missing_key", "path", r.URL.Path)
			fail(w, r, http.StatusUnauthorized, "缺少 X-API-Key 或 Authorization: Bearer 请求头")
			return
		}

		hash := HashKey(key)

		// 先查进程内缓存：命中直接短路，省掉一次 PostgreSQL 往返。
		// 负缓存也参与 —— hit=true 且 k=nil 表示「确定查无」。
		if k, hit := a.cache.get(hash); hit {
			if k == nil {
				slog.Warn("auth_invalid_key", "path", r.URL.Path)
				fail(w, r, http.StatusUnauthorized, "API Key 无效或已停用")
				return
			}
			ctx := identity.WithIdentity(r.Context(), &identity.Identity{
				TenantID:  k.TenantID,
				KeyID:     k.ID,
				TenantQPS: k.TenantQPSLimit,
				TenantTPM: k.TenantTPMLimit,
				KeyQPS:    k.QPSLimit,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		k, err := a.keys.GetByHash(r.Context(), hash)
		if err != nil {
			slog.Error("auth_lookup_failed", "err", err)
			fail(w, r, http.StatusInternalServerError, "鉴权查询失败")
			return
		}

		// 无论命中与否都写缓存（含负缓存），避免无效 key 打穿数据库。
		a.cache.put(hash, k)

		if k == nil {
			slog.Warn("auth_invalid_key", "path", r.URL.Path)
			fail(w, r, http.StatusUnauthorized, "API Key 无效或已停用")
			return
		}

		// 把身份写进 context，下游（限流 / AI 网关 / 用量记录）从这里取。
		// 用 identity.WithIdentity 而不是直接 context.WithValue
		ctx := identity.WithIdentity(r.Context(), &identity.Identity{
			TenantID:  k.TenantID,
			KeyID:     k.ID,
			TenantQPS: k.TenantQPSLimit,
			TenantTPM: k.TenantTPMLimit,
			KeyQPS:    k.QPSLimit,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// FromContext 从 context 取身份；没鉴权过返回 (nil, false)。
func FromContext(ctx context.Context) (*identity.Identity, bool) {
	return identity.FromContext(ctx)
}

// HashKey 返回 key 的 SHA256 十六进制哈希。
func HashKey(key string) string {
	return identity.HashKey(key)
}
