package logsink

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingWriter 是测试用的下游：可选地阻塞在第一次 Write 上，
// 用来验证队列满时的丢弃策略。
type recordingWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	writes  int
	block   chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	if w.block != nil {
		w.once.Do(func() { close(w.entered) })
		<-w.block
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes++
	return w.buf.Write(p)
}

// 并发写入：所有记录完整落盘、不交错、不丢失。
func TestConcurrentWritesNoLoss(t *testing.T) {
	var out bytes.Buffer
	// 队列容量大于总写入量，确保本用例只验证并发正确性，不触发丢弃。
	s := New(&out, 64*1024, 65536, 5*time.Millisecond)
	defer s.Close()

	const goroutines, perG = 16, 1000
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				fmt.Fprintf(s, "g%02d-%04d\n", g, i)
			}
		}(g)
	}
	wg.Wait()

	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := s.Dropped(); got != 0 {
		t.Fatalf("不应有丢弃, got=%d", got)
	}

	text := strings.TrimSuffix(out.String(), "\n")
	if text == "" {
		t.Fatal("输出为空")
	}
	lines := strings.Split(text, "\n")
	if len(lines) != goroutines*perG {
		t.Fatalf("行数不对, want=%d got=%d", goroutines*perG, len(lines))
	}
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		// 记录被截断或与其他记录交错时，长度和格式都会变。
		if len(line) != len("g00-0000") || line[0] != 'g' || line[3] != '-' {
			t.Fatalf("记录被截断或交错: %q", line)
		}
		seen[line] = struct{}{}
	}
	if len(seen) != goroutines*perG {
		t.Fatalf("出现重复或缺失记录, distinct=%d", len(seen))
	}
}

// Write 必须拷贝入参：slog 返回后会复用缓冲区。
func TestWriteCopiesInput(t *testing.T) {
	var out bytes.Buffer
	s := New(&out, 1<<10, 16, time.Hour)

	p := []byte("hello\n")
	if _, err := s.Write(p); err != nil {
		t.Fatalf("write: %v", err)
	}
	copy(p, "XXXXX\n") // 模拟 slog 复用缓冲区

	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := out.String(); got != "hello\n" {
		t.Fatalf("Write 未拷贝入参, got=%q", got)
	}
}

// 下游阻塞 + 队列很小：Write 仍应立即返回，多出的记录计入 dropped。
func TestQueueFullDropsNonBlocking(t *testing.T) {
	w := &recordingWriter{block: make(chan struct{}), entered: make(chan struct{})}
	// bufSize=8 < 单条记录长度 → 第一条会直接透传下游并阻塞在那里。
	s := New(w, 8, 1, time.Hour)

	s.Write([]byte("0123456789abcdef"))
	select {
	case <-w.entered:
	case <-time.After(time.Second):
		t.Fatal("下游未收到第一条记录")
	}

	s.Write([]byte("second-record!!!")) // 填满容量为 1 的队列

	const extra = 100
	start := time.Now()
	for i := 0; i < extra; i++ {
		n, err := s.Write([]byte("overflow-record!"))
		if err != nil || n != len("overflow-record!") {
			t.Fatalf("Write 应始终返回成功, n=%d err=%v", n, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("队列满时 Write 不应阻塞, 耗时 %v", elapsed)
	}
	if got := s.Dropped(); got < extra {
		t.Fatalf("应至少丢弃 %d 条, got=%d", extra, got)
	}

	close(w.block)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// Close 排干已入队记录，且可重复调用。
func TestCloseDrainsAndIsIdempotent(t *testing.T) {
	var out bytes.Buffer
	s := New(&out, 1<<10, 64, time.Hour)

	const n = 10
	for i := 0; i < n; i++ {
		fmt.Fprintf(s, "line-%d\n", i)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if got := s.Dropped(); got != 0 {
		t.Fatalf("正常关闭不应丢弃, got=%d", got)
	}

	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != n {
		t.Fatalf("Close 未排干队列, want=%d got=%d", n, len(lines))
	}
}

// 关闭后的写入：不 panic、不阻塞，并计入 dropped。
func TestWriteAfterClose(t *testing.T) {
	var out bytes.Buffer
	s := New(&out, 1<<10, 8, time.Hour)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	n, err := s.Write([]byte("late\n"))
	if n != len("late\n") || err != nil {
		t.Fatalf("关闭后 Write 应返回成功, n=%d err=%v", n, err)
	}
	if got := s.Dropped(); got != 1 {
		t.Fatalf("关闭后写入应计 1 次丢弃, got=%d", got)
	}
}

// Close 与并发写入竞争：不 panic、不死锁。
func TestConcurrentCloseAndWrite(t *testing.T) {
	var out bytes.Buffer
	s := New(&out, 1<<10, 1024, time.Millisecond)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				s.Write([]byte("x\n"))
			}
		}()
	}

	time.Sleep(2 * time.Millisecond)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	wg.Wait()
	if err := s.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}
