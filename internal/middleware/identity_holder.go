package middleware

import (
	"context"

	"github.com/bobdfy/flowgate/internal/identity"
)

// identityHolderKey 是 ctx 的键，值为 *identityHolder。
type identityHolderKey struct{}

// identityHolder 让外层 Logging 读到内层鉴权写入的身份。
//
// 和 routeLabel 同理：context 的值只能向下传，而身份是鉴权中间件内层才
// 有的。外层如果直接 identity.FromContext 拿到的永远是 nil —— 权限值在
// 内层新建的 request 对象上，外层手里那个 request 看不到。
type identityHolder struct{ id *identity.Identity }

func withIdentityHolder(ctx context.Context) context.Context {
	return context.WithValue(ctx, identityHolderKey{}, &identityHolder{})
}

// storeIdentitySnapshot 由鉴权中间件调用；没有持有者时是空操作。
func storeIdentitySnapshot(ctx context.Context, id *identity.Identity) {
	if h, ok := ctx.Value(identityHolderKey{}).(*identityHolder); ok {
		h.id = id
	}
}

// IdentitySnapshot 返回本次请求的身份；没鉴权过返回 (nil, false)。
func IdentitySnapshot(ctx context.Context) (*identity.Identity, bool) {
	if h, ok := ctx.Value(identityHolderKey{}).(*identityHolder); ok && h.id != nil {
		return h.id, true
	}
	return nil, false
}
