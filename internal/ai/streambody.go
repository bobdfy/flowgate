package ai

import (
	"context"
	"io"
	"sync/atomic"
	"time"
)

// streamBody 给 SSE 响应体加 [读空闲超时]，并在关闭时统一释放所有 context
type streamBody struct {
	rc      io.ReadCloser
	idle    time.Duration
	timer   *time.Timer
	cancel  context.CancelFunc
	idleHit atomic.Bool
}

func newStreamBody(rc io.ReadCloser, idle time.Duration, cancel context.CancelFunc) *streamBody {
	b := &streamBody{rc: rc, idle: idle, cancel: cancel}
	if idle > 0 {
		b.timer = time.AfterFunc(idle, func() {
			b.idleHit.Store(true)
			cancel()
		})
	}
	return b
}

func (b *streamBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 && b.timer != nil {
		b.timer.Reset(b.idle) // 只有真的读到数据才续命
	}
	return n, err
}

func (b *streamBody) Close() error {
	if b.timer != nil {
		b.timer.Stop()
	}
	err := b.rc.Close()
	b.cancel() // 释放最大时长那个 ctx
	return err
}
