// Package loadbalance 提供负载均衡所需的节点池、节点状态和负载均衡器。
package loadbalance

import (
	"log/slog"
	"sync"
	"time"

	"github.com/bobdfy/flowgate/internal/observability"
)

// CircuitState 熔断器三态。
type CircuitState string

const (
	StateClosed   CircuitState = "closed"    // 正常转发，累计连续失败
	StateOpen     CircuitState = "open"      // 快速失败，冷却期内不派发
	StateHalfOpen CircuitState = "half_open" // 放少量试探请求判断是否恢复
)

// NodeState 表示一个上游实例的运行时状态。服务器信息
type NodeState struct {
	ID               int64  // upstream_nodes.id，运行时状态上报用
	Address          string // 实例地址，如 http://localhost:8091
	Weight           int    // 权重
	State            CircuitState
	Fails            int
	OpenUntil        time.Time // Open 态冷却到期时间
	HalfOpenInFlight int       // HalfOpen 态正在飞行的试探请求数
}

// NodePool 是一个服务的实例池。
// 需要锁：请求挑实例（读）和后台健康检查（写）会并发访问这份数据。
type NodePool struct {
	mu                 sync.RWMutex
	nodes              []*NodeState
	cursor             int // 轮询游标：记住上次轮到第几个
	cbFailureThreshold int // 熔断：连续失败上限
	cbCooldownMs       int // 熔断：Open 态冷却时长（毫秒）
	cbHalfOpenLimit    int // 熔断：HalfOpen 态最多同时放几个试探请求
}

// Lease 表示一次派发占用的节点名额
type Lease struct {
	pool  *NodePool
	node  *NodeState
	probe bool // 是否占用名额
	once  sync.Once
}

// Outcome 一次派发的结算结果
type Outcome int

const (
	OutcomeSuccess Outcome = iota
	OutcomeFailure
	OutcomeNeutral
)

// Node 返回本次派发到的节点。
func (l *Lease) Node() *NodeState { return l.node }

// Done 记录本次转发的最终结果并释放名额。
// 重复调用只生效一次 —— 这样「正常路径显式调用」和「defer 兜底」可以同时存在。
func (l *Lease) Done(o Outcome) {
	l.once.Do(func() { l.pool.recordResult(l.node, o, l.probe) })
}

// SetCBConfig 设置熔断参数（连续失败阈值、冷却时长、半开试探上限），非正值参数用默认值兜底。
func (p *NodePool) SetCBConfig(failurethreshold int, cooldownMs int, halfOpeninflight int) {
	if failurethreshold <= 0 {
		failurethreshold = 5
	}
	if cooldownMs <= 0 {
		cooldownMs = 10000
	}
	if halfOpeninflight <= 0 {
		halfOpeninflight = 1
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cbFailureThreshold = failurethreshold
	p.cbCooldownMs = cooldownMs
	p.cbHalfOpenLimit = halfOpeninflight
}

// NodeSpec 描述一个实例的静态配置(地址 + 权重), 用于建池
type NodeSpec struct {
	ID      int64
	Address string
	Weight  int
}

// NewNodePool 用一组地址创建实例池，默认全部健康、权重 1。
func NewNodePool(specs []NodeSpec) *NodePool {
	pool := &NodePool{
		cbFailureThreshold: 5,
		cbCooldownMs:       10000,
		cbHalfOpenLimit:    1,
	}
	for _, s := range specs {
		w := s.Weight
		if w <= 0 {
			w = 1
		}
		pool.nodes = append(pool.nodes, &NodeState{ID: s.ID, Address: s.Address, Weight: w, State: StateClosed})
	}
	return pool
}

// AllNodes 返回全部节点（含不健康的，健康检查要探测所有节点才能发现恢复）。
// 读操作，带锁返回副本，避免调用方直接碰内部切片。
func (p *NodePool) AllNodes() []*NodeState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*NodeState, len(p.nodes))
	copy(out, p.nodes)
	return out
}

// Snapshot 返回节点状态的只读值拷贝，供运行时状态上报使用。
// 与 AllNodes 不同，这里复制的是 NodeState 本身，调用方读取时不会和后台健康检查竞争。
func (p *NodePool) Snapshot() []NodeState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]NodeState, len(p.nodes))
	for i, n := range p.nodes {
		out[i] = *n
	}
	return out
}

// ShouldProbe 判断健康检查这次是否要探测该节点：Open 且冷却未到期就跳过。
// 放进池子里加锁读，避免和 pick/recordResult 并发读写 State / OpenUntil。
func (p *NodePool) ShouldProbe(n *NodeState) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return !(n.State == StateOpen && time.Now().Before(n.OpenUntil))
}

// LoadBalancer 负载均衡器接口：从池子里挑一个健康节点。
type LoadBalancer interface {
	Pick() *Lease // 返回选中的节点；没有可用节点时返回 nil
}

// RoundRobin 轮流挑选：第 1 个请求挑节点 0，第 2 个挑节点 1，…… 循环。
type RoundRobin struct {
	pool *NodePool
}

// NewRoundRobin 创建一个轮询负载均衡器。
func NewRoundRobin(pool *NodePool) *RoundRobin {
	return &RoundRobin{pool: pool}
}

// Pick 实现 LoadBalancer：从健康节点里轮流选一个。
func (p *NodePool) pick() *Lease {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	avail := make([]*NodeState, 0, len(p.nodes))
	for _, n := range p.nodes {
		switch n.State {
		case StateClosed:
			avail = append(avail, n)
		case StateOpen:
			// 冷却到期：惰性转half_open，等真实流量
			if now.Before(n.OpenUntil) {
				continue
			}
			n.State = StateHalfOpen
			n.Fails = 0
			n.HalfOpenInFlight = 0
			slog.Info("circuit_half_open",
				"addr", n.Address,
				"open_duration_ms", p.cbCooldownMs)
			observability.CircuitState.WithLabelValues(n.Address).Set(2)
			avail = append(avail, n)
		case StateHalfOpen:
			// 名额空着派发, 不在这里计数
			if n.HalfOpenInFlight < p.cbHalfOpenLimit {
				avail = append(avail, n)
			}
		}
	}
	if len(avail) == 0 {
		return nil
	}

	// 按权重展开循环队列：权重3的节点占3个格子
	var expanded []*NodeState
	for _, n := range avail {
		w := n.Weight
		if w <= 0 {
			w = 1
		}
		for i := 0; i < w; i++ {
			expanded = append(expanded, n)
		}
	}

	n := expanded[p.cursor%len(expanded)]
	p.cursor++

	l := &Lease{pool: p, node: n}
	// 只给被选中的half-open 节点占名额
	if n.State == StateHalfOpen {
		n.HalfOpenInFlight++
		l.probe = true
	}

	return l

}

// Pick 实现 LoadBalancer：轮询 + 权重
func (r *RoundRobin) Pick() *Lease {
	return r.pool.pick()
}

// RecordResult 记录一次检查结果
// ok = true 表示这次检查通过（活着）
func (p *NodePool) RecordResult(n *NodeState, ok bool) {
	o := OutcomeFailure
	if ok {
		o = OutcomeSuccess
	}
	p.recordResult(n, o, false)
}

// recordResult 在锁内更新计数、释放试探名额并决定是否翻转状态
func (p *NodePool) recordResult(n *NodeState, o Outcome, probe bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	from := n.State
	switch n.State {
	case StateClosed:
		if o == OutcomeNeutral {
			return
		}
		if o == OutcomeSuccess {
			n.Fails = 0
			return
		}
		n.Fails++
		if n.Fails >= p.cbFailureThreshold {
			n.State = StateOpen
			n.OpenUntil = time.Now().Add(time.Duration(p.cbCooldownMs) * time.Millisecond)
			n.Fails = 0
			slog.Warn("circuit_open",
				"addr", n.Address,
				"open_until", n.OpenUntil.Format(time.RFC3339),
				"failure_threshold", p.cbFailureThreshold,
				"cooldown_ms", p.cbCooldownMs)
			observability.CircuitState.WithLabelValues(n.Address).Set(1)
		}
	case StateHalfOpen:
		if probe && n.HalfOpenInFlight > 0 {
			n.HalfOpenInFlight--
		}
		if o == OutcomeNeutral {
			return
		}
		if o == OutcomeSuccess {
			// 试探成功 → 恢复正常
			n.State = StateClosed
			n.Fails = 0
			n.HalfOpenInFlight = 0
			observability.CircuitState.WithLabelValues(n.Address).Set(0)
			slog.Info("circuit_closed",
				"addr", n.Address,
				"from", from, // half_open → closed，试探成功恢复
				"reason", "probe_success")
		} else {
			// 试探失败 → 重新打开，冷却重新计时
			n.State = StateOpen
			n.OpenUntil = time.Now().Add(time.Duration(p.cbCooldownMs) * time.Millisecond)
			n.HalfOpenInFlight = 0
			observability.CircuitState.WithLabelValues(n.Address).Set(1)
			slog.Warn("circuit_open",
				"addr", n.Address,
				"from", from, // half_open → open，试探失败重开
				"reason", "probe_failed",
				"open_until", n.OpenUntil.Format(time.RFC3339))
		}
	case StateOpen:
	}
}

// SyncNodes 用新地址列表原地更新池子: 还在的保留、消失的删、新来的加
func (p *NodePool) SyncNodes(specs []NodeSpec) {
	p.mu.Lock()
	defer p.mu.Unlock()

	want := map[string]NodeSpec{}
	for _, s := range specs {
		want[s.Address] = s
	}

	kept := make([]*NodeState, 0, len(p.nodes))
	for _, n := range p.nodes {
		if s, ok := want[n.Address]; ok {
			w := s.Weight
			if w <= 0 {
				w = 1
			}
			n.ID = s.ID
			n.Weight = w
			kept = append(kept, n)
		}
	}

	have := map[string]bool{}
	for _, n := range kept {
		have[n.Address] = true
	}

	for _, s := range specs {
		if !have[s.Address] {
			w := s.Weight
			if w <= 0 {
				w = 1
			}
			kept = append(kept, &NodeState{ID: s.ID, Address: s.Address, Weight: w, State: StateClosed})
		}
	}

	p.nodes = kept

}
