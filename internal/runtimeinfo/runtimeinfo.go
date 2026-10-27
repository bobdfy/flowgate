// Package runtimeinfo 负责 gateway → Redis 的运行时状态快照上报与读取。
//
// 健康检查结果和 AI 配置加载结果都只存在于 gateway 进程内存里，
// 管理面（admin）不跑这些循环。gateway 每 5 秒把快照写进 Redis，
// admin 读取后暴露为只读查询接口；key 带 TTL，gateway 挂掉后会自动过期，
// 管理面因此能把“没有上报”和“上报了但很旧”区分开。
package runtimeinfo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/redis/go-redis/v9"
)

const (
	// StatusKey 是所有实例共享的状态 key（最后上报的实例为准）。
	StatusKey = "flowgate:gateway:status"
	// PublishInterval 是 gateway 的上报间隔。
	PublishInterval = 5 * time.Second
	// StatusTTL 是快照的存活时间：约 3 个上报周期，进程停掉后会自然过期。
	StatusTTL = 15 * time.Second
	// StaleAfter 超过这个年龄视为过期（gateway 可能已停止上报）。
	StaleAfter = StatusTTL
)

// Publish 写入一份运行时快照；PublishedAt 由本函数填当前时间。
func Publish(ctx context.Context, rdb *redis.Client, st model.GatewayRuntime) error {
	if rdb == nil {
		return errors.New("runtime status: redis client is nil")
	}
	if st.Instance == "" {
		st.Instance = "gateway"
	}
	st.PublishedAt = time.Now().UTC()
	payload, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal runtime status failed: %w", err)
	}
	if err := rdb.Set(ctx, StatusKey, payload, StatusTTL).Err(); err != nil {
		return fmt.Errorf("publish runtime status failed: %w", err)
	}
	return nil
}

// Read 读取 gateway 最近一次上报的快照。
// ok=false 表示 Redis 里还没有（gateway 未启动或已停止上报超过 TTL）。
func Read(ctx context.Context, rdb *redis.Client) (model.GatewayRuntime, bool, error) {
	var st model.GatewayRuntime
	if rdb == nil {
		return st, false, errors.New("runtime status: redis client is nil")
	}
	payload, err := rdb.Get(ctx, StatusKey).Bytes()
	if errors.Is(err, redis.Nil) {
		return st, false, nil
	}
	if err != nil {
		return st, false, fmt.Errorf("read runtime status failed: %w", err)
	}
	if err := json.Unmarshal(payload, &st); err != nil {
		return st, false, fmt.Errorf("decode runtime status failed: %w", err)
	}
	return st, true, nil
}
