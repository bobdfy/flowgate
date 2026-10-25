// Package logsink 提供一个并发安全的日志输出通道。
//
// 背景：日志 handler 会被所有请求 goroutine 并发调用，而 bufio.Writer
// 不是并发安全的。这里把「写」与「落盘」拆开：
//
//   - 调用方（Write）只做一次拷贝 + 非阻塞入队，永远不等待 I/O；
//   - 单个后台 goroutine 独占 bufio.Writer，负责批量写与定时 Flush。
//
// 队列满或 sink 已关闭时直接丢弃并计数，保证日志系统不会反向拖慢业务路径。
package logsink

import (
	"bufio"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bobdfy/flowgate/internal/observability"
)

const (
	defaultBufSize       = 64 * 1024
	defaultQueueCapacity = 4096
	defaultFlushInterval = 200 * time.Millisecond
)

// Sink 是并发安全的 io.Writer，内部由单个 goroutine 串行落盘。
//
// 同一实例只应有一个 Close 调用方（重复调用安全，但会等待第一次完成）。
type Sink struct {
	bw        *bufio.Writer
	ch        chan []byte
	done      chan struct{}
	exited    chan struct{}
	flushTick time.Duration

	// mu 把「判断 closed + 入队」与「置 closed」互斥：
	// Close 拿到写锁时，不可能有 writer 正卡在入队过程中，
	// 因此队列里已有的记录都能被排干，不存在漏排。
	mu     sync.RWMutex
	closed bool

	closeOnce sync.Once
	closeErr  error // 仅消费者 goroutine 写，close(exited) 之后对 Close 可见
	writeErr  error // 仅消费者 goroutine 访问
	dropped   atomic.Uint64
}

// New 创建 Sink 并启动唯一的消费者 goroutine。
// bufSize / queueCap / flushTick 非正时使用默认值（64KB / 4096 条 / 200ms）。
func New(out io.Writer, bufSize, queueCap int, flushTick time.Duration) *Sink {
	if bufSize <= 0 {
		bufSize = defaultBufSize
	}
	if queueCap <= 0 {
		queueCap = defaultQueueCapacity
	}
	if flushTick <= 0 {
		flushTick = defaultFlushInterval
	}

	s := &Sink{
		bw:        bufio.NewWriterSize(out, bufSize),
		ch:        make(chan []byte, queueCap),
		done:      make(chan struct{}),
		exited:    make(chan struct{}),
		flushTick: flushTick,
	}
	go s.run()
	return s
}

// Write 实现 io.Writer：拷贝记录后非阻塞入队，永不阻塞、永不返回错误。
// slog 在 Write 返回后会复用/改写传入切片，所以必须拷贝。
func (s *Sink) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		s.drop()
		return len(p), nil
	}

	buf := make([]byte, len(p))
	copy(buf, p)

	select {
	case s.ch <- buf:
		s.mu.RUnlock()
		return len(p), nil
	default:
		s.mu.RUnlock()
		s.drop()
		return len(p), nil
	}
}

// Dropped 返回被丢弃的日志条数（队列满、关闭后写入、下游写失败）。
func (s *Sink) Dropped() uint64 {
	return s.dropped.Load()
}

// Close 停止接收新日志，排干队列并做最后一次 Flush。可重复调用。
func (s *Sink) Close() error {
	s.closeOnce.Do(func() {
		// 先置 closed 再 close(done)：写锁与 Write 的读锁互斥，
		// 保证此后没有 writer 还在入队，队列内容即最终集合。
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()

		close(s.done)
		<-s.exited
	})
	return s.closeErr
}

func (s *Sink) drop() {
	s.dropped.Add(1)
	observability.LogDroppedTotal.Inc()
}

// run 是唯一接触 bufio.Writer 的 goroutine。
func (s *Sink) run() {
	defer close(s.exited)
	defer func() {
		observability.LogQueueDepth.Set(0)
	}()

	ticker := time.NewTicker(s.flushTick)
	defer ticker.Stop()

	for {
		select {
		case b := <-s.ch:
			s.write(b)
			observability.LogQueueDepth.Set(float64(len(s.ch)))
		case <-ticker.C:
			s.flush()
			observability.LogQueueDepth.Set(float64(len(s.ch)))
		case <-s.done:
			// 排干 Close 之前已经入队的记录。
			for {
				select {
				case b := <-s.ch:
					s.write(b)
				default:
					s.flush()
					s.closeErr = s.writeErr
					return
				}
			}
		}
	}
}

func (s *Sink) write(b []byte) {
	if _, err := s.bw.Write(b); err != nil {
		// bufio 会记住错误，后续写入会持续失败；这里只保留首个错误。
		if s.writeErr == nil {
			s.writeErr = err
		}
		s.drop()
	}
}

func (s *Sink) flush() {
	if err := s.bw.Flush(); err != nil {
		if s.writeErr == nil {
			s.writeErr = err
		}
	}
}
