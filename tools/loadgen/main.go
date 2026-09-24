// loadgen 是给数据面做并发验证的小工具。
// 两种用法：
//  1. 打满并发，看状态码分布
//     go run ./tools/loadgen -url http://127.0.0.1:8090/ov/ff -key sk_xxx -n 20
//  2. ★ 客户端中途断开（验证"断开不算过载"）
//     go run ./tools/loadgen -url http://127.0.0.1:8090/ov/disc -key sk_xxx \
//     -n 10 -timeout 200ms
//     -timeout 会让每个请求在 200ms 后主动 cancel context，
//     模拟"客户端提前关掉了页面"。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// result 是一次请求的观测结果。
type result struct {
	code    int // -1 = 客户端断开；-2 = 构造失败
	body    string
	retry   string
	elapsed time.Duration
}

func main() {
	url := flag.String("url", "", "要打的地址")
	key := flag.String("key", "", "X-API-Key")
	n := flag.Int("n", 10, "总共发多少个请求")
	conc := flag.Int("conc", 0, "并发数；0 = 全部同时发")
	timeout := flag.Duration("timeout", 0, "每个请求的超时；>0 时模拟客户端中途断开")
	flag.Parse()

	if *url == "" || *key == "" {
		fmt.Println("必须用 -url 和 -key")
		return
	}
	total := *n

	concurrency := *conc // 实际并发数
	if concurrency <= 0 {
		concurrency = total
	}

	client := &http.Client{
		// 别让 client 自己的超时干扰 —— 超时统一由 ctx 控制
		Transport: &http.Transport{
			MaxIdleConns:        concurrency * 2,
			MaxIdleConnsPerHost: concurrency * 2,
		},
	}

	var (
		mu      sync.Mutex
		results = make([]result, 0, total)
		wg      sync.WaitGroup
		sem     = make(chan struct{}, concurrency)
	)

	t0 := time.Now()
	for range total {
		wg.Go(func() {
			sem <- struct{}{}        // 获得一个并发名额
			defer func() { <-sem }() // 函数退出释放名额

			ctx := context.Background()
			cancel := func() {}
			if *timeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, *timeout)
			}
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, *url, nil)
			if err != nil {
				mu.Lock()
				results = append(results, result{code: -2, body: err.Error()})
				mu.Unlock()
				return
			}
			req.Header.Set("X-API-Key", *key)

			st := time.Now()
			resp, err := client.Do(req)
			el := time.Since(st)

			r := result{elapsed: el}
			if err != nil {
				// ctx 超时/取消走这里：客户端自己跑了
				r.code = -1
				r.body = err.Error()
			} else {
				defer resp.Body.Close()
				b, _ := io.ReadAll(resp.Body)
				r.code = resp.StatusCode
				r.body = string(b)
				r.retry = resp.Header.Get("Retry-After")
			}
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
		})
	}
	wg.Wait()
	wall := time.Since(t0)

	// ── 汇总 ──
	byCode := map[int][]result{}
	for _, r := range results {
		byCode[r.code] = append(byCode[r.code], r)
	}
	codes := make([]int, 0, len(byCode))
	for c := range byCode {
		codes = append(codes, c)
	}
	sort.Ints(codes)

	fmt.Printf("\n总数 %d  并发 %d  墙钟耗时 %v\n", total, concurrency, wall.Round(time.Millisecond))
	fmt.Println("────────────────────────────────────────")
	for _, c := range codes {
		rs := byCode[c]
		label := fmt.Sprintf("HTTP %d", c)
		switch c {
		case -1:
			label = "客户端断开(ctx取消)"
		case -2:
			label = "请求构造失败"
		}
		fmt.Printf("  %-20s × %-4d  min=%v max=%v\n",
			label, len(rs), minDur(rs).Round(time.Millisecond), maxDur(rs).Round(time.Millisecond))
		if c == 200 || c < 0 {
			continue
		}
		fmt.Printf("       body: %s\n", trim(rs[0].body))
		if rs[0].retry != "" {
			fmt.Printf("       Retry-After: %s\n", rs[0].retry)
		} else {
			fmt.Printf("       Retry-After: (无)\n")
		}
	}
}

// minDur 返回一组结果中最小耗时
func minDur(rs []result) time.Duration {
	m := rs[0].elapsed
	for _, r := range rs {
		if r.elapsed < m {
			m = r.elapsed
		}
	}
	return m
}

// maxDur返回结果中最大耗时
func maxDur(rs []result) time.Duration {
	m := rs[0].elapsed
	for _, r := range rs {
		if r.elapsed > m {
			m = r.elapsed
		}
	}
	return m
}

// trim 去掉换行，让 body 打印在一行里。
func trim(s string) string {
	if len(s) > 100 {
		s = s[:100] + "..."
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
