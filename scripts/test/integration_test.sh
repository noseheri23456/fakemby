#!/usr/bin/env bash
#
# FakEmby 端到端冒烟（M1-7）
#
# 早前这里是 curl 拼字符串比对，失败时只打印一堆响应体，很难判断哪一步断了。
# 现在改为调用 Go 写的冒烟套件 tests/integration —— 每一步都有断言与可读的失败信息。
#
# 用法：
#   bash scripts/test/integration_test.sh                     # 进程内跑（无需先起服务）
#   FAKEMBY_SMOKE_BASE_URL=http://192.168.1.10:8096 \
#   FAKEMBY_ADMIN_API_KEY=xxx bash scripts/test/integration_test.sh   # 对活体服务跑
#
set -euo pipefail

cd "$(dirname "$0")/../.."

echo "========================================"
echo "FakEmby 冒烟测试"
echo "========================================"

if [ -n "${FAKEMBY_SMOKE_BASE_URL:-}" ]; then
    echo "目标：活体服务 ${FAKEMBY_SMOKE_BASE_URL}"
else
    echo "目标：进程内服务（内存库，无需外部依赖）"
fi
echo ""

go test ./tests/integration/... -run TestSmoke -count=1 -v

echo ""
echo "? 冒烟通过"
