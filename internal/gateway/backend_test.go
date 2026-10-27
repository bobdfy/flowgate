package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bobdfy/flowgate/internal/loadbalance"
)

// classifyOutcome 的难点只有一个：超时到底算谁的。
// 客户端自己超时/取消 → 中立（不计熔断）；网关 WithTimeout 到期 → 节点失败。
// 两者都表现为 context.DeadlineExceeded，靠 origCtx 区分。
func TestClassifyOutcome(t *testing.T) {
	healthy := context.Background()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	expiredCtx, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()

	tests := []struct {
		name   string
		ctx    context.Context
		err    error
		status int
		want   loadbalance.Outcome
		reason string
	}{
		{"正常响应", healthy, nil, 200, loadbalance.OutcomeSuccess, ""},
		{"4xx 是客户端问题", healthy, nil, 404, loadbalance.OutcomeSuccess, ""},
		{"5xx 记在节点头上", healthy, nil, 502, loadbalance.OutcomeFailure, "status"},
		{"客户端取消", canceled, context.Canceled, 0, loadbalance.OutcomeNeutral, ""},
		{"客户端自身超时", expiredCtx, context.DeadlineExceeded, 0, loadbalance.OutcomeNeutral, ""},
		// 关键用例：origCtx 健康却拿到 DeadlineExceeded，
		// 说明是我们自己的 WithTimeout 到期 = 上游太慢，必须计入失败。
		{"网关超时即上游太慢", healthy, context.DeadlineExceeded, 0, loadbalance.OutcomeFailure, "timeout"},
		{"连接错误", healthy, errors.New("connection refused"), 0, loadbalance.OutcomeFailure, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := classifyOutcome(tt.ctx, tt.err, tt.status)
			if got != tt.want || reason != tt.reason {
				t.Fatalf("classifyOutcome() = (%v, %q), want (%v, %q)", got, reason, tt.want, tt.reason)
			}
		})
	}
}
