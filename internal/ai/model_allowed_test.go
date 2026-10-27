package ai

import (
	"context"
	"testing"

	"github.com/bobdfy/flowgate/internal/identity"
)

func TestModelAllowed(t *testing.T) {
	base := context.Background()

	// 没有身份（或白名单为空）→ 不限制。
	if !modelAllowed(base, "any") {
		t.Fatal("无身份时应放行")
	}
	if !modelAllowed(identity.WithIdentity(base, &identity.Identity{}), "any") {
		t.Fatal("空白名单应放行")
	}

	ctx := identity.WithIdentity(base, &identity.Identity{AllowedModels: []string{"flowgate-mock"}})
	if !modelAllowed(ctx, "flowgate-mock") {
		t.Fatal("白名单内模型应放行")
	}
	if modelAllowed(ctx, "flowgate-deepseek") {
		t.Fatal("白名单外模型应拒绝")
	}
}
