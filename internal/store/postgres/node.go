package postgres

import (
	"context"
	"fmt"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NodeStore 负责 upstream_nodes 表的数据访问。
type NodeStore struct {
	pool *pgxpool.Pool
}

// NewNodeStore 创建一个 NodeStore。
func NewNodeStore(pool *pgxpool.Pool) *NodeStore {
	return &NodeStore{pool: pool}
}

// Create 插入一条实例记录，并把自增 ID 写回 node.ID。
func (n *NodeStore) Create(ctx context.Context, node *model.Node) error {
	err := n.pool.QueryRow(ctx,
		`INSERT INTO upstream_nodes (service_id, address, weight, enabled) 
		 VALUES ($1, $2, $3, $4) 
		 RETURNING id`,
		node.ServiceID, node.Address, node.Weight, node.Enabled,
	).Scan(&node.ID)

	if err != nil {
		// 包装错误，带上关键业务标识
		return fmt.Errorf("create node failed: %w", err)
	}
	return nil
}

// ListByService 返回某个服务下的所有实例。
func (n *NodeStore) ListByService(ctx context.Context, serviceID int64) ([]model.Node, error) {
	rows, err := n.pool.Query(ctx,
		`SELECT id, service_id, address, weight, enabled, health_status, created_at, updated_at
		 FROM upstream_nodes 
		 WHERE service_id = $1 
		 ORDER BY id`,
		serviceID,
	)
	if err != nil {
		return nil, fmt.Errorf("query nodes by service_id=%d failed: %w", serviceID, err)
	}
	// 重要：必须 defer rows.Close() 释放数据库连接
	defer rows.Close()

	var nodes []model.Node
	for rows.Next() {
		var node model.Node
		err := rows.Scan(
			&node.ID,
			&node.ServiceID,
			&node.Address,
			&node.Weight,
			&node.Enabled,
			&node.HealthStatus,
			&node.CreatedAt,
			&node.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan node row failed: %w", err)
		}
		nodes = append(nodes, node)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	// 如果没有查到数据，返回空切片（而不是 nil），避免调用方判空麻烦
	if nodes == nil {
		return []model.Node{}, nil
	}
	return nodes, nil
}

// Update 按 ID 更新实例的可变字段（address/weight/enabled）。
func (n *NodeStore) Update(ctx context.Context, node *model.Node) error {
	cmdTag, err := n.pool.Exec(ctx,
		`UPDATE upstream_nodes 
		 SET address = $1, weight = $2, enabled = $3, updated_at = now() 
		 WHERE id = $4`,
		node.Address, node.Weight, node.Enabled, node.ID,
	)
	if err != nil {
		return fmt.Errorf("update node failed: %w", err)
	}

	// 【最佳实践】如果 RowsAffected() == 0，说明数据已被删除或 ID 不存在，应当报错
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("update node failed: record not found")
	}
	return nil
}

// Delete 按 ID 删除实例。
func (n *NodeStore) Delete(ctx context.Context, id int64) error {
	cmdTag, err := n.pool.Exec(ctx,
		`DELETE FROM upstream_nodes WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("delete node failed: %w", err)
	}

	// 同样判断是否真的删掉了东西
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("delete node failed: record not found")
	}
	return nil
}
