package ai

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/bobdfy/flowgate/internal/quota"
)

const (
	defaultCompletionTokens     = 512
	maxReservedCompletionTokens = 4096
)

// estimateNeed 估算一次请求最多会消耗多少 token
func estimateNeed(body []byte) int64 {
	return int64(len(body)/4) + int64(maxCompletionTokens(body))
}

// maxCompletionTokens 取 max_tokens / max_completion_tokens 缺省512，封顶4096
func maxCompletionTokens(body []byte) int {
	var req struct {
		MaxTokens           int `json:"max_tokens"`
		MaxCompletionTokens int `json:"max_completion_tokens"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return defaultCompletionTokens
	}

	n := req.MaxTokens
	if n <= 0 {
		n = req.MaxCompletionTokens
	}
	if n <= 0 {
		n = defaultCompletionTokens
	}
	if n > maxReservedCompletionTokens {
		n = maxReservedCompletionTokens
	}

	return n
}

// tpmSettler 保证一次请求只结算一次
type tpmSettler struct {
	ql   quota.Limiter
	res  quota.Reservation
	base context.Context
	once sync.Once
}

// Settle 只在第一次调用时生效
func (s *tpmSettler) Settle(actual int64) {
	s.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(s.base), 2*time.Second)
		defer cancel()
		if err := s.ql.Settle(ctx, s.res, actual); err != nil {
			slog.Error("tpm_settle_failed", "err", err)
		}

	})
}
