package gatewayapp

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/bobdfy/flowgate/internal/logsink"
)

// SetupLogging 安装全局日志，返回需要在退出时关闭的 sink。
// logsink 用 bufio.Writer 攒批由单协程定时刷盘
// 拷贝 + 非阻塞入队，避免 bufio 的并发竞态，不被慢速 stdout 阻塞（队列满时丢弃并计数）。
func SetupLogging() *logsink.Sink {
	sink := logsink.New(os.Stdout, 64*1024, 4096, 200*time.Millisecond)
	slog.SetDefault(slog.New(slog.NewJSONHandler(sink, nil)))
	return sink
}

// Fatal 打印错误到 stderr 后退出，用于启动阶段的致命错误。
//
// 为什么不能用 log.Fatal：那时 slog 已经接到 logsink，消息进了队列，
// 而 os.Exit 不等 200ms 的 flush，结果就是"启动失败但一个字都没输出"。
func Fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// ShutdownLogging 排干队列并落盘，必须在进程真正退出前调用。
//
// 注意两点：
//  1. 当前 Go 版本里 slog.SetDefault 会把标准库 log 也桥接到 slog，
//     所以 Close 之后再 log 的内容会被丢弃（计入 dropped）；
//     退出类信息必须在 Close 之前发出，由 Close 负责刷盘。
//  2. 调用方的 deferred cancel 在这之后才会停掉健康检查等后台 goroutine，
//     它们若再写日志同样计入 dropped，不影响正确性。
func ShutdownLogging(sink *logsink.Sink) {
	if n := sink.Dropped(); n > 0 {
		slog.Warn("log_queue_overflow", "dropped", n)
	}
	slog.Info("gateway 已退出")
	if err := sink.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "日志落盘失败: %v\n", err)
	}
}
