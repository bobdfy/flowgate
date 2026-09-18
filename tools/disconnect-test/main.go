// disconnect-test 验证"客户端中途断开 → 网关停止转发 → 上游也停止生成"。
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func main() {
	url := flag.String("url", "http://localhost:8090/v1/chat/completions", "网关的 AI 接口地址")
	key := flag.String("key", "", "X-API-Key 的值")
	model := flag.String("model", "flowgate-mock", "对外模型名")
	abortAfter := flag.Int("after", 2, "读到几个事件后主动断开")
	flag.Parse()

	if *key == "" {
		fmt.Println("必须用 -key 指定 API Key")
		return
	}

	// 关键：带 cancel 的 ctx。取消它 = 客户端主动断开连接。
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body := fmt.Sprintf(
		`{"model":%q,"stream":true,"messages":[{"role":"user","content":"hi"}]}`, *model)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, *url, strings.NewReader(body))
	if err != nil {
		fmt.Printf("造请求失败: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", *key)

	fmt.Printf("发送流式请求: %s  model=%s\n", *url, *model)
	fmt.Printf("计划: 读到 %d 个事件后主动断开 (cancel context)\n\n", *abortAfter)

	t0 := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("HTTP %d  Content-Type: %s\n", resp.StatusCode, resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		fmt.Printf("body: %s\n", string(b))
		return
	}

	// 逐行读 SSE（流式响应本来就是一行一个事件）
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	count := 0
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		count++
		elapsed := time.Since(t0)
		show := line
		if len(show) > 90 {
			show = show[:90] + "..."
		}
		fmt.Printf("  [%6dms] 事件 %d: %s\n", elapsed.Milliseconds(), count, show)

		if count >= *abortAfter {
			fmt.Printf("\n★ 读到 %d 个事件，主动断开连接 (cancel)\n", count)
			cancel() // ← 这就是"客户端提前关页面"
			break
		}
	}

	// 断开后再读一次，确认连接真的断了
	time.Sleep(300 * time.Millisecond)
	_, err = io.Copy(io.Discard, resp.Body)
	fmt.Printf("\n断开后读剩余数据: %v\n", err)

	fmt.Println("\n========================================")
	fmt.Println("现在去看这两个终端：")
	fmt.Println("  1. 网关终端   → 应该有 stream_client_gone")
	fmt.Println("  2. ★ mockai 终端 → 应该有「写事件失败（客户端可能已断开）」")
	fmt.Println("     ★ 第 2 条才是「没白烧 token」的证据")
	fmt.Println("========================================")
}
