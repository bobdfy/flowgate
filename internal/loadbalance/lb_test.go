package loadbalance

import (
	"sync"
	"testing"
	"time"
)

func newTestPool(threshold, cooldownMs, halfOpen int, addrs ...string) *NodePool {
	specs := make([]NodeSpec, 0, len(addrs))
	for i, a := range addrs {
		specs = append(specs, NodeSpec{ID: int64(i + 1), Address: a})
	}
	p := NewNodePool(specs)
	p.SetCBConfig(threshold, cooldownMs, halfOpen)
	return p
}

// 核心回归：多节点同时 half-open 时，只有被选中的节点占名额。
// 旧实现会给所有候选节点 +1，未选中的永久泄漏。
func TestPickClaimsOnlyChosenNode(t *testing.T) {
	p := newTestPool(1, 1, 1, "http://a", "http://b", "http://c")

	// 每个节点失败一次就打开（threshold=1），冷却 1ms
	for _, n := range p.AllNodes() {
		p.RecordResult(n, false)
	}
	time.Sleep(5 * time.Millisecond)

	l := p.pick()
	if l == nil {
		t.Fatal("冷却到期后应能派发")
	}

	sum, claimed := 0, 0
	for _, n := range p.Snapshot() {
		sum += n.HalfOpenInFlight
		if n.HalfOpenInFlight > 0 {
			claimed++
		}
	}
	if sum != 1 || claimed != 1 {
		t.Fatalf("只应占用被选中节点的名额, sum=%d claimed=%d", sum, claimed)
	}
}

// 冷却未到期的 Open 节点不派发。
func TestPickSkipsOpenBeforeCooldown(t *testing.T) {
	p := newTestPool(1, 1000, 1, "http://a")
	p.RecordResult(p.AllNodes()[0], false) // → open，冷却 1s

	if l := p.pick(); l != nil {
		t.Fatalf("冷却未到期不应派发, 却拿到 %s", l.Node().Address)
	}
}

// Done 幂等：同一次派发重复释放，只能记一次结果。
func TestLeaseDoneIsIdempotent(t *testing.T) {
	p := newTestPool(5, 1000, 1, "http://a")

	l := p.pick()
	if l == nil {
		t.Fatal("closed 节点应可派发")
	}
	l.Done(OutcomeFailure)

	if got := p.Snapshot()[0].Fails; got != 1 {
		t.Fatalf("重复 Done 不应重复记账, Fails=%d", got)
	}
}

// 并发派发 + 成败混合：结束后不能有任何名额残留。
func TestConcurrentLeasesDoNotLeak(t *testing.T) {
	p := newTestPool(3, 1, 2, "http://a", "http://b", "http://c", "http://d")

	var wg sync.WaitGroup
	for g := range 16 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				l := p.pick()
				if l == nil {
					continue
				}
				if (i+g)%3 != 0 {
					l.Done(OutcomeSuccess)
				} else {
					l.Done(OutcomeFailure)
				}
			}
		}(g)
	}
	wg.Wait()

	for _, n := range p.Snapshot() {
		if n.HalfOpenInFlight != 0 {
			t.Fatalf("节点 %s 泄漏名额: state=%s inFlight=%d", n.Address, n.State, n.HalfOpenInFlight)
		}
	}
}

// 中立结果（客户端主动取消）在 half-open 下必须归还试探名额，且不翻转状态。
// 泄漏点：占名额后提前 return，名额永不归还，等冷却过期也再探不动。
func TestNeutralOutcomeReleasesProbeSlot(t *testing.T) {
	p := newTestPool(1, 1, 1, "http://a")

	p.RecordResult(p.AllNodes()[0], false) // threshold=1 → open，冷却 1ms
	time.Sleep(5 * time.Millisecond)

	l := p.pick()
	if l == nil {
		t.Fatal("冷却到期后应能派发")
	}
	if got := p.Snapshot()[0]; got.State != StateHalfOpen || got.HalfOpenInFlight != 1 {
		t.Fatalf("派发后应 half-open 且占 1 个名额, state=%v inFlight=%d", got.State, got.HalfOpenInFlight)
	}

	l.Done(OutcomeNeutral)

	got := p.Snapshot()[0]
	if got.HalfOpenInFlight != 0 {
		t.Fatalf("中立结果未归还试探名额, inFlight=%d", got.HalfOpenInFlight)
	}
	if got.State != StateHalfOpen {
		t.Fatalf("中立结果不应翻转熔断状态, state=%v", got.State)
	}
	if got.Fails != 0 {
		t.Fatalf("中立结果不应计入失败, Fails=%d", got.Fails)
	}
}

// 中立结果在 closed 下是纯 no-op：不清零已有失败数，也不累加。
func TestNeutralOutcomeOnClosedIsNoop(t *testing.T) {
	p := newTestPool(3, 1000, 1, "http://a")
	p.RecordResult(p.AllNodes()[0], false) // Fails=1，尚未到阈值

	l := p.pick()
	if l == nil {
		t.Fatal("closed 节点应可派发")
	}
	l.Done(OutcomeNeutral)

	got := p.Snapshot()[0]
	if got.Fails != 1 {
		t.Fatalf("中立结果不应改动失败计数, Fails=%d want 1", got.Fails)
	}
	if got.State != StateClosed {
		t.Fatalf("中立结果不应翻转状态, state=%v", got.State)
	}
}

// 配置热更新与请求派发并发：不 panic、不死锁（-race 下能查出竞争）。
func TestSetCBConfigConcurrentWithPick(t *testing.T) {
	p := newTestPool(5, 1, 1, "http://a", "http://b")

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			p.SetCBConfig(1+i%5, 1+i%3, 1+i%2)
		}
	}()

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if l := p.pick(); l != nil {
			l.Done(OutcomeSuccess)
		}
	}
	<-done
}
