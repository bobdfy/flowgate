package ai

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/bobdfy/flowgate/internal/observability"
	"github.com/bobdfy/flowgate/internal/sse"
)

// StreamEventSink 是流式转发过程中对每个完整事件的处理钩子。
type StreamEventSink func(event []byte, isLast bool) []byte

// StreamingForwarder 是流式转发函数。
// 职责：把上游 body 逐块读出 → 过 internal/sse 分帧 → 逐事件 Flush 给客户端。
type StreamingForwarder func(w http.ResponseWriter, r *http.Request, upstream io.ReadCloser, sink StreamEventSink, model string) error

// streamBufSize 是每次从上游读取的缓冲区大小。
const streamBufSize = 4096

// streamProxy 是 StreamingForwarder 的默认实现。
//  1. 分帧 2. 实时 3. 取消
func streamProxy(w http.ResponseWriter, r *http.Request, upstream io.ReadCloser, sink StreamEventSink, model string) error {
	rc := http.NewResponseController(w)

	framer := sse.NewFramer()

	//  打点用的变量。
	start := time.Now()
	firstEventAt := time.Time{}
	totalEvents := 0

	buf := make([]byte, streamBufSize)
	for {
		n, err := upstream.Read(buf)
		if n > 0 {
			for _, event := range framer.Frame(buf[:n]) {
				if werr := writeEvent(rc, w, event, false, sink, &totalEvents); werr != nil {
					slog.Info("stream_client_gone",
						"err", werr,
						"events_sent", totalEvents,
						"duration_ms", time.Since(start).Milliseconds())
					return werr
				}
				if firstEventAt.IsZero() {
					firstEventAt = time.Now()
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if tail := framer.Flush(); len(tail) > 0 {
					if werr := writeEvent(rc, w, tail, true, sink, &totalEvents); werr != nil {

						return werr
					}
					if firstEventAt.IsZero() {
						firstEventAt = time.Now()
					}
				}
				break
			}
			slog.Warn("stream_upstream_error",
				"err", err,
				"events_sent", totalEvents,
				"duration_ms", time.Since(start).Milliseconds())
			return err
		}
	}

	if !firstEventAt.IsZero() {
		// C7：model 标签填真实模型名，别恒为 "stream"，否则无法按模型拆 TTFT。
		observability.AIStreamTTFT.WithLabelValues(model).
			Observe(firstEventAt.Sub(start).Seconds())
	}
	observability.AIStreamDuration.Observe(time.Since(start).Seconds())
	observability.AIStreamEventsTotal.Add(float64(totalEvents))

	// 分帧器进过 RESYNC 说明上游发过畸形数据（单个事件超 1 MiB）。
	// 正常上游这个数应该永远是 0，值得告警。
	if n := framer.OverflowCount(); n > 0 {
		slog.Warn("stream_framer_overflow",
			"overflow_count", n,
			"events_sent", totalEvents)
	}

	return nil

}

// writeEvent 把一个事件写给客户端，并立刻 Flush。
//
// 四步的顺序不能变：过 sink → 写内容 → 写分隔符 → Flush。
func writeEvent(rc *http.ResponseController, w http.ResponseWriter, event []byte, isLast bool, sink StreamEventSink, totalEvents *int) error {
	// 1. 过钩子（抽 usage / 改写）。
	if sink != nil {
		event = sink(event, isLast)
	}
	// 钩子返回 nil 表示丢弃这个事件。
	//
	// 但末块事件不丢：它是 Flush 交出来的尾巴，
	if len(event) == 0 && !isLast {
		return nil
	}

	// 2. 写事件内容。
	if _, err := w.Write(event); err != nil {
		return err
	}

	// 3. 补回分隔符。
	if _, err := w.Write([]byte("\n\n")); err != nil {
		return err
	}

	// 4. 立刻 Flush，而且要在做别的事之前。
	if err := rc.Flush(); err != nil {
		return err
	}

	*totalEvents++
	return nil
}

// prepareStreamHeaders 设置流式响应头。
func prepareStreamHeaders(w http.ResponseWriter, upstream *http.Response) {
	copySafeHeaders(w.Header(), upstream.Header)

	h := w.Header()
	h.Del("Content-Length")

	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
}
