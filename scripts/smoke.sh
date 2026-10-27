#!/usr/bin/env bash
# ============================================================================
# smoke.sh —— 部署之后的冒烟检查：一条命令验证关键链路通不通
#
# 跑法：
#   FLOWGATE_KEY=fg-xxx bash scripts/smoke.sh
#   FLOWGATE_KEY=fg-xxx FLOWGATE_ROUTE=/v1/chat FLOWGATE_MODEL=qwen-plus bash scripts/smoke.sh
#
# 它检查的是「部署通不通」：环境变量、迁移、端口、admin→gateway 配置下发。
# 并发 / 取消 / 熔断这类逻辑正确性由 go test 覆盖（internal/gateway/integration_test.go）。
# ============================================================================

# 【1】这一行叫 shebang（读作 "shee-bang"）：告诉操作系统「用哪个程序来跑这个文件」。
#      env bash 的意思是「去 PATH 里找一个叫 bash 的」，这样在 Linux / macOS / Git Bash
#      上都能用，不会写死成 /bin/bash 那种在某台机器上不存在的位置。
#      它必须是文件的第一行、第一个字符。


# 【2】set 三个开关，写脚本必开。逐个说：
set -euo pipefail
#   -e  ：任何一条命令失败（退出码非 0），脚本立刻停下。
#         不开的话，中间某步失败了脚本还傻乎乎往下跑，最后报个不相干的错。
#   -u  ：用到没定义的变量就报错。
#         不开的话，$FLOWGATE_KEy（把 K 打错了）会静默变成空字符串，
#         然后你带着一个空 key 跑出十几个莫名其妙的 401。
#   -o pipefail ：管道 a | b | c 里只要有一环失败就算失败。
#         默认只看最后一环 —— grep 找不到东西返回 1，但前面 curl 崩了它也看不见。


# 【3】变量赋值。写法是 名字=值，等号两边【不能有空格】。
#     ${VAR:-默认值} = 如果 VAR 没设或为空，就用默认值。
BASE=${FLOWGATE_BASE:-http://127.0.0.1:8090}
METRICS=${FLOWGATE_METRICS:-http://127.0.0.1:9090}
ROUTE=${FLOWGATE_ROUTE:-/hello}
MODEL=${FLOWGATE_MODEL:-}

# 【4】${VAR:?提示} = 如果 VAR 没设，打印这句提示并退出。
#     为什么要这样：key 绝不能写死在脚本里，写死 = 又把密钥提交进仓库一次。
#
#     两种传法都支持：
#       bash scripts/smoke.sh fg-xxx                ← 直接传参，PowerShell 里也能用
#       FLOWGATE_KEY=fg-xxx bash scripts/smoke.sh   ← 环境变量（仅 bash）
#     ${1:-} 是「第一个参数，没有就当空」；-n 判断非空。
if [[ -n "${1:-}" ]]; then
  KEY=$1
else
  KEY=${FLOWGATE_KEY:?请用第一个参数传 key，或设置 FLOWGATE_KEY（管理面建一个测试 key）}
fi


# 【5】变量赋值时等号右边如果有空格，整行会被当成「命令」而不是赋值，会报错。
#     所以数字直接写，不加引号。
pass=0
fail=0


# 【6】函数定义。check() 后面跟一对大括号，中间是函数体。
#     $1 $2 $3 是「第 1、2、3 个参数」—— shell 不写形参名，按位置取。
#     调用时写：check "描述" 期望 实际
check() {
  # 【7】if [[ 条件 ]]; then ... else ... fi
  #      [[ ]] 是 bash 的判断语法；"$2" == "$3" 是比较两个字符串是否相等。
  #      字符串比较一定要加双引号，否则空值会让语法崩掉。fi 就是 if 倒过来写，表示结束。
  if [[ "$2" == "$3" ]]; then

    # 【8】printf '格式' 参数…… 比 echo 可靠：echo 会把 "-n" 当参数，
    #      printf 不会。%s 是占位符，按顺序填后面的参数；\n 是换行。
    printf '  ok   %s\n' "$1"
    
    # 【9】$(( ... )) 是算术运算，把里面的东西当数字算。
    #      (()) 一层是「算数」，$(()) 外面再加 $ 是「取算完的值」。
    pass=$((pass + 1))
  else
    printf '  FAIL %s\n       期望: %s\n       实际: %s\n' "$1" "$2" "$3"
    fail=$((fail + 1))
  fi
}

# 【10】短函数可以写在一行里，分号 ; 代替换行：
#       say() { printf '\n%s\n' "$1"; }  意思是「打印一个空行 + 参数」
say() { printf '\n%s\n' "$1"; }

# ---------------------------------------------------------------------------
# 【11】$( 命令 ) 叫「命令替换」：先把括号里的命令跑完，把它的输出当成值。
#      所以这里 code 函数「返回」的东西，就是它打印到屏幕上的那串字符。
#      shell 里函数没法像 Go 那样 return 一个值，只能 print，外面用 $( ) 接住。
code() {
  # 【12】curl 是发 HTTP 请求的命令，这几个参数：
  #        -s                   安静模式，别打印进度条
  #        -o /dev/null         把响应正文丢掉（/dev/null 是「黑洞文件」，写进去就没了）
  #        -w '%{http_code}'    请求结束后只打印状态码
  #        --max-time 10        最多等 10 秒
  #        "$@"                 把调用者传进来的【所有】参数原样接在这里
  #                             （所以 code -H "X-API-Key: x" URL 里的 -H 会传进 curl）
  #      || 是「前面失败了才执行后面」：连不上时 curl 返回非 0，这里补一个 000，
  #      否则 set -e 会让整个脚本当场退出，你就看不到后面还有哪些失败。
  curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$@" || echo 000
}

# ===========================================================================
say "等待网关就绪 ($BASE/health)"

# 【13】for 循环：变量 _ 依次取 $(seq 30) 的每个值（1 到 30）。
#      _ 只是个名字，表示「这个值我不用」。
#      用轮询而不是 sleep 20：机器快就早走，慢也不会误判。
ready=0
for _ in $(seq 30); do
  if [[ "$(code "$BASE/health")" == "200" ]]; then
    ready=1
    # 【14】break 跳出循环；continue 是跳到下一轮。和 Go 一样。
    break
  fi
  sleep 1
done

# 【15】-ne 是「不等于」（numeric equal 的反面）；-eq 等于、-gt 大于、-lt 小于。
#      字符串比较用 == / !=，数字比较用 -eq / -ne。这是 shell 的坑之一。
if [[ $ready -ne 1 ]]; then
  # 【16】>&2 表示「把这个输出送到标准错误」而不是标准输出。
  #      错误信息走 stderr，正常结果走 stdout，这样别人把输出重定向到文件时错误还能看见。
  echo "30 秒内没等到 /health 返回 200，先看 docker compose ps 和容器日志" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
say "A 组：鉴权与转发"
check "无 key → 401" 401 "$(code "$BASE$ROUTE")"
check "错 key → 401" 401 "$(code -H 'X-API-Key: fg-not-a-real-key' "$BASE$ROUTE")"

authed=$(code -H "X-API-Key: $KEY" "$BASE$ROUTE")
# 【17】[[ 表达式 ]] && echo 1 || echo 0 是 shell 里的「三目运算符」：
#      条件成立就打印 1，否则打印 0。把布尔值变成能比较的字符串。
check "正确 key → 网关可达" 1 "$([[ "$authed" != "000" ]] && echo 1 || echo 0)"
check "正确 key → 不是 401" 1 "$([[ "$authed" != "401" ]] && echo 1 || echo 0)"
check "正确 key → 不是 5xx" 1 "$([[ "$authed" < "500" ]] && echo 1 || echo 0)"

# 【18】管道 | ：把左边命令的输出，当成右边命令的输入。
#      这一串是：发请求 → 只看响应头(-D -) → 去 Windows 的 \r → 找出 X-Request-ID 的值。
#        tr -d '\r'                  删掉所有回车符（Git Bash 下的响应头带 \r）
#        awk '... {print $2}'        按空格切分，打印第 2 列
#        tolower($1) == "x-request-id:"  第 1 列转小写后等于这个（冒号算在第 1 列里）
rid=$(curl -s -D - -o /dev/null --max-time 10 -H "X-API-Key: $KEY" "$BASE$ROUTE" \
      | tr -d '\r' | awk 'tolower($1) == "x-request-id:" {print $2}')
check "响应带 X-Request-ID" 1 "$([[ -n "$rid" ]] && echo 1 || echo 0)"
#     【19】-n "$rid" 意思是「字符串非空」；反过来 -z "$rid" 是「为空」。

# ---------------------------------------------------------------------------
say "F 组：可观测性"
# 【20】|| true：curl 失败了也不让 set -e 杀掉脚本 —— 指标拉不到应该记一条 FAIL，而不是中断。
metrics=$(curl -s --max-time 10 "$METRICS/metrics" || true)
check "指标端点可达" 1 "$([[ -n "$metrics" ]] && echo 1 || echo 0)"

# 【21】grep -q 是「找到就行，不打印」（q = quiet），只靠退出码说话，适合放在 if 或 && 里。
#      <<<"$metrics" 叫 here-string：把这个变量的内容当成 grep 的输入，等价于 echo "$metrics" | grep。
check "暴露请求计数" 1 "$(grep -q 'flowgate_requests_total' <<<"$metrics" && echo 1 || echo 0)"
check "暴露鉴权缓存指标" 1 "$(grep -q 'flowgate_auth_cache_entries' <<<"$metrics" && echo 1 || echo 0)"

# ---------------------------------------------------------------------------
if [[ -n "$MODEL" ]]; then
  say "E 组：AI 流式（model=$MODEL）"
  # 【22】grep -c 是「数有多少行匹配」，只打印数字。
  #      -N 是 curl 的参数：关掉输出缓冲，否则 SSE 会被攒着不吐出来。
  #      ${frames:-0} = 如果 frames 是空的就当 0 用，避免 -ge 比较空值报错。
  frames=$(curl -s -N --max-time 15 \
    -H "X-API-Key: $KEY" -H 'Content-Type: application/json' \
    -d "{\"model\":\"$MODEL\",\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"ping\"}]}" \
    "$BASE/v1/chat/completions" | grep -c '^data:' || true)
  check "SSE 至少 2 个 data 帧" 1 "$([[ ${frames:-0} -ge 2 ]] && echo 1 || echo 0)"
else
  say "E 组：跳过（未设置 FLOWGATE_MODEL）"
fi

# ---------------------------------------------------------------------------
# 【23】退出码：exit 0 表示成功，非 0 表示失败。CI / 其他脚本靠它判断这一步过没过。
#      [[ $fail -eq 0 ]] || exit 1 的意思是：失败数为 0 就正常结束，否则退出码 1。
printf '\n通过 %d，失败 %d\n' "$pass" "$fail"
[[ $fail -eq 0 ]] || exit 1
