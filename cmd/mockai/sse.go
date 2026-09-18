package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// 流式输出用的细节常量。
const (
	// sleepBetweenEvents 是 sse_slow 模式下事件之间的间隔。
	// 选 200ms：肉眼能明显看到"逐字出"，又不会让测试等太久。
	sleepBetweenEvents = 200 * time.Millisecond

	// sleepAfterFirstHalf 是切分模式下两次 Write 之间的间隔。
	//
	// ★ 为什么必须 sleep：即使 Flush 了，两次 Write 也可能因为内核
	// 缓冲策略被合并成一个 TCP 段。加一点点延迟能确保它们真的分成两段发出去。
	// 不分段的话，网关那边一次 Read 就拿到完整事件，
	// 分帧器的 tail 拼接路径根本不会被触发 —— 测试就白做了。
	sleepAfterFirstHalf = 15 * time.Millisecond

	// oversizeGarbageBytes 是 sse_oversize 模式注入的垃圾字节数。
	//
	// 比网关分帧器的 1 MiB 上限多一点，确保能触发 RESYNC。
	oversizeGarbageBytes = 1<<20 + 1024
)

// handleStream 处理请求体里 stream=true 的情况，按模式输出 SSE。
func handleStream(w http.ResponseWriter, r *http.Request, modes *modeStore, logger *log.Logger, model string, reqBody []byte) {
	// ★ 必须先把流式响应头设好再 WriteHeader。
	//
	// 另外要删掉 Content-Length —— 我们是边生成边写，
	// 长度未知（Go 会自动用 chunked 传输编码）。
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Del("Content-Length")

	rc := http.NewResponseController(w)
	w.WriteHeader(http.StatusOK)

	// 组装要发的 SSE 事件。
	//
	// ★ usage 事件放在最后 —— 这是 OpenAI 的做法，
	//   也是"末块 flush 不能省"那条规则要保护的东西。
	usageEvent := fmt.Sprintf(
		`data: {"id":"chatcmpl-mockai-1","object":"chat.completion.chunk","model":%q,`+
			`"choices":[],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d}}`,
		model,
		estimateTokens(promptOf(reqBody)),
		estimateTokens(mockAnswer),
		estimateTokens(promptOf(reqBody))+estimateTokens(mockAnswer),
	)

	events := []struct {
		text string
		// isLast 为 true 的事件在 sse_no_tail 模式下会省掉末尾空行
		isLast bool
	}{
		{text: `data: {"id":"chatcmpl-mockai-1","object":"chat.completion.chunk","model":"` + model + `","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`},
		{text: `data: {"id":"chatcmpl-mockai-1","object":"chat.completion.chunk","model":"` + model + `","choices":[{"index":0,"delta":{"content":"你"},"finish_reason":null}]}`},
		{text: `data: {"id":"chatcmpl-mockai-1","object":"chat.completion.chunk","model":"` + model + `","choices":[{"index":0,"delta":{"content":"好"},"finish_reason":null}]}`},
		{text: `data: {"id":"chatcmpl-mockai-1","object":"chat.completion.chunk","model":"` + model + `","choices":[{"index":0,"delta":{"content":"，世界"},"finish_reason":null}]}`},
		{text: `data: {"id":"chatcmpl-mockai-1","object":"chat.completion.chunk","model":"` + model + `","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`},
		{text: usageEvent},
		{text: "data: [DONE]", isLast: true},
	}

	mode := modes.get()

	// sse_oversize：先发一段超大垃圾（没有分隔符），再发正常事件。
	// 目的是触发网关分帧器的 RESYNC，并验证"不连坐后面的好事件"。
	if mode == modeSSEOversize {
		if err := writeRaw(w, rc, "data: "+strings.Repeat("x", oversizeGarbageBytes)); err != nil {
			logger.Printf("oversize 写入失败: %v", err)
			return
		}
	}

	for i, ev := range events {
		// 组装这一个事件的完整字节（含分隔符）。
		var payload string
		switch {
		case mode == modeSSENoTail && ev.isLast:
			// ★ 故意不补末尾空行 —— 模拟真实上游（比如 vertex）
			//   关流时不补空行。网关的 framer.Flush() 必须把这个尾巴发射出去，
			//   否则丢的就是最后那条带 usage 的事件。
			payload = ev.text
		case mode == modeSSEDelim:
			// ★ 用 \r\n\r\n 作分隔符，而且故意切在 CR 和 LF 之间。
			payload = ev.text + "\r\n\r\n"
		default:
			// 注意：这里用 "\n\n" 而不是 "\r\n\r\n"，
			// 和 OpenAI 的实际行为一致。
			payload = ev.text + "\n\n"
		}

		var err error
		switch mode {
		case modeSSESplit:
			err = writeSplit(w, rc, payload)
		case modeSSEDelim:
			err = writeDelimSplit(w, rc, payload)
		case modeSSESlow:
			err = writeRaw(w, rc, payload)
			if err == nil && i < len(events)-1 {
				time.Sleep(sleepBetweenEvents)
			}
		case modeSSEAbort:
			// 发到一半直接返回（连接会被 net/http 关闭）。
			if i == 3 {
				logger.Printf("sse_abort: 在第 %d 个事件后中断连接", i+1)
				panic(http.ErrAbortHandler) // net/http 的惯用法：静默中断，不打堆栈
			}
			err = writeRaw(w, rc, payload)
		default: // sse_normal / sse_no_tail / sse_oversize 的后续事件
			err = writeRaw(w, rc, payload)
		}

		if err != nil {
			// 写失败 = 客户端断了。直接停，不再往下发。
			logger.Printf("写事件失败（客户端可能已断开）: %v", err)
			return
		}
	}

	logger.Printf("流式发送完成 mode=%s events=%d", mode, len(events))
}

// writeRaw 写一段字节并 Flush。
func writeRaw(w http.ResponseWriter, rc *http.ResponseController, s string) error {
	if _, err := w.Write([]byte(s)); err != nil {
		return err
	}
	return rc.Flush()
}

// writeSplit 把一个事件切成两半，中间 Flush + sleep 再发第二半。
//
// ★ 这是 sse_split 模式的核心：模拟"一个 SSE 事件被 TCP 切成两个包"。
// 网关那边必须靠 Framer 的 tail 缓冲把两半拼回来。
func writeSplit(w http.ResponseWriter, rc *http.ResponseController, payload string) error {
	half := len(payload) / 2
	if half == 0 {
		return writeRaw(w, rc, payload)
	}

	if _, err := w.Write([]byte(payload[:half])); err != nil {
		return err
	}
	if err := rc.Flush(); err != nil {
		return err
	}
	time.Sleep(sleepAfterFirstHalf)

	if _, err := w.Write([]byte(payload[half:])); err != nil {
		return err
	}
	return rc.Flush()
}

// writeDelimSplit 把分隔符 \r\n\r\n 切在 CR 和 LF 之间发出去。
//
// ★ 这是 sse_delim 模式的核心，专门测分帧器的 pending CR 逻辑：
//
//	第一次 Write 以孤立的 '\r' 结尾 —— 这个 CR 还无法定性
//	（它可能是 CRLF 的前半）。分帧器必须把它留在 tail 里，
//	等下一块的第一个字节到了再判断。
//
// 如果分帧器提前认定它是裸 CR 行尾，就会吐出一个空事件、并把边界搞错。
//
// 实现：payload 形如 "...data: {...}\r\n\r\n"，
// 我们从末尾去掉 2 个字节（"\r\n"），先发剩下的（以 '\r' 结尾），
// sleep，再把 "\r\n" 发出去。
func writeDelimSplit(w http.ResponseWriter, rc *http.ResponseController, payload string) error {
	if len(payload) < 4 {
		return writeRaw(w, rc, payload)
	}

	// 剩下最后 2 字节 "\r\n" 不在这里发
	first := payload[:len(payload)-2] // 以 '\r' 结尾
	second := payload[len(payload)-2:]

	if _, err := w.Write([]byte(first)); err != nil {
		return err
	}
	if err := rc.Flush(); err != nil {
		return err
	}
	time.Sleep(sleepAfterFirstHalf)

	if _, err := w.Write([]byte(second)); err != nil {
		return err
	}
	return rc.Flush()
}
