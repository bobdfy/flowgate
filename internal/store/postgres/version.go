package postgres

import (
	"context"
	"errors"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// VersionStore 负责 route_versions 和 route_version_items 两张表的数据访问。
type VersionStore struct {
	pool *pgxpool.Pool
}

// NewVersionStore 创建一个 VersionStore。
func NewVersionStore(pool *pgxpool.Pool) *VersionStore {
	return &VersionStore{pool: pool}
}

// GetPublished 取当前生效版本（status='published' 的最新一条）
func (v *VersionStore) GetPublished(ctx context.Context) (*model.Version, error) {
	const query = `
		SELECT id, version, status, config_hash, published_at, created_at
		FROM route_versions
		WHERE status = 'published'
		ORDER BY version DESC
		LIMIT 1
	`
	var ver model.Version

	err := v.pool.QueryRow(ctx, query).Scan(&ver.ID, &ver.Version, &ver.Status, &ver.ConfigHash, &ver.PublishedAt, &ver.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &ver, nil
}

// GetByID 按 ID 查询版本元数据；不存在时返回 (nil, nil)。
func (v *VersionStore) GetByID(ctx context.Context, id int64) (*model.Version, error) {
	const query = `
		SELECT id, version, status, config_hash, published_at, created_at
		FROM route_versions
		WHERE id = $1`
	var ver model.Version
	err := v.pool.QueryRow(ctx, query, id).Scan(&ver.ID, &ver.Version, &ver.Status, &ver.ConfigHash, &ver.PublishedAt, &ver.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &ver, nil
}

// GetItemsByVersion 取某版本的所有快照条目。
func (v *VersionStore) GetItemsByVersion(ctx context.Context, versionID int64) ([]model.VersionItem, error) {
	const query = `
		SELECT id, version_id, resource_type, resource_id, resource_snapshot FROM route_version_items WHERE version_id = $1 ORDER BY id`

	rows, err := v.pool.Query(ctx, query, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.VersionItem, 0)

	for rows.Next() {
		var item model.VersionItem

		err := rows.Scan(&item.ID, &item.VersionID, &item.ResourceType, &item.ResourceID, &item.ResourceSnapshot)
		if err != nil {
			return nil, err
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

// ListVersions 列出所有版本（按 version 倒序，最新的在前）。
func (v *VersionStore) ListVersions(ctx context.Context) ([]model.Version, error) {
	// 提示：SELECT ... FROM route_versions ORDER BY version DESC
	const query = `SELECT id, version, status, config_hash, published_at, created_at FROM route_versions ORDER BY version DESC`

	rows, err := v.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := make([]model.Version, 0)

	for rows.Next() {
		var ver model.Version

		err := rows.Scan(&ver.ID, &ver.Version, &ver.Status, &ver.ConfigHash, &ver.PublishedAt, &ver.CreatedAt)
		if err != nil {
			return nil, err
		}
		versions = append(versions, ver)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return versions, nil
}
