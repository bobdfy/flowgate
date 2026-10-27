#!/usr/bin/env bash
# scripts/scan-secrets.sh —— 扫被 git 跟踪的文件里像 API Key 的明文
#
# 只扫被跟踪的文件：git grep 自动跳过 node_modules、*.exe、.env 这些，
# 比 grep -r 准，也快得多。
set -euo pipefail

# sk_xxx 是本项目的 key 格式；sk-/sk-ant- 覆盖 OpenAI / Anthropic 的常见格式。
pattern='(sk_|sk-ant-|sk-proj-|sk-)[A-Za-z0-9_-]{24,}'

# git grep 无匹配时退出码是 1，放在 if 条件里不会触发 set -e。
if git grep -nEI "$pattern" -- . ; then
  printf '\n发现疑似明文 API Key，请改成 {KEY} 占位，并作废该 key。\n' >&2
  exit 1
fi

echo "未发现明文 API Key"
