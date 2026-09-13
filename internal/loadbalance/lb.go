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
	Address          string // 实例地址，如 http://localhost:8091
	Weight           int    // 权重（WeightedRoundRobin 用，先保留默认 1）
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
	cursor             int // 轮询游标：记住上次轮到第几个（RoundRobin 用）
	CBFailureThreshold int // 熔断：连续失败上限
	CBCooldownMs       int // 熔断：Open 态冷却时长（毫秒）
	CBHalfOpenLimit    int // 熔断：HalfOpen 态最多同时放几个试探请求
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
	p.CBFailureThreshold = failurethreshold
	p.CBCooldownMs = cooldownMs
	p.CBHalfOpenLimit = halfOpeninflight
}

// NodeSpec 描述一个实例的静态配置(地址 + 权重), 用于建池
type NodeSpec struct {
	Address string
	Weight  int
}

// NewNodePool 用一组地址创建实例池，默认全部健康、权重 1。
func NewNodePool(specs []NodeSpec) *NodePool {
	pool := &NodePool{}
	for _, s := range specs {
		w := s.Weight
		if w <= 0 {
			w = 1
		}
		pool.nodes = append(pool.nodes, &NodeState{Address: s.Address, Weight: w, State: StateClosed})
	}
	return pool
}

// AvailableNodes 返回当前可派发的健康节点列表；对冷却已到期的 Open 节点惰性转入 HalfOpen 并放行试探请求。
func (p *NodePool) AvailableNodes() []*NodeState {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []*NodeState{}
	for _, n := range p.nodes {
		switch n.State {
		//closed 开放
		case StateClosed:
			out = append(out, n)
		case StateHalfOpen:
			if n.HalfOpenInFlight < p.CBHalfOpenLimit {
				n.HalfOpenInFlight++
				out = append(out, n)
			}
		case StateOpen:
			if time.Now().After(n.OpenUntil) || time.Now().Equal(n.OpenUntil) {
				// 冷却到期，惰性切换到 HalfOpen，等待真实流量试探
				n.State = StateHalfOpen
				n.HalfOpenInFlight = 0
				n.Fails = 0
				slog.Info("circuit_half_open",
					"addr", n.Address,
					"open_duration_ms", p.CBCooldownMs)
				if n.HalfOpenInFlight < p.CBHalfOpenLimit {
					n.HalfOpenInFlight++
					out = append(out, n)
				}
			}
			// 冷却未到期：跳过，不派发
		}

	}
	return out
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

// NextIndex 在锁内推进轮询游标，返回本次应挑的索引（RoundRobin 用）。
// 为什么要放 NodePool 里加锁：多个请求并发 Pick，游标必须串行推进，否则会重复/跳号。
func (p *NodePool) NextIndex(limit int) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	idx := p.cursor % limit
	p.cursor++
	return idx
}

// LoadBalancer 负载均衡器接口：从池子里挑一个健康节点。
type LoadBalancer interface {
	Pick() *NodeState // 返回选中的节点；没有可用节点时返回 nil
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
func (r *RoundRobin) Pick() *NodeState {
	available := r.pool.AvailableNodes()
	if len(available) == 0 {
		return nil // 全部不健康，无节点可用
	}

	//按权重展开成循环队列：权重 3 的节点复制 3 份
	var expanded []*NodeState
	for _, n := range available {
		w := n.Weight
		if w <= 0 {
			w = 1
		}
		for i := 0; i < w; i++ {
			expanded = append(expanded, n)
		}
	}

	idx := r.pool.NextIndex(len(expanded))
	return expanded[idx]
}

// RecordResult 记录一次检查结果，在锁内更新连续计数并决定是否翻转健康状态。
// ok = true 表示这次检查通过（活着）
func (p *NodePool) RecordResult(n *NodeState, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	from := n.State

	switch n.State {
	case StateClosed:
		if ok {
			n.Fails = 0
			return
		}
		n.Fails++
		if n.Fails >= p.CBFailureThreshold {
			n.State = StateOpen
			n.OpenUntil = time.Now().Add(time.Duration(p.CBCooldownMs) * time.Millisecond)
			n.Fails = 0
			slog.Warn("circuit_open",
				"addr", n.Address,
				"open_until", n.OpenUntil.Format(time.RFC3339),
				"failure_threshold", p.CBFailureThreshold,
				"cooldown_ms", p.CBCooldownMs)
		}
	case StateHalfOpen:
		// 释放一个试探名额
		if n.HalfOpenInFlight > 0 {
			n.HalfOpenInFlight--
		}
		if ok {
			// 试探成功 → 恢复正常
			n.State = StateClosed
			n.Fails = 0
			n.HalfOpenInFlight = 0
			observability.CircuitState.WithLabelValues(n.Address).Set(1)
			slog.Info("circuit_closed",
				"addr", n.Address,
				"from", from, // half_open → closed，试探成功恢复
				"reason", "probe_success")
		} else {
			// 试探失败 → 重新打开，冷却重新计时
			n.State = StateOpen
			n.OpenUntil = time.Now().Add(time.Duration(p.CBCooldownMs) * time.Millisecond)
			n.HalfOpenInFlight = 0
			observability.CircuitState.WithLabelValues(n.Address).Set(1)
			slog.Warn("circuit_open",
				"addr", n.Address,
				"from", from, // half_open → open，试探失败重开
				"reason", "probe_failed",
				"open_until", n.OpenUntil.Format(time.RFC3339))
		}

	case StateOpen:
		// Open 态不派发流量，正常不会走到这里；
		// 如果是健康检查在冷却到期后探测到的结果，也忽略——
		// 恢复由 AvailableNodes 的惰性转换 + 真实流量试探驱动
	}
}

// SyncNodes 用新地址列表原地更新池子: 还在的保留、消失的删、新来的加
func (p *NodePool) SyncNodes(specs []NodeSpec) {
	p.mu.Lock()
	defer p.mu.Unlock()

	want := map[string]int{}
	for _, s := range specs {
		want[s.Address] = s.Weight
	}

	kept := make([]*NodeState, 0, len(p.nodes))
	for _, n := range p.nodes {
		if w, ok := want[n.Address]; ok {
			if w <= 0 {
				w = 1
			}
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
			kept = append(kept, &NodeState{Address: s.Address, Weight: w, State: StateClosed})
		}
	}

	p.nodes = kept

}
