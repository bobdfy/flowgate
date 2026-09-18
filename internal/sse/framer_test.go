package sse

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// 本文件是 internal/sse 的完整测试。

// ---------------------------------------------------------------------------
// 测试辅助
// ---------------------------------------------------------------------------

// feedAll 把 chunks 依次喂给一个新建的 Framer，最后调 Flush 收尾。
//
// 每次都新建 Framer：测试之间必须完全隔离，
// 否则上一个用例残留的 tail 会污染下一个。
func feedAll(chunks ...string) [][]byte {
	f := NewFramer()
	var events [][]byte
	for _, c := range chunks {
		events = append(events, f.Frame([]byte(c))...)
	}
	if tail := f.Flush(); tail != nil {
		events = append(events, tail)
	}
	return events
}

// feedNoFlush 同上，但不 Flush。
//
// 有些用例要看"还没关流时"的行为：
// 比如"尾部残留但不是末块 → 残留必须保留，不能提前发射"。
func feedNoFlush(chunks ...string) [][]byte {
	f := NewFramer()
	var events [][]byte
	for _, c := range chunks {
		events = append(events, f.Frame([]byte(c))...)
	}
	return events
}

// want1 是只期望一个事件时的简写。
func want1(s string) [][]byte { return [][]byte{[]byte(s)} }

// ---------------------------------------------------------------------------
// 用例 1~3、6、9、10：一次调用内的常规分帧
// ---------------------------------------------------------------------------

func TestFrameBasicCases(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   [][]byte
	}{
		{
			name:   "1_空_chunk_无事件",
			chunks: []string{""},
			want:   nil,
		},
		{
			name:   "2_单个完整事件",
			chunks: []string{"data: {\"a\":1}\n\n"},
			want:   want1(`data: {"a":1}`),
		},
		{
			name:   "3_一次回调多个事件_顺序正确",
			chunks: []string{"data: {\"a\":1}\n\ndata: {\"b\":2}\n\ndata: [DONE]\n\n"},
			want: [][]byte{
				[]byte(`data: {"a":1}`),
				[]byte(`data: {"b":2}`),
				[]byte(`data: [DONE]`),
			},
		},
		{
			name:   "6_CRLF与混合分隔符",
			chunks: []string{"data: {\"a\":1}\r\n\r\ndata: {\"b\":2}\n\r\n"},
			want: [][]byte{
				[]byte(`data: {"a":1}`),
				[]byte(`data: {"b":2}`),
			},
		},
		{
			name:   "9_多行事件保留内部换行",
			chunks: []string{"event: message_start\ndata: {\"a\":1}\n\n"},
			want:   want1("event: message_start\ndata: {\"a\":1}"),
		},
		{
			// ★ 规则 7：空事件也是合法事件，framer 不吞。
			// 在 framer 里做业务过滤，等于把分帧和业务耦合在一起。
			name:   "10_事件之间的空事件_原样发射",
			chunks: []string{"\n\ndata: {\"a\":1}\n\n\n\n"},
			want: [][]byte{
				[]byte(""),
				[]byte(`data: {"a":1}`),
				[]byte(""),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := feedAll(c.chunks...)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("分帧结果不一致\n  期望: %q\n  实际: %q", c.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 用例 4：★ 黄金测试 —— 每个字节偏移处切成两半
// ---------------------------------------------------------------------------
// TestFrameSplitAtEveryByteOffset
//
// 手法：先在"不切"的情况下算出正确答案，然后把流在每一个字节偏移处
// 切成两段分别喂进去，断言结果与不切完全一致。
//
// 为什么必须这么测：真实上游怎么切字节，取决于 TCP 分段、内核缓冲区、
// 网络 MTU，你控制不了。一段 200 字节的流就有 201 种切法，
// 手工造用例一定会漏 —— 而穷举不会漏。
//
// ★ 失败信息里的偏移量是关键：它直接告诉你哪个位置的两个字节出了
// 问题，对着那个位置看就知道是哪条规则没处理对。
func TestFrameSplitAtEveryByteOffset(t *testing.T) {
	// 挑一段"内容丰富"的流：一个带 usage 的事件 + 一个终止事件
	intact := "data: {\"type\":\"message_start\",\"usage\":{\"input_tokens\":42}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	want := feedAll(intact)
	if len(want) != 2 {
		t.Fatalf("基准错了：期望 2 个事件，实际 %d 个：%q", len(want), want)
	}

	// i <= len(intact)：i == len(intact) 是"一次性全给"，也是合法切法。
	for i := 0; i <= len(intact); i++ {
		got := feedAll(intact[:i], intact[i:])
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("在第 %d 字节处切开时结果不一致\n  期望: %q\n  实际: %q\n"+
				"  该偏移附近的字节: %q",
				i, want, got, windowAround(intact, i))
		}
	}
}

// windowAround 返回 offset 附近的一小段字节，方便失败时定位。
func windowAround(s string, offset int) string {
	const pad = 6
	lo := max(offset-pad, 0)
	hi := min(offset+pad, len(s))
	return s[lo:hi]
}

// ---------------------------------------------------------------------------
// 用例 5：★ 逐字节喂
// ---------------------------------------------------------------------------
// TestFrameByteByByte 比黄金测试更狠：黄金测试只切一刀（2 段），
// 这个切 n 刀（n 段），每个字节都是一次独立调用。
//
// tail 必须跨 n 次调用正确累积 —— 这一个用例就能抓住"tail 没拷贝"的 bug。
func TestFrameByteByByte(t *testing.T) {
	event := "data: {\"type\":\"message_stop\",\"usage\":{\"output_tokens\":7}}\n\n"

	f := NewFramer()
	var events [][]byte
	for i := 0; i < len(event); i++ {
		events = append(events, f.Frame([]byte{event[i]})...)
	}
	if tail := f.Flush(); tail != nil {
		events = append(events, tail)
	}

	want := want1(`data: {"type":"message_stop","usage":{"output_tokens":7}}`)
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("逐字节喂失败\n  期望: %q\n  实际: %q", want, events)
	}
}

// ---------------------------------------------------------------------------
// 用例 7：CRLF 分隔符被切在 CR 与 LF 之间
// ---------------------------------------------------------------------------
func TestFrameCRLFDelimiterSplitBetweenCRAndLF(t *testing.T) {
	// 第一段以 "\r\n\r" 结尾 —— 最后那个 \r 是 CRLF 的前半。
	got := feedAll("data: {\"a\":1}\r\n\r", "\ndata: {\"b\":2}\n\n")
	want := [][]byte{
		[]byte(`data: {"a":1}`),
		[]byte(`data: {"b":2}`),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CRLF 被切在 CR/LF 之间时结果不一致\n  期望: %q\n  实际: %q", want, got)
	}
}

// ---------------------------------------------------------------------------
// 用例 8：★ 尾部 pending CR 不被误判
// ---------------------------------------------------------------------------
// TestFramePendingCRAtEnd 精确验证规则 3。
//
// 这个用例不能用 feedAll —— 必须分别检查两次调用的返回值：
// 第一次必须是空（CR 还没定性），第二次才出事件。
//
// ★ 如果第一次就返回了 1 个事件，说明把 pending CR 当成了裸 CR 行尾，
// 那就是 Higress 修了 6 轮里的那个 bug（Rust 版至今是反面教材）。
func TestFramePendingCRAtEnd(t *testing.T) {
	f := NewFramer()

	got := f.Frame([]byte("data: {\"a\":1}\n\r"))
	if len(got) != 0 {
		t.Fatalf("pending 的 CR 不该被定性，但返回了 %d 个事件: %q", len(got), got)
	}

	// 补上 \n，现在能确定是 CRLF 了
	got = f.Frame([]byte("\n"))
	want := want1(`data: {"a":1}`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("补上 \\n 后应该出 1 个事件\n  期望: %q\n  实际: %q", want, got)
	}
}

// ---------------------------------------------------------------------------
// 用例 11、12：末块 flush
// ---------------------------------------------------------------------------
// TestFrameFlushUnterminatedTail 验证规则 6。
//
// ★ 这是整个分帧器最不能省的一条：真实上游关流时可能不补末尾空行，
// 而那个尾巴通常正是携带 usage 的终止事件。
// 丢了它 → 功能看起来完全正常（字都出来了），只有计费数字偏小。
func TestFrameFlushUnterminatedTail(t *testing.T) {
	f := NewFramer()

	got := f.Frame([]byte("data: {\"a\":1}\n\n"))
	if !reflect.DeepEqual(got, want1(`data: {"a":1}`)) {
		t.Fatalf("第一个事件应该正常出来，实际 %q", got)
	}

	// 第二段没有末尾空行（模拟上游直接关流）
	got = f.Frame([]byte(`data: {"final":true,"usage":{"total_tokens":5}}`))
	if len(got) != 0 {
		t.Fatalf("未终止的事件不该在 Frame 里发射，实际 %q", got)
	}

	tail := f.Flush()
	want := []byte(`data: {"final":true,"usage":{"total_tokens":5}}`)
	if !bytes.Equal(tail, want) {
		t.Fatalf("Flush 应把未终止尾部作为事件发射\n  期望: %q\n  实际: %q", want, tail)
	}
}

func TestFrameFlushWithEmptyTailReturnsNil(t *testing.T) {
	f := NewFramer()

	if got := f.Frame([]byte("data: {\"a\":1}\n\n")); len(got) != 1 {
		t.Fatalf("应该出 1 个事件，实际 %q", got)
	}
	// 已经处理干净了，tail 是空的
	if tail := f.Flush(); tail != nil {
		t.Fatalf("没有残留内容时 Flush 应返回 nil，实际 %q", tail)
	}
	// 再 Flush 一次仍然是 nil（状态已重置）
	if tail := f.Flush(); tail != nil {
		t.Fatalf("第二次 Flush 应返回 nil，实际 %q", tail)
	}
}

// TestFrameTailNotFlushedBeforeLastChunk 验证"残留不能提前发射"：
// 不是末块时，不完整的尾巴必须留着，不能当成完整事件发出去。
func TestFrameTailNotFlushedBeforeLastChunk(t *testing.T) {
	got := feedNoFlush("data: {\"a\":1}\n\ndata: {\"par")
	want := want1(`data: {"a":1}`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("未关流时只应发射完整事件\n  期望: %q\n  实际: %q", want, got)
	}
}

// ---------------------------------------------------------------------------
// 用例 13、14：★ RESYNC（畸形流）
// ---------------------------------------------------------------------------
// TestFrameOversizedTailResyncs 同时验证规则 4 和规则 5。
//
// 规则 4：1 MiB 上限只作用于最后一个不完整后缀，不能撑爆内存。
// 规则 5：超限后进 RESYNC，但**同一个回调内**要继续处理后续合法事件 ——
//
//	否则一个畸形事件会连坐丢掉它后面的所有好事件。
func TestFrameOversizedTailResyncs(t *testing.T) {
	f := NewFramer()

	// 造一个超过 1 MiB、且完全没有分隔符的"事件"
	big := strings.Repeat("x", maxIncompleteEventBytes+1)
	got := f.Frame([]byte("data: " + big))

	if len(got) != 0 {
		t.Fatalf("超限的未完成事件不该被发射，实际返回 %d 个", len(got))
	}
	if !f.resyncing {
		t.Fatal("超过上限后应该进入 RESYNC")
	}
	if f.OverflowCount() != 1 {
		t.Errorf("OverflowCount = %d, 期望 1", f.OverflowCount())
	}
	// 不变量：carry 不能超过 maxDelimiterBytes-1
	if len(f.resyncCarry) > maxDelimiterBytes-1 {
		t.Errorf("resyncCarry 长度 = %d, 超过上限 %d",
			len(f.resyncCarry), maxDelimiterBytes-1)
	}

	// ★ 关键：下一个 chunk 里，分隔符之后还有一个合法事件，它必须还能出来
	got = f.Frame([]byte("tail\n\ndata: {\"ok\":1}\n\n"))
	want := want1(`data: {"ok":1}`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RESYNC 恢复失败 —— 畸形事件连坐了后面的好事件\n  期望: %q\n  实际: %q",
			want, got)
	}
	if f.resyncing {
		t.Error("找到分隔符后应该退出 RESYNC")
	}
}

// TestFrameFlushWhileResyncingEmitsNothing 验证：
// RESYNC 中的 tail 不发射（那是已判定畸形而丢弃的字节），
// carry 永远丢弃（它是分隔符状态，不是内容）。
func TestFrameFlushWhileResyncingEmitsNothing(t *testing.T) {
	f := NewFramer()

	big := strings.Repeat("x", maxIncompleteEventBytes+1)
	if got := f.Frame([]byte("data: " + big)); len(got) != 0 {
		t.Fatalf("不该有输出，实际 %q", got)
	}
	if !f.resyncing {
		t.Fatal("应该处于 RESYNC")
	}

	// 关流时还在 RESYNC：丢弃的后缀永远不该被发射
	if tail := f.Flush(); tail != nil {
		t.Fatalf("RESYNC 中 Flush 不该发射任何内容, 实际 %q", tail)
	}
	if f.resyncing {
		t.Error("Flush 必须重置 RESYNC 状态")
	}
	if f.resyncCarry != nil {
		t.Errorf("Flush 后 carry 应为 nil, 实际 %q", f.resyncCarry)
	}
}

// ---------------------------------------------------------------------------
// 用例 15：两个 Framer 互不干扰
// ---------------------------------------------------------------------------
func TestFrameTwoFramersDoNotAlias(t *testing.T) {
	f1 := NewFramer()
	f2 := NewFramer()

	// 两边各喂一个"半截事件"的开头
	if got := f1.Frame([]byte("data: {p")); len(got) != 0 {
		t.Fatalf("f1 半截事件不该有输出，实际 %q", got)
	}
	if got := f2.Frame([]byte("data: {c")); len(got) != 0 {
		t.Fatalf("f2 半截事件不该有输出，实际 %q", got)
	}

	// 各自补完。★ 如果两个实例共享了状态，这里会互相污染。
	if got := f1.Frame([]byte("1}\n\n")); !reflect.DeepEqual(got, want1(`data: {p1}`)) {
		t.Errorf("f1 结果错: %q", got)
	}
	if got := f2.Frame([]byte("2}\n\n")); !reflect.DeepEqual(got, want1(`data: {c2}`)) {
		t.Errorf("f2 结果错: %q", got)
	}
}

// ---------------------------------------------------------------------------
// 用例 16：截断尾部原样发射
// ---------------------------------------------------------------------------
// TestFrameTruncatedTailDeliveredVerbatim 验证：
// framer 不伪造内容，也不做业务过滤 —— 截断的事件原样交付，
// 由上层（解析 JSON 的那一层）决定它值不值得用。
func TestFrameTruncatedTailDeliveredVerbatim(t *testing.T) {
	f := NewFramer()

	if got := f.Frame([]byte(`data: {"truncat`)); len(got) != 0 {
		t.Fatalf("不完整的事件不该提前发射，实际 %q", got)
	}

	tail := f.Flush()
	want := []byte(`data: {"truncat`)
	if !bytes.Equal(tail, want) {
		t.Fatalf("Flush 应原样交付尾部\n  期望: %q\n  实际: %q", want, tail)
	}
}

// ---------------------------------------------------------------------------
// 规则 1 专项：tail 必须是深拷贝，不能 alias 调用方的缓冲区
// ---------------------------------------------------------------------------
// TestTailDoesNotAliasInputBuffer 专门抓"tail 没拷贝"这个 bug。
//
// 手法：准备一个共享的读缓冲区，两次"读"都复用它 ——
// 这正是 net/http resp.Body.Read 的真实行为（它复用同一个 buf）。
//
// ★ 如果实现里把 buf[:n] 直接存成 tail，第二次 copy 覆盖 buf 时
// tail 的内容就被改掉了，拼出来的事件会是错的。
//
// 这个 bug 在别处极难发现：本机小数据量下 Read 常常一次给完，
// tail 路径根本不走；只有真实网络分片才会暴露。
func TestTailDoesNotAliasInputBuffer(t *testing.T) {
	buf := make([]byte, 64) // ★ 只分配一次，两次读都复用它
	f := NewFramer()

	// 第一次"读"：写入 "data: {"，只"读"到 7 字节
	n := copy(buf, "data: {")
	if got := f.Frame(buf[:n]); len(got) != 0 {
		t.Fatalf("半截事件不该有输出, 实际 %q", got)
	}

	// 第二次"读"：★ 从 buf[0] 开始覆盖（模拟真实的循环 Read）
	//
	// 注意拼起来的内容必须是一个完整、合法的事件：
	//   "data: {" + "a1}\n\n" = "data: {a1}\n\n" → 事件是 "data: {a1}"
	//
	// 别写成 `"a\"}\n\n"` —— 那里面真的有一个双引号字符，
	// 拼出来是 `data: {a"}`，反而会让人误以为是 framer 出错。
	n = copy(buf, "a1}\n\n")
	got := f.Frame(buf[:n])

	want := want1(`data: {a1}`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tail 被读缓冲区覆盖了 —— 说明没有深拷贝\n  期望: %q\n  实际: %q",
			want, got)
	}
}

// ---------------------------------------------------------------------------
// 不变量：resyncCarry 的长度上限
// ---------------------------------------------------------------------------
// TestResyncCarryIsBounded 钉死 lastBytes 的 k 值。
//
// maxDelimiterBytes-1 = 3 是"跨边界的分隔符前一块最多留几字节"的上界：
// 最长分隔符 "\r\n\r\n" 是 4 字节，跨边界时前一块最多含前 3 个。
//
// ★ 留 4 字节（也就是漏掉那个 -1）会把完整的分隔符留在 carry 里，
// 下次扫描时会把 carry 整段丢掉 —— 功能上不一定错，但白占字节，
// 而且和结构体注释里的不变量矛盾。
func TestResyncCarryIsBounded(t *testing.T) {
	cases := []struct {
		name    string
		padding int // 超过 1 MiB 之后再多加几个字节
	}{
		{"后缀刚超上限", 0},
		{"后缀比上限长 1 字节", 1},
		{"后缀比上限长 8 字节", maxDelimiterBytes * 2},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := NewFramer()
			// 用实际数据触发上限：1 MiB+ 的无分隔符后缀
			big := strings.Repeat("y", maxIncompleteEventBytes+1+c.padding)
			if got := f.Frame([]byte(big)); len(got) != 0 {
				t.Fatalf("不该有输出，实际 %q", got)
			}
			if !f.resyncing {
				t.Fatal("应该进入 RESYNC")
			}
			if got := len(f.resyncCarry); got > maxDelimiterBytes-1 {
				t.Errorf("resyncCarry 长度 = %d, 上限应为 %d",
					got, maxDelimiterBytes-1)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 一个稍大的端到端场景（非黄金测试，但更接近真实流量）
// ---------------------------------------------------------------------------
// TestFrameRealisticStream 模拟一段真实的长流：多个事件、事件跨多次调用，
// 最后验证拼出来的事件序列完全正确。
func TestFrameRealisticStream(t *testing.T) {
	// 上游"想"发的（SSE 事件边界）
	events := []string{
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}",
		"data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}",
		"data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}",
		"data: {\"choices\":[{\"delta\":{\"content\":\"，世界\"}}]}",
		"data: {\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":4,\"total_tokens\":14}}",
		"data: [DONE]",
	}
	intact := strings.Join(events, "\n\n") + "\n\n"

	// 上游"实际"发出来的：切成不等长，且刻意切在分隔符和事件中间。
	//
	// 注意这些切点是按下面 intact 的实际内容数出来的。
	// 如果改了 events 的内容，这些下标都要重新数 ——
	// 所以这个用例只测"一个手工构造的恶劣切法"，
	// 穷举交给 TestFrameSplitAtEveryByteOffset 去做。
	chunks := []string{
		intact[0:7],   // 切在 "data: {" 中间
		intact[7:34],  // 跨过第一个 \n\n
		intact[34:71], // 切在第三个事件的 JSON 里
		intact[71:73],
		intact[73:], // 剩下的全部
	}
	// 把最后 1 字节单独切出来，让结尾是"未终止"的（后面靠 Flush 收）
	last := chunks[len(chunks)-1]
	chunks[len(chunks)-1] = last[:len(last)-1]
	chunks = append(chunks, last[len(last)-1:])

	got := feedAll(chunks...)

	want := make([][]byte, 0, len(events))
	for _, e := range events {
		want = append(want, []byte(e))
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("真实流分帧失败\n  期望 %d 个事件: %q\n  实际 %d 个事件: %q",
			len(want), want, len(got), got)
	}
}
