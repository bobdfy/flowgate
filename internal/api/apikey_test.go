package api

import (
	"testing"
	"time"

	"github.com/bobdfy/flowgate/internal/model"
)

// PUT /api/v1/keys/{id} 是全量覆盖语义，而 Go 的结构体分不出「没传」和「传了零值」。
// 不合并的话，改一个名字的 PUT 会顺带把 qps_limit 打回默认 100、
// 把 expires_at 清成永不过期、把已停用的 Key 重新启用 —— 而且全部无提示。
func TestMergeAPIKeyUpdate(t *testing.T) {
	expires := time.Now().Add(24 * time.Hour).Round(time.Second)
	existing := &model.APIKey{
		Name:          "old",
		Status:        "disabled",
		QPSLimit:      500,
		ExpiresAt:     &expires,
		AllowedModels: []string{"gpt-4o-mini"},
	}

	t.Run("只改 name 时其余字段保持旧值", func(t *testing.T) {
		k := model.APIKey{Name: "renamed"}
		mergeAPIKeyUpdate(&k, existing)

		if k.Name != "renamed" {
			t.Errorf("name = %q", k.Name)
		}
		if k.Status != "disabled" {
			t.Errorf("status = %q，不该把已停用的 Key 悄悄启用", k.Status)
		}
		if k.QPSLimit != 500 {
			t.Errorf("qps_limit = %d，不该被打回默认 100", k.QPSLimit)
		}
		if k.ExpiresAt == nil || !k.ExpiresAt.Equal(expires) {
			t.Errorf("expires_at 被清掉了: %v", k.ExpiresAt)
		}
		if len(k.AllowedModels) != 1 || k.AllowedModels[0] != "gpt-4o-mini" {
			t.Errorf("allowed_models 被清掉了: %v", k.AllowedModels)
		}
	})

	t.Run("显式传入的值覆盖旧值", func(t *testing.T) {
		newExpires := time.Now().Add(72 * time.Hour).Round(time.Second)
		k := model.APIKey{
			Name:          "k",
			Status:        "active",
			QPSLimit:      10,
			ExpiresAt:     &newExpires,
			AllowedModels: []string{"glm-5.3"},
		}
		mergeAPIKeyUpdate(&k, existing)

		if k.Status != "active" || k.QPSLimit != 10 {
			t.Errorf("显式值被旧值覆盖了: status=%q qps=%d", k.Status, k.QPSLimit)
		}
		if !k.ExpiresAt.Equal(newExpires) {
			t.Errorf("expires_at = %v，期望 %v", k.ExpiresAt, newExpires)
		}
		if len(k.AllowedModels) != 1 || k.AllowedModels[0] != "glm-5.3" {
			t.Errorf("allowed_models = %v", k.AllowedModels)
		}
	})

	t.Run("空数组表示显式清空限制", func(t *testing.T) {
		// nil（没传）和 []（传了空）在业务上完全不同：后者是「取消模型白名单」。
		k := model.APIKey{AllowedModels: []string{}}
		mergeAPIKeyUpdate(&k, existing)

		if k.AllowedModels == nil || len(k.AllowedModels) != 0 {
			t.Fatalf("显式传空数组应保持为空，得到 %v", k.AllowedModels)
		}
	})
}
