package errorlog

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// 这组测试要防的错：
//  1. Record 在队列满时阻塞 → Redis 抖动会把请求 goroutine 一起拖住
//  2. Close 后仍往无人消费的队列里塞 → 内存只增不减
//  3. 退出时没排干队列 → 最后几条错误（往往就是最关键的）丢了
//  4. detail 截断踩碎 UTF-8 / 截断后仍超长
//  5. 聚合键选错 → 每次请求都算「新错误」，窗口被同一种错误灌满（改动前的缺陷）

type memSink struct {
	mu      sync.Mutex
	entries []Entry
}

func (s *memSink) Push(_ context.Context, e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
	return nil
}

func (s *memSink) all() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, len(s.entries))
	copy(out, s.entries)
	return out
}

// gatedSink 在 gate 关闭前一直阻塞，用来制造「后端写得慢」的场景。
type gatedSink struct{ gate chan struct{} }

func (s *gatedSink) Push(ctx context.Context, _ Entry) error {
	select {
	case <-s.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestRecorderDrainsOnClose(t *testing.T) {
	sink := &memSink{}
	r := NewRecorder(sink, 16)

	for i := 0; i < 5; i++ {
		r.Record(Entry{Status: 500, RequestID: string(rune('a' + i))})
	}
	r.Close()

	got := sink.all()
	if len(got) != 5 {
		t.Fatalf("Close 应排干队列，实际落盘 %d 条", len(got))
	}
	for i, e := range got {
		if want := string(rune('a' + i)); e.RequestID != want {
			t.Fatalf("第 %d 条顺序错乱：want %q, got %q", i, want, e.RequestID)
		}
	}
}

func TestRecorderDoesNotBlockWhenQueueFull(t *testing.T) {
	sink := &gatedSink{gate: make(chan struct{})}
	r := NewRecorder(sink, 1)

	r.Record(Entry{Status: 500}) // 消费者取走并卡在 Push
	r.Record(Entry{Status: 500}) // 占满容量为 1 的队列

	start := time.Now()
	for i := 0; i < 100; i++ {
		r.Record(Entry{Status: 500})
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("队列满时 Record 不能阻塞，实际耗时 %s", elapsed)
	}
	if r.Dropped() == 0 {
		t.Fatal("队列满时应计入丢弃")
	}

	close(sink.gate)
	r.Close()
}

func TestRecorderDropAfterClose(t *testing.T) {
	sink := &memSink{}
	r := NewRecorder(sink, 4)
	r.Close()

	r.Record(Entry{Status: 502}) // 不能 panic
	if r.Dropped() != 1 {
		t.Fatalf("Close 之后的条目应计入丢弃，实际 %d", r.Dropped())
	}

	r.Close() // 幂等
}

func TestRecorderNilIsNoop(t *testing.T) {
	var r *Recorder
	r.Record(Entry{Status: 500})
	r.Close()
	if r.Dropped() != 0 {
		t.Fatal("nil Recorder 不该有丢弃计数")
	}
}

func TestTruncateKeepsRunesIntact(t *testing.T) {
	long := strings.Repeat("上游错误", 100) // 400 个字符
	got := Truncate(long)

	if !strings.HasPrefix(got, "上游错误") {
		t.Fatalf("截断后中文应保持完整，实际开头 %q", got[:12])
	}
	if n := len([]rune(got)); n != DetailLimit+1 {
		t.Fatalf("应按字符截断到 %d + 省略号，实际 %d", DetailLimit, n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatal("截断后应有省略号")
	}

	short := "上游超时"
	if Truncate(short) != short {
		t.Fatal("未超长的 detail 不应改动")
	}
}

func TestReasonHolder(t *testing.T) {
	ctx := WithHolder(context.Background())
	Set(ctx, KindStream, "upstream_error: EOF")

	kind, detail, ok := Get(ctx)
	if !ok || kind != KindStream || detail != "upstream_error: EOF" {
		t.Fatalf("归因未写入：ok=%v kind=%s detail=%q", ok, kind, detail)
	}

	// 没有持有者时是空操作：不能 panic，也不能凭空造出归因。
	Set(context.Background(), KindTimeout, "x")
	if _, _, ok := Get(context.Background()); ok {
		t.Fatal("没有持有者时不该读到归因")
	}
}

func TestSetTruncatesDetail(t *testing.T) {
	ctx := WithHolder(context.Background())
	Set(ctx, KindUpstream, strings.Repeat("x", 1000))

	_, detail, _ := Get(ctx)
	if len([]rune(detail)) != DetailLimit+1 {
		t.Fatalf("detail 应在写入时截断，实际长度 %d", len([]rune(detail)))
	}
}

// TestSignatureIgnoresPerRequestFields 守住聚合的核心语义：
// 只有「方法 / 路由 / 状态码 / 分类」参与签名，其余字段逐请求都不同，
// 一旦混进签名，每次请求都会被当成一种新错误。
func TestSignatureIgnoresPerRequestFields(t *testing.T) {
	base := Entry{
		TS: time.Unix(1, 0), Instance: "gw-1", RequestID: "req-a",
		Method: "GET", Route: "/v1/chat/completions", Status: 429,
		DurationMS: 3, Kind: KindOverload, Detail: "rate limited",
		TenantID: 7, KeyID: 9,
	}
	same := base
	same.TS = time.Unix(999, 0)
	same.Instance = "gw-2"
	same.RequestID = "req-b"
	same.DurationMS = 5000
	same.Detail = "rate limited (reject reason = tenant)"
	same.TenantID = 8
	same.KeyID = 10

	if Signature(base) != Signature(same) {
		t.Fatalf("同一种错误必须同签名：%q vs %q", Signature(base), Signature(same))
	}

	// 四个维度任一不同就是另一种错误
	other := base
	other.Kind = KindTimeout
	if Signature(other) == Signature(base) {
		t.Error("kind 不同应算不同错误")
	}
	other = base
	other.Status = 503
	if Signature(other) == Signature(base) {
		t.Error("status 不同应算不同错误")
	}
	other = base
	other.Route = "/slow"
	if Signature(other) == Signature(base) {
		t.Error("route 不同应算不同错误")
	}
	other = base
	other.Method = "POST"
	if Signature(other) == Signature(base) {
		t.Error("method 不同应算不同错误")
	}
}

// newTestRedis 返回一个可用客户端；没有 Redis 环境时跳过用例。
// 用法同 internal/quota：REDIS_ADDR=127.0.0.1:6380 REDIS_PASSWORD=flowgate go test ./internal/errorlog -v
// （compose 把 Redis 的 6379 映射到宿主机 6380，所以本地一般用 6380。）
func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR 未设置，跳过 Redis 集成测试")
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: os.Getenv("REDIS_PASSWORD"),
		// 连不上就快速跳过：默认的 3 次重试 + 长超时会让 skip 本身耗掉几秒。
		MaxRetries:  -1,
		DialTimeout: 500 * time.Millisecond,
	})
	t.Cleanup(func() { rdb.Close() })
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis 不可用（%s）：%v", addr, err)
	}
	return rdb
}

// testKey 给每个用例独立的 key 前缀，避免和正在跑的 gateway 抢 flowgate:errors；
// 用完连三个 key 一起删（索引 / 次数 / meta）。
func testKey(t *testing.T, rdb *redis.Client) string {
	t.Helper()
	key := "flowgate:errors:test:" + t.Name()
	t.Cleanup(func() {
		ctx := context.Background()
		rdb.Del(ctx, key, key+countSuffix, key+metaSuffix)
	})
	return key
}

// TestRedisSinkAggregatesIntegration 验证存储层聚合：
// N 次同种错误 → 1 行，count = N，meta 是最新一次的值。
func TestRedisSinkAggregatesIntegration(t *testing.T) {
	rdb := newTestRedis(t)
	key := testKey(t, rdb)
	ctx := context.Background()

	sink := NewRedisSink(rdb, key, Cap)
	now := time.Now()
	for i := 0; i < 5; i++ {
		sink.Push(ctx, Entry{
			TS: now.Add(time.Duration(i) * time.Second), RequestID: "req-" + string(rune('a'+i)),
			Method: "GET", Route: "/hello", Status: 503, Kind: KindOverload,
			Detail: "overload: fail_fast",
		})
	}

	entries, err := NewRedisReader(rdb, key, Cap).List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("5 次同种错误应聚合成 1 行，实际 %d 行：%+v", len(entries), entries)
	}
	if entries[0].Count != 5 {
		t.Fatalf("count 应为 5，实际 %d", entries[0].Count)
	}
	// meta 覆盖为最近一次：RequestID / TS 应来自第 5 次
	if entries[0].RequestID != "req-e" {
		t.Errorf("request_id 应保留最近一次的值 req-e，实际 %q", entries[0].RequestID)
	}
	if !entries[0].TS.Equal(now.Add(4 * time.Second)) {
		t.Errorf("ts 应为最近一次的 %s，实际 %s", now.Add(4*time.Second), entries[0].TS)
	}
}

// TestRedisSinkOrdersByRecencyAndTrimsIntegration 验证两件事：
//  1. 读取顺序按「最近发生时间」倒序，而不是按分类或首次出现时间；
//  2. 超过 limit 时裁掉最久没发生的种类 —— 旧错误的「次数」不会永久占住窗口。
func TestRedisSinkOrdersByRecencyAndTrimsIntegration(t *testing.T) {
	rdb := newTestRedis(t)
	key := testKey(t, rdb)
	ctx := context.Background()

	sink := NewRedisSink(rdb, key, 2)
	now := time.Now()
	push := func(route string, at time.Time) {
		t.Helper()
		if err := sink.Push(ctx, Entry{
			TS: at, Route: route, Method: "GET", Status: 503, Kind: KindUpstream,
		}); err != nil {
			t.Fatalf("push %s: %v", route, err)
		}
	}

	push("/a", now)
	push("/b", now.Add(time.Second))
	push("/a", now.Add(2*time.Second)) // /a 重新活跃，排序前移
	push("/c", now.Add(3*time.Second)) // 触发裁剪：只剩最近 2 种

	entries, err := NewRedisReader(rdb, key, Cap).List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("limit=2 应只保留 2 种错误，实际 %d：%+v", len(entries), entries)
	}
	if entries[0].Route != "/c" || entries[1].Route != "/a" {
		t.Fatalf("应按最近发生时间倒序为 [/c /a]，实际 [%s %s]", entries[0].Route, entries[1].Route)
	}
	if entries[1].Count != 2 {
		t.Errorf("/a 发生过 2 次，count 应为 2，实际 %d", entries[1].Count)
	}
}

// TestRedisReaderEmptyIntegration 空窗口应返回空切片（JSON 序列化成 []），不能是 nil。
func TestRedisReaderEmptyIntegration(t *testing.T) {
	rdb := newTestRedis(t)
	key := testKey(t, rdb)

	entries, err := NewRedisReader(rdb, key, Cap).List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if entries == nil {
		t.Fatal("空窗口应返回空切片")
	}
	if len(entries) != 0 {
		t.Fatalf("空窗口不该有数据，实际 %d 行", len(entries))
	}
}

// TestResetLegacyIntegration 守住升级路径：旧版本的 LIST 结构如果不清理，
// 新版本的 ZADD 会一直 WRONGTYPE 失败，看板静默空白。
func TestResetLegacyIntegration(t *testing.T) {
	rdb := newTestRedis(t)
	key := testKey(t, rdb)
	ctx := context.Background()

	// 模拟旧版本遗留：key 是 LIST
	if err := rdb.LPush(ctx, key, `{"status":502}`).Err(); err != nil {
		t.Fatalf("准备旧数据: %v", err)
	}
	if err := ResetLegacy(ctx, rdb, key); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if typ, _ := rdb.Type(ctx, key).Result(); typ != "none" {
		t.Fatalf("旧 LIST 应被删除，实际类型 %s", typ)
	}

	// 清理后写入必须成功（这是整个修复的意义）
	if err := NewRedisSink(rdb, key, Cap).Push(ctx, Entry{
		TS: time.Now(), Method: "GET", Route: "/hello", Status: 502, Kind: KindUpstream,
	}); err != nil {
		t.Fatalf("reset 之后写入不应失败: %v", err)
	}

	// 类型正确的 key 是空操作：不能被误删
	if err := ResetLegacy(ctx, rdb, key); err != nil {
		t.Fatalf("第二次 reset: %v", err)
	}
	entries, err := NewRedisReader(rdb, key, Cap).List(ctx)
	if err != nil || len(entries) != 1 {
		t.Fatalf("类型正确时不该动数据：err=%v entries=%+v", err, entries)
	}
}
