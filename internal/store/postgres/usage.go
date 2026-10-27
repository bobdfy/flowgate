package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bobdfy/flowgate/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	usageQueueSize     = 4096
	usageBatchSize     = 256
	usageFlushInterval = time.Second
	usageInsertTimeout = 5 * time.Second
)

// UsageStore 负责 ai_usage 表的写入与查询。
//
// 写入走"内存队列 + 后台批量 CopyFrom"：
// 请求路径上只做一次非阻塞 send，不因为数据库抖动拖慢 AI 响应；
// 队列满时丢弃并计数（宁可用量少记一行，也不能让网关被拖死）。
type UsageStore struct {
	pool    *pgxpool.Pool
	ch      chan model.AIUsage
	dropped atomic.Int64

	wg sync.WaitGroup
}

// NewUsageStore 创建一个 UsageStore。写入需要调用 Start 启动后台协程。
func NewUsageStore(pool *pgxpool.Pool) *UsageStore {
	return &UsageStore{
		pool: pool,
		ch:   make(chan model.AIUsage, usageQueueSize),
	}
}

// Start 启动后台批量写入；ctx 取消后会先刷完队列再退出。
func (s *UsageStore) Start(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run(ctx)
	}()
}

// Close 等待后台协程退出（调用前必须先取消 Start 传入的 ctx）。
func (s *UsageStore) Close() {
	s.wg.Wait()
}

// Record 把一条用量放进队列。非阻塞：队列满直接丢弃。
func (s *UsageStore) Record(u model.AIUsage) {
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now()
	}
	select {
	case s.ch <- u:
	default:
		if n := s.dropped.Add(1); n == 1 || n%1000 == 0 {
			slog.Warn("ai_usage_dropped",
				"total_dropped", n,
				"reason", "queue full",
				"tenant_id", u.TenantID)
		}
	}
}

func (s *UsageStore) run(ctx context.Context) {
	ticker := time.NewTicker(usageFlushInterval)
	defer ticker.Stop()

	batch := make([]model.AIUsage, 0, usageBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		s.insert(batch)
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			// 退出前把队列里剩下的都写掉，最多丢 still-in-flight 的请求。
			for {
				select {
				case u := <-s.ch:
					batch = append(batch, u)
					if len(batch) >= usageBatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case u := <-s.ch:
			batch = append(batch, u)
			if len(batch) >= usageBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// insert 用 CopyFrom 批量写一批记录。失败只记日志，不重试（避免阻塞队列）。
func (s *UsageStore) insert(batch []model.AIUsage) {
	ctx, cancel := context.WithTimeout(context.Background(), usageInsertTimeout)
	defer cancel()

	rows := make([][]any, len(batch))
	for i, u := range batch {
		rows[i] = []any{
			u.TenantID, u.KeyID, u.Model,
			u.PromptTokens, u.CompletionTokens, u.TotalTokens, u.Estimated,
			u.StatusCode, u.Streaming, u.DurationMS, u.TTFTMS, u.RequestID, u.CreatedAt,
		}
	}

	_, err := s.pool.CopyFrom(ctx, pgx.Identifier{"ai_usage"},
		[]string{
			"tenant_id", "key_id", "model",
			"prompt_tokens", "completion_tokens", "total_tokens", "estimated",
			"status_code", "streaming", "duration_ms", "ttft_ms", "request_id", "created_at",
		},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		slog.Error("ai_usage_insert_failed", "rows", len(batch), "err", err)
	}
}

// UsageQuery 是用量列表 / 聚合的过滤条件。零值字段表示不过滤。
type UsageQuery struct {
	TenantID *int64
	KeyID    *int64
	Model    string
	From     time.Time
	To       time.Time
	Limit    int
	Offset   int
}

const usageColumns = `id, tenant_id, key_id, model, prompt_tokens, completion_tokens,
	total_tokens, estimated, status_code, streaming, duration_ms, ttft_ms,request_id, created_at`

// List 按时间倒序返回明细。
func (s *UsageStore) List(ctx context.Context, q UsageQuery) ([]model.AIUsage, error) {
	where, args := usageWhere(q)
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := max(q.Offset, 0)

	sql := fmt.Sprintf(`SELECT %s FROM ai_usage%s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d OFFSET $%d`, usageColumns, where, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list ai usage failed: %w", err)
	}
	defer rows.Close()

	out := []model.AIUsage{}
	for rows.Next() {
		var u model.AIUsage
		if err := rows.Scan(&u.ID, &u.TenantID, &u.KeyID, &u.Model,
			&u.PromptTokens, &u.CompletionTokens, &u.TotalTokens, &u.Estimated,
			&u.StatusCode, &u.Streaming, &u.DurationMS, &u.TTFTMS, &u.RequestID, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ai usage row failed: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// usageGroupExpr 是 Summary 支持的聚合维度。白名单，避免 SQL 注入。
var usageGroupExpr = map[string]string{
	"model":  "model",
	"key":    "key_id::text",
	"tenant": "tenant_id::text",
	"day":    "to_char(date_trunc('day', created_at), 'YYYY-MM-DD')",
}

// Summary 按 model / key / tenant / day 聚合。groupBy 不合法时按 model 处理。
func (s *UsageStore) Summary(ctx context.Context, q UsageQuery, groupBy string) ([]model.AIUsageSummary, error) {
	expr, ok := usageGroupExpr[groupBy]
	if !ok {
		expr = usageGroupExpr["model"]
	}
	where, args := usageWhere(q)

	sql := fmt.Sprintf(`SELECT %s AS grp,
			count(*),
			COALESCE(sum(prompt_tokens), 0),
			COALESCE(sum(completion_tokens), 0),
			COALESCE(sum(total_tokens), 0),
			count(*) FILTER (WHERE estimated)
		FROM ai_usage%s
		GROUP BY grp
		ORDER BY COALESCE(sum(total_tokens), 0) DESC`, expr, where)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("summarize ai usage failed: %w", err)
	}
	defer rows.Close()

	out := []model.AIUsageSummary{}
	for rows.Next() {
		var it model.AIUsageSummary
		if err := rows.Scan(&it.Group, &it.Requests, &it.PromptTokens,
			&it.CompletionTokens, &it.TotalTokens, &it.EstimatedCount); err != nil {
			return nil, fmt.Errorf("scan ai usage summary failed: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// usageWhere 拼出 WHERE 子句（含前导空格；无过滤条件时为空串）和参数。
func usageWhere(q UsageQuery) (string, []any) {
	var args []any
	sql := ""

	add := func(expr string, v any) {
		args = append(args, v)
		if sql == "" {
			sql = " WHERE "
		} else {
			sql += " AND "
		}
		sql += fmt.Sprintf(expr, len(args))
	}

	if q.TenantID != nil {
		add("tenant_id = $%d", *q.TenantID)
	}
	if q.KeyID != nil {
		add("key_id = $%d", *q.KeyID)
	}
	if q.Model != "" {
		add("model = $%d", q.Model)
	}
	if !q.From.IsZero() {
		add("created_at >= $%d", q.From)
	}
	if !q.To.IsZero() {
		add("created_at < $%d", q.To)
	}
	return sql, args
}
