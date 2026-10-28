package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bobdfy/flowgate/internal/identity"
	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/observability"
	"golang.org/x/sync/singleflight"
)

type keyLookup interface {
	GetByHash(ctx context.Context, hash string) (*model.APIKey, error)
}

// Authenticator 鉴权器, 依赖 APIKeyStore 查库。
// 内建进程内 TTL 缓存：hash → APIKey（含负缓存）
type Authenticator struct {
	keys  keyLookup
	cache *authCache

	// sf 合并同一 key 的并发 miss：热点 key 刚过期就融入几百请求只一次数据库查询
	sf singleflight.Group
}

// NewAuthenticator 创建一个鉴权器（默认关闭缓存，保持旧行为）。
func NewAuthenticator(keys keyLookup) *Authenticator {
	return &Authenticator{
		keys: keys,
		// ttl=0 → 缓存禁用，每请求都查库。
		cache: newAuthCache(0),
	}
}

// NewAuthenticatorWithCache 创建一个带鉴权缓存的鉴权器。
// ttl 是缓存有效期；<=0 时退化为 NewAuthenticator（缓存禁用）
func NewAuthenticatorWithCache(keys keyLookup, ttl time.Duration) *Authenticator {
	return &Authenticator{
		keys:  keys,
		cache: newAuthCache(ttl),
	}
}

// InvalidateCache 清空进程内鉴权缓存。
// gateway 轮询到 auth_version 变化时调用，让 key 白名单 / 租户限额改动立即生效；
// 缓存未启用时是空操作。
func (a *Authenticator) InvalidateCache() {
	a.cache.clear()
}

// loadKey 在缓存未命中时回源，并用 singleflight 合并同一 key 的并发 miss
func (a *Authenticator) loadKey(ctx context.Context, hash string) (*model.APIKey, error) {
	if a.cache.disabled() {
		k, err := a.keys.GetByHash(ctx, hash)
		recordDBSearch(k, err)
		return k, err
	}

	v, err, _ := a.sf.Do(hash, func() (any, error) {
		if k, hit := a.cache.peek(hash); hit {
			return k, nil
		}
		k, err := a.keys.GetByHash(ctx, hash)
		recordDBSearch(k, err)
		if err != nil {
			return nil, err
		}
		a.cache.put(hash, k)
		return k, nil
	})
	if err != nil {
		return nil, err
	}

	k, _ := v.(*model.APIKey)
	return k, nil

}

// apiKeyFromRequest 取客户端 key：优先 X-API-Key，其次 Authorization: Bearer。
//
// 后者是为了兼容 OpenAI SDK / agent 这类只会发 Bearer 的客户端；
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

// setIdentity 把 key 对应的身份写进 ctx，交给下游（限流 / AI 网关 / 用量记录）。
func setIdentity(ctx context.Context, k *model.APIKey) context.Context {
	id := &identity.Identity{
		TenantID:      k.TenantID,
		KeyID:         k.ID,
		TenantQPS:     k.TenantQPSLimit,
		TenantTPM:     k.TenantTPMLimit,
		KeyQPS:        k.QPSLimit,
		AllowedModels: k.AllowedModels,
	}
	// 外层的 Logging 拿不到内层新建的 ctx，错误看板要靠这个快照拿到租户 / Key。
	storeIdentitySnapshot(ctx, id)
	return identity.WithIdentity(ctx, id)
}

// RequireAuth 返回鉴权中间件。
// 流程：白名单跳过 → 取 X-API-Key / Authorization Bearer（空则 401）
// → hash 查库 → nil 则 401 → 注入 context 放行。
func (a *Authenticator) RequireAuth(next http.Handler, responder ErrorResponder) http.Handler {
	fail := func(w http.ResponseWriter, r *http.Request, status int, msg string) {
		// 鉴权失败发生在路由之前，指标标签用固定值而不是路径
		SetRouteLabel(r, LabelAuthRejected)
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

			next.ServeHTTP(w, r.WithContext(setIdentity(r.Context(), k)))
			return
		}

		// 未命中（含过期）：singleflight 合并回源。
		k, err := a.loadKey(r.Context(), hash)
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

		next.ServeHTTP(w, r.WithContext(setIdentity(r.Context(), k)))
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

// recordDBSearch 记一次回源结果。缓存开/关两条路径都要记，否则开缓存前后的回源量没法对比。
func recordDBSearch(k *model.APIKey, err error) {
	switch {
	case err != nil:
		observability.AuthDBSearchesTotal.WithLabelValues("error").Inc()
	case k == nil:
		observability.AuthDBSearchesTotal.WithLabelValues("not_found").Inc()
	default:
		observability.AuthDBSearchesTotal.WithLabelValues("found").Inc()
	}
}
