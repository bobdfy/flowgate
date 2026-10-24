package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/bobdfy/flowgate/internal/loadbalance"
	"github.com/bobdfy/flowgate/internal/model"
	"github.com/bobdfy/flowgate/internal/runtimeinfo"
	"github.com/redis/go-redis/v9"
)

// publishRuntimeStatus 周期性把节点健康与 AI 配置加载结果写入 Redis，
// 供管理面的只读接口（/api/v1/runtime/*）查询。
//
// pools 是快照函数而不是 map 本身：路由刷新会并发修改 map，拿快照更安全。
func publishRuntimeStatus(
	ctx context.Context,
	rdb *redis.Client,
	instance string,
	pools func() map[int64]*loadbalance.NodePool,
	aiStatus func() model.AIRuntimeStatus,
) {
	ticker := time.NewTicker(runtimeinfo.PublishInterval)
	defer ticker.Stop()

	publish := func() {
		st := model.GatewayRuntime{Instance: instance, AI: aiStatus()}
		for serviceID, pool := range pools() {
			for _, node := range pool.Snapshot() {
				st.Nodes = append(st.Nodes, nodeRuntime(serviceID, node))
			}
		}
		if err := runtimeinfo.Publish(ctx, rdb, st); err != nil {
			slog.Warn("runtime_status_publish_failed", "err", err)
		}
	}

	publish()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			publish()
		}
	}
}

func nodeRuntime(serviceID int64, n loadbalance.NodeState) model.NodeRuntime {
	out := model.NodeRuntime{
		ServiceID:        serviceID,
		NodeID:           n.ID,
		Address:          n.Address,
		State:            string(n.State),
		Healthy:          n.State == loadbalance.StateClosed,
		Fails:            n.Fails,
		HalfOpenInFlight: n.HalfOpenInFlight,
	}
	if n.State == loadbalance.StateOpen && !n.OpenUntil.IsZero() {
		until := n.OpenUntil.UTC()
		out.OpenUntil = &until
	}
	return out
}
