// Package loadbalance 提供负载均衡所需的节点池、节点状态和负载均衡器。
package loadbalance

import (
	"sync"
)

// NodeState 表示一个上游实例的运行时状态。服务器信息
type NodeState struct {
	Address string // 实例地址，如 http://localhost:8091
	Weight  int    // 权重（WeightedRoundRobin 用，先保留默认 1）
	Healthy bool   // 是否健康；健康才会被派发流量
	fails   int    //连续失败次数
	success int    //连续成功次数
}

// NodePool 是一个服务的实例池。
// 需要锁：请求挑实例（读）和后台健康检查（写）会并发访问这份数据。
type NodePool struct {
	mu     sync.RWMutex
	nodes  []*NodeState
	cursor int // 轮询游标：记住上次轮到第几个（RoundRobin 用）
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
		pool.nodes = append(pool.nodes, &NodeState{Address: s.Address, Weight: w, Healthy: true})
	}
	return pool
}

// HealthyNodes 返回所有健康节点（供负载均衡器挑选）。
// 读操作，用读锁。
func (p *NodePool) HealthyNodes() []*NodeState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := []*NodeState{}
	for _, n := range p.nodes {
		if n.Healthy {
			out = append(out, n)
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

// MarkHealthy 把节点标记为健康（实例恢复后重新加入）。
// 写操作，用写锁。
func (p *NodePool) MarkHealthy(n *NodeState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n.Healthy = true
}

// MarkUnhealthy 把节点标记为不健康（摘除，不再被挑中）。
func (p *NodePool) MarkUnhealthy(n *NodeState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n.Healthy = false
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
	healthy := r.pool.HealthyNodes()
	if len(healthy) == 0 {
		return nil // 全部不健康，无节点可用
	}

	//按权重展开成循环队列：权重 3 的节点复制 3 份
	var expanded []*NodeState
	for _, n := range healthy {
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
	if ok {
		n.success++
		n.fails = 0
		if n.success >= 2 { // 连续成功 2 次 → 恢复
			n.Healthy = true
		}
	} else {
		n.fails++
		n.success = 0
		if n.fails >= 3 { // 连续失败 3 次 → 摘除
			n.Healthy = false
		}
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
			kept = append(kept, &NodeState{Address: s.Address, Weight: w, Healthy: true})
		}
	}

	p.nodes = kept

}
