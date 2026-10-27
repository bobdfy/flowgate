package ai

import "testing"

func TestEstimateNeed(t *testing.T) {
	body := []byte(`{"model":"x","messages":[{"role":"user","content":"hello"}],"max_tokens":128}`)
	if got := estimateNeed(body); got < 128 {
		t.Fatalf("need=%d，至少应包含 max_tokens=128", got)
	}

	body2 := []byte(`{"model":"x","max_tokens":100000}`)
	if got := maxCompletionTokens(body2); got != maxReservedCompletionTokens {
		t.Fatalf("max_tokens 应封顶 %d，得到 %d", maxReservedCompletionTokens, got)
	}

	body3 := []byte(`{"model":"x"}`)
	if got := maxCompletionTokens(body3); got != defaultCompletionTokens {
		t.Fatalf("缺省应为 %d，得到 %d", defaultCompletionTokens, got)
	}
}
