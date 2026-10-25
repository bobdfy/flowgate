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
	l.Done(false)
	l.Done(false)

	if got := p.Snapshot()[0].Fails; got != 1 {
		t.Fatalf("重复 Done 不应重复记账, Fails=%d", got)
	}
}

// 并发派发 + 成败混合：结束后不能有任何名额残留。
func TestConcurrentLeasesDoNotLeak(t *testing.T) {
	p := newTestPool(3, 1, 2, "http://a", "http://b", "http://c", "http://d")

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				l := p.pick()
				if l == nil {
					continue
				}
				l.Done((i+g)%3 != 0)
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
			l.Done(true)
		}
	}
	<-done
}
