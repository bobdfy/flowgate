package sse

import "slices"

const (
	// maxIncompleteEventBytes 是单个未完成事件的上限（1 MiB）。
	// 超过则丢弃该后缀并进入 RESYNC
	maxIncompleteEventBytes = 1 << 20

	// maxDelimiterBytes 最长分隔符"\r\n\r\n"
	maxDelimiterBytes = 4
)

// Framer 是一个请求级的 SSE 事件分帧器
// 每个流式请求一个实例, 且不可并发使用
type Framer struct {
	tail          []byte // 未完成的事件的原始字节
	resyncing     bool   // 为 true 时, 处于RESYNC: 正在丢弃字节直到拍下一个分隔符
	resyncCarry   []byte // RESYNC 状态下 <= 3 字节的分隔符匹配状态
	overflowCount int    // 有界诊断: 进入 RESYNC 的次数
}

// NewFramer 创建一个空的 Framer
func NewFramer() *Framer {
	return &Framer{}
}

// Frame 送入一段字节，返回本次能确定的完整事件。
func (f *Framer) Frame(chunk []byte) (events [][]byte) {
	// 阶段A
	if f.resyncing {
		s := sseByteWindow{a: f.resyncCarry, b: chunk}
		ends := s.scanEventEnds(1)
		if len(ends) == 0 {
			f.resyncCarry = s.lastBytes(0, maxDelimiterBytes-1)
			return nil
		}

		end := ends[0]
		f.resyncing = false
		f.resyncCarry = nil
		f.tail = nil
		if end > len(s.a) {
			chunk = chunk[end-len(s.a):]
		}
	}

	// 阶段B
	s := sseByteWindow{a: f.tail, b: chunk}
	ends := s.scanEventEnds(0)
	n := s.len()
	prev := 0
	for _, end := range ends {
		var event []byte
		if prev >= len(f.tail) {
			event = append([]byte(nil), chunk[prev-len(f.tail):end-len(f.tail)]...)
		} else {
			event = s.sliceRange(prev, end)
		}
		events = append(events, normalizeEvent(event))
		prev = end
	}

	// suffixLen 表示一个事件之后还剩多少个字节
	switch suffixLen := n - prev; {
	case suffixLen == 0:
		f.tail = nil
	case suffixLen <= maxIncompleteEventBytes:
		f.tail = s.sliceRange(prev, n)
	default:
		f.tail = nil
		f.resyncing = true
		f.resyncCarry = s.lastBytes(prev, maxDelimiterBytes-1)
		f.overflowCount++
	}

	return events

}

// Flush 在 isLastChunk 时调用：把保留的未终止尾部作为一个事件发射，
// 并重置分帧器状态。
func (f *Framer) Flush() []byte {
	tail := f.tail
	f.tail = nil
	f.resyncing = false
	f.resyncCarry = nil
	if len(tail) == 0 {
		return nil
	}
	return normalizeEvent(tail)
}

// OverflowCount 返回进入 RESYNC 的次数（诊断用）。
func (f *Framer) OverflowCount() int {
	return f.overflowCount
}

type sseByteWindow struct {
	a []byte
	b []byte
}

func (s sseByteWindow) len() int {
	return len(s.a) + len(s.b)
}

func (s sseByteWindow) at(i int) byte {
	if i < len(s.a) {
		return s.a[i]
	}
	return s.b[i-len(s.a)]
}

// lineEndingEnd 给从偏移 i 开始的行尾分类（i 处必须是 '\r' 或 '\n'），
// 返回"刚好越过这个行尾"的偏移：
//
//	'\n'                -> i+1（LF）
//	'\r' + '\n'         -> i+2（CRLF）
//	'\r' + 其他任意字节   -> i+1（裸 CR，SSE 规范允许的第三种行尾）
func (s sseByteWindow) lineEndingEnd(i int) (end int, pending bool) {
	if s.at(i) == '\n' {
		return i + 1, false
	}
	if i+1 == s.len() {
		return i, true
	}
	if s.at(i+1) == '\n' {
		return i + 2, false
	}
	return i + 1, false
}

// scanEventEnds 从偏移 0 开始扫描窗口，返回每个完整事件的结束偏移
// 一个事件分隔符 = 两个连续的行尾。要识别的形式：
//
//	"\n\n"  "\r\n\r\n"  "\n\r\n"  "\r\n\n"  以及裸 CR 参与的混合形式
//
// limit > 0 时最多返回 limit 个偏移（RESYNC 只需要找第一个分隔符）。
// 可能以 pending 的 '\r' 结尾（它的归属要看下一个 chunk）。
func (s sseByteWindow) scanEventEnds(limit int) []int {
	var ends []int
	n := s.len()
	pos := 0
	for pos < n {
		c := s.at(pos) // 判断当前偏移是否是\n \r
		if c != '\n' && c != '\r' {
			pos++
			continue
		}
		firstEnd, pending := s.lineEndingEnd(pos)
		if pending {
			// 必须 break 不能 continue ——
			// 这个 CR 还没定性，把它留在窗口里当后缀。
			break
		}
		if firstEnd < n {
			if c2 := s.at(firstEnd); c2 == '\n' || c2 == '\r' {
				secondEnd, pending := s.lineEndingEnd(firstEnd)
				if pending {
					break
				}
				// 两个连续行尾 = 事件分隔符。
				ends = append(ends, secondEnd)
				if limit > 0 && len(ends) >= limit {
					return ends
				}
				pos = secondEnd
				continue
			}
		}
		pos = firstEnd
	}
	return ends
}

// sliceRange 返回 w[from:to] 的一份新拷贝，用于保留的 tail、
// RESYNC 的 carry，以及跨边界的事件。
// 结果绝不能 alias 调用方的 chunk（宿主会复用那个缓冲区）。
func (s sseByteWindow) sliceRange(from, to int) []byte {
	out := make([]byte, 0, to-from)
	if from >= len(s.a) {
		return append(out, s.b[from-len(s.a):to-len(s.a)]...)
	}
	if to <= len(s.a) {
		return append(out, s.a[from:to]...)
	}
	out = append(out, s.a[from:]...)
	return append(out, s.b[:to-len(s.a)]...)
}

// lastBytes 返回 w[from:] 的最后 min(k, 剩余长度) 个字节的新拷贝。
// 用来从被丢弃的后缀（或还没匹配上的 carry‖chunk）里提取
// <= 3 字节的 RESYNC 匹配状态。
func (s sseByteWindow) lastBytes(from, k int) []byte {
	n := s.len()
	start := max(n-k, from)
	return s.sliceRange(start, n)
}

// normalizeEvent 把事件的行尾统一成 LF，并剥掉末尾的空行。
// 注意这里是"逐字节扫描"而不是 strings.ReplaceAll，
// 因为只处理 \r\n 与裸 \r 两种需要转换的情形，且要避免为每次替换分配新串。
func normalizeEvent(ev []byte) []byte {
	// 快速路径：绝大多数事件里没有 '\r'，直接原地修掉末尾换行即可。
	hasCR := slices.Contains(ev, '\r')

	var out []byte
	if hasCR {
		out = make([]byte, 0, len(ev))
		for i := range ev {
			if ev[i] != '\r' {
				out = append(out, ev[i])
				continue
			}
			// '\r' 后面跟 '\n' 就吃掉这个 '\r'（CRLF -> LF）；
			// 裸 '\r' 保留成 '\n'（裸 CR 也是合法行尾）。
			if i+1 < len(ev) && ev[i+1] == '\n' {
				continue
			}
			out = append(out, '\n')
		}
	} else {
		out = ev
	}

	// 剥掉末尾的空行（分隔符）。一个事件可能以 \n\n 结束，
	// 剥干净后尾部不该残留换行 —— 调用方写出时要自己补回 \n\n。
	for len(out) > 0 && (out[len(out)-1] == '\n') {
		out = out[:len(out)-1]
	}
	return out
}
