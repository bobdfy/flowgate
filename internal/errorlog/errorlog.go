// Package errorlog 汇总失败请求，供控制台「错误请求」页钻取。
//
// 只记失败请求，不记全量日志。同样的存储预算下，最近 500 种错误能覆盖
// 几十分钟、回答「最近哪几类请求挂了、挂在哪个环节」；最近 500 条全量
// 日志只覆盖几秒。单条请求的完整上下文仍然去 gateway stdout 按
// request_id 搜（docker logs flowgate-gateway | grep <request_id>）。
//
// 按「错误签名」聚合（见 Signature）：同一种错误在窗口里只占一行，
// 带累计次数。否则一次 429 风暴就能把 500 条窗口全部占满，
// 把真正要看的 504/熔断挤出窗口。
//
// 安全边界：只记 method / route / status / 耗时 / 请求 ID / 租户与 Key 的
// 数值 ID，以及截断后的错误摘要。不记请求头、请求体，也不记 API Key 原文。
package errorlog

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bobdfy/flowgate/internal/observability"
	"github.com/redis/go-redis/v9"
)

// Key 是所有 gateway 实例共用的错误索引（Redis ZSET：member=错误签名，
// score=最近一次发生时间）。多实例天然汇总，重启不丢。
//
// 配套还有两个 HASH：
//   - Key + ":count"：签名 → 累计次数（HINCRBY 原子自增）
//   - Key + ":meta"：签名 → 最近一次的完整条目 JSON
//
// 为什么要拆成三个 key：次数要原子自增（HINCRBY），最近一次的上下文要整体
// 覆盖（HSET）。塞进同一个 JSON 里就得读-改-写，还要处理并发覆盖。
const Key = "flowgate:errors"

// 派生 key 的后缀。
const (
	countSuffix = ":count"
	metaSuffix  = ":meta"
)

// Cap 是保留的最大错误种类数（不是请求数）。
const Cap int64 = 500

// entryTTL 是聚合表的存活时间。索引按 Cap 裁剪后，被裁掉的种类会残留
// 在 count/meta 两个 HASH 里；靠 TTL 兜底回收，不做精确删除
// （精确删除要在同一个请求里多一次往返，收益不值）。
const entryTTL = 24 * time.Hour

// DetailLimit 是 detail 的最大字符数。上游错误原文可能很长（甚至带上游
// 返回内容），截断既控制单条体积，也避免把整段上游响应搬进 Redis。
const DetailLimit = 200

// Kind 是错误分类的有限枚举，前端按它分组展示。
type Kind string

const (
	KindUpstream Kind = "upstream_error"    // 上游 5xx / 连接失败
	KindTimeout  Kind = "timeout"           // 上游超时、流最大时长
	KindOverload Kind = "overload"          // 并发 / 队列 / 限流拒绝
	KindCircuit  Kind = "circuit_open"      // 熔断中，请求没到上游
	KindStream   Kind = "stream_terminated" // 流已经开始输出后中途断开
	KindAuth     Kind = "auth_failed"       // 鉴权失败
	KindInternal Kind = "internal_error"    // 网关自身错误（500）
)

// Entry 是错误看板的一行 —— 一个「错误签名」一行，不是一次请求一行。
//
// TS / RequestID / Detail 等字段取自该签名最近一次发生时的实际值；
// Count 是该签名在窗口内累计发生的次数。要看某一次具体请求，
// 拿 RequestID 去 gateway 日志里 grep。
type Entry struct {
	TS         time.Time `json:"ts"`
	Instance   string    `json:"instance"`
	RequestID  string    `json:"request_id"`
	Method     string    `json:"method"`
	Route      string    `json:"route"`
	Status     int       `json:"status"`
	DurationMS int64     `json:"duration_ms"`
	Kind       Kind      `json:"kind"`
	Detail     string    `json:"detail,omitempty"`
	TenantID   int64     `json:"tenant_id,omitempty"`
	KeyID      int64     `json:"key_id,omitempty"`
	Count      int64     `json:"count"`
}

// Signature 是「同一种错误」的指纹：方法 + 路由 + 状态码 + 分类。
//
// 刻意不含 request_id / 时间 / 耗时 / detail —— 带上它们，每次请求都会算作
// 一种新错误，聚合就失效了，等于退化成改动前的样子（一次 429 风暴灌满窗口）。
// detail 虽然最能区分根因，但它可能带上游原文，做 key 会撑大 HASH 且不稳定，
// 所以只在聚合行上展示「最近一次」的那份。
func Signature(e Entry) string {
	return e.Method + "|" + e.Route + "|" + strconv.Itoa(e.Status) + "|" + string(e.Kind)
}

// ResetLegacy 清理版本升级遗留的 key 类型冲突。
//
// 旧版本把错误存在 Redis LIST 里（LPUSH / LTRIM）。新版本改成 ZSET（签名索引）
// + 两个 HASH；旧 key 还在的话，ZADD / HINCRBY 会对它一律返回 WRONGTYPE，
// 看板从此空白，而且只在 flowgate_errorlog_dropped_total 上留痕，很难联想到原因。
// 窗口本身是易失数据（最多 500 种、TTL 24h），类型不对就直接删掉重建。
//
// gateway 启动时调用一次；key 不存在或类型正确时是空操作。
func ResetLegacy(ctx context.Context, rdb *redis.Client, key string) error {
	for _, k := range []string{key, key + countSuffix, key + metaSuffix} {
		typ, err := rdb.Type(ctx, k).Result()
		if err != nil {
			return err
		}
		want := "hash"
		if k == key {
			want = "zset"
		}
		if typ == "none" || typ == want {
			continue
		}
		if err := rdb.Del(ctx, k).Err(); err != nil {
			return err
		}
		slog.Warn("errorlog_legacy_key_reset", "key", k, "old_type", typ)
	}
	return nil
}

// Truncate 按字符（不是字节）截断 detail，避免切碎 UTF-8。
func Truncate(s string) string {
	r := []rune(s)
	if len(r) <= DetailLimit {
		return s
	}
	return string(r[:DetailLimit]) + "…"
}

// Sink 是错误条目的落点。生产实现是 RedisSink，测试用内存实现。
type Sink interface {
	Push(ctx context.Context, e Entry) error
}

// Recorder 把失败请求非阻塞地交给 Sink。
//
// 设计目标与 logsink 相同：请求路径永远不因为「写错误看板」而变慢或失败。
// 队列满、Redis 不可用时直接丢弃并计数 —— 错误本身已经有 Prometheus
// 计数器在报警，这里只是给人看的钻取入口。
type Recorder struct {
	sink     Sink
	ch       chan Entry
	done     chan struct{}
	exited   chan struct{}
	closeOne sync.Once
	dropped  atomic.Uint64
}

// NewRecorder 启动后台写入 goroutine；capacity <= 0 时取 256。
func NewRecorder(sink Sink, capacity int) *Recorder {
	if capacity <= 0 {
		capacity = 256
	}
	r := &Recorder{
		sink:   sink,
		ch:     make(chan Entry, capacity),
		done:   make(chan struct{}),
		exited: make(chan struct{}),
	}
	go r.run()
	return r
}

// Record 非阻塞入队；nil Recorder 是空操作。
func (r *Recorder) Record(e Entry) {
	if r == nil {
		return
	}
	select {
	case <-r.done:
		r.drop("closed")
		return
	default:
	}
	select {
	case r.ch <- e:
	default:
		r.drop("queue_full")
	}
}

// Dropped 返回累计丢弃条数。
func (r *Recorder) Dropped() uint64 {
	if r == nil {
		return 0
	}
	return r.dropped.Load()
}

// Close 排干队列后退出后台 goroutine，幂等。
func (r *Recorder) Close() {
	if r == nil {
		return
	}
	r.closeOne.Do(func() { close(r.done) })
	<-r.exited
}

func (r *Recorder) run() {
	defer close(r.exited)
	for {
		select {
		case e := <-r.ch:
			r.push(e)
		case <-r.done:
			// 排干剩余的条目再退出，尽量不丢最后几条。
			for {
				select {
				case e := <-r.ch:
					r.push(e)
				default:
					return
				}
			}
		}
	}
}

func (r *Recorder) push(e Entry) {
	// 单条独立超时：Redis 卡住时不能把后台 goroutine 永久挂起。
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.sink.Push(ctx, e); err != nil {
		r.drop("push_failed:" + err.Error())
	}
}

// drop 计数并降噪打印：Redis 长时间不可用时，每 100 条最多打一行日志。
func (r *Recorder) drop(reason string) {
	n := r.dropped.Add(1)
	observability.ErrorLogDroppedTotal.Inc()
	if n%100 == 1 {
		slog.Warn("errorlog_dropped", "reason", reason, "total", n)
	}
}

// RedisSink 把条目按签名聚合进 Redis。
//
// 一次 Push = 一条 pipeline（仍是 1 个 RTT）：
//
//	HINCRBY key:count <sig> 1        次数 +1
//	HSET    key:meta  <sig> <json>   覆盖成最近一次的上下文
//	ZADD    key       <now> <sig>    刷新排序（score = 最近发生时间）
//	ZREMRANGEBYRANK key 0 -(limit+1) 只保留最近 limit 种错误
//
// 注意写入次数并没有因为聚合而减少：429 风暴下仍然是每个失败请求一次
// pipeline。聚合解决的是「窗口被同一种错误占满」，不是「写放大」。
// 后者要靠在 Recorder 层做时间窗合并，是另一件事。
type RedisSink struct {
	rdb   *redis.Client
	key   string
	limit int64
}

// NewRedisSink 创建 Redis 落点。
func NewRedisSink(rdb *redis.Client, key string, limit int64) *RedisSink {
	return &RedisSink{rdb: rdb, key: key, limit: limit}
}

// Push 实现 Sink。
func (s *RedisSink) Push(ctx context.Context, e Entry) error {
	sig := Signature(e)
	// meta 里不带 Count：次数存在独立的 HASH 里由 HINCRBY 维护，
	// 这里的 Count 永远是 0，读的时候再合上。
	e.Count = 0
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	countKey, metaKey := s.key+countSuffix, s.key+metaSuffix

	pipe := s.rdb.Pipeline()
	pipe.HIncrBy(ctx, countKey, sig, 1)
	pipe.HSet(ctx, metaKey, sig, b)
	// score 用毫秒时间戳：ZSET 按分数排序天然是「最近发生」的顺序，
	// 同一个签名重复发生时只更新分数，不产生新成员。
	pipe.ZAdd(ctx, s.key, redis.Z{Score: float64(e.TS.UnixMilli()), Member: sig})
	// 保留分数最高的 limit 个，即最近发生的 limit 种错误。
	// stop 取 -(limit+1)：limit=500 时删除 rank 0..-(501)，恰好留下后 500 个。
	pipe.ZRemRangeByRank(ctx, s.key, 0, -(s.limit + 1))
	// 被裁掉的签名在两个 HASH 里会留下孤儿字段，靠 TTL 整体回收。
	pipe.Expire(ctx, countKey, entryTTL)
	pipe.Expire(ctx, metaKey, entryTTL)
	_, err = pipe.Exec(ctx)
	return err
}

// RedisReader 读取聚合后的错误列表（admin 用）。
type RedisReader struct {
	rdb   *redis.Client
	key   string
	limit int64
}

// NewRedisReader 创建读取器；limit 是读取的最大种类数（按最近发生时间倒序）。
func NewRedisReader(rdb *redis.Client, key string, limit int64) *RedisReader {
	return &RedisReader{rdb: rdb, key: key, limit: limit}
}

// List 返回最近 limit 种错误，最近发生过的在前。
// 单个签名的 meta 解析失败只跳过该行（对应的次数也会一起丢），
// 不让一条脏数据拖垮整个页面。
func (r *RedisReader) List(ctx context.Context) ([]Entry, error) {
	// 先拿签名，按「最近发生时间」倒序。
	sigs, err := r.rdb.ZRevRange(ctx, r.key, 0, r.limit-1).Result()
	if err != nil {
		return nil, err
	}
	if len(sigs) == 0 {
		return []Entry{}, nil
	}

	// 再一次性取回全部次数和 meta：两个 HMGET 走同一条 pipeline。
	pipe := r.rdb.Pipeline()
	countCmd := pipe.HMGet(ctx, r.key+countSuffix, sigs...)
	metaCmd := pipe.HMGet(ctx, r.key+metaSuffix, sigs...)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}

	counts := countCmd.Val()
	metas := metaCmd.Val()
	out := make([]Entry, 0, len(sigs))
	for i, raw := range metas {
		item, _ := raw.(string)
		if item == "" {
			// HASH 里没有：索引与 HASH 的 TTL 边界上会短暂出现，跳过即可。
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(item), &e); err != nil {
			slog.Warn("errorlog_decode_failed", "err", err)
			continue
		}
		if i < len(counts) {
			if n, ok := counts[i].(string); ok {
				if v, perr := strconv.ParseInt(n, 10, 64); perr == nil {
					e.Count = v
				}
			}
		}
		if e.Count == 0 {
			e.Count = 1
		}
		out = append(out, e)
	}
	return out, nil
}
