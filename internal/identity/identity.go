// Package identity 定义"请求属于谁"这一最基础的类型。
//
// 为什么单独成一个包：ai / usage / middleware 都需要"这次请求是谁"，
// 如果它们各自去 import middleware，而 middleware 以后又要 import ai，
// 就成环了。把身份抽成零依赖的叶子包，两边都引它，环永远不会出现。
//
// 硬约束：本包只允许 import 标准库。
// 一旦它 import 了 store / middleware / ai 里的任何东西，
// 这个包就失去了"避环"的意义。
package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

// ctxKey 是 context 的 key 类型。
//
// 用私有空结构体而不是字符串：别的包没法构造出这个类型，
// 也就不可能读到或覆盖我们写进去的身份。
// 如果这里用 string 当 key，任何包写 "identity" 都能伪造身份。
type ctxKey struct{}

// Identity 表示一次请求的已认证身份。
type Identity struct {
	TenantID int64 // 所属租户
	KeyID    int64 // 用的是哪个 API Key
	// TenantQPS / KeyQPS 是两层的限流额度。
	// 放在身份里一起传递，是因为限流中间件只认 Identity，
	// 不该为了拿额度再去查一次库。
	TenantQPS int64
	KeyQPS    int64
}

// WithIdentity 把身份注入 context，返回新的 context。
//
// 只应该由鉴权中间件调用一次；下游用 FromContext 取。
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext 从 context 取身份；没鉴权过返回 (nil, false)。
//
// 约定：返回 nil 时调用方必须自己决定怎么办（放行 / 401），
// 不要假设"没有身份就是匿名放行"。
func FromContext(ctx context.Context) (*Identity, bool) {
	v, ok := ctx.Value(ctxKey{}).(*Identity)
	return v, ok
}

// HashKey 返回 key 的 SHA256 十六进制哈希。
//
// 发放 key（admin）和校验 key（gateway 鉴权）必须用同一个算法：
// 库里只存哈希、不存明文，两边的算法一旦漂移，所有已发放的 key 立刻全部失效。
// 所以这个函数只在这里实现一份，别处一律调它。
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
