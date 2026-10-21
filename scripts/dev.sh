#!/usr/bin/env sh
# FastLink-tool 一键开发启动（Linux / macOS）
# 用法：./scripts/dev.sh [-p 端口] [--root 状态目录] [--no-open] [--plain]
set -eu

PORT="${PORT:-8787}"
STATE="${STATE:-.run-dev}"
NO_OPEN=0
PLAIN=0

usage() {
  cat <<'EOF'
FastLink-tool 开发启动

  -p, --port <端口>    监听端口，默认 8787
      --root <目录>    状态目录，默认 .run-dev
      --no-open        不自动打开浏览器
      --plain          强制使用 go run（默认在装有 air 时自动热重载）
  -h, --help           显示本帮助

环境变量 PORT、STATE 可代替对应参数。
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    -p|--port)  [ $# -ge 2 ] || { echo "缺少端口值" >&2; exit 2; }; PORT="$2"; shift 2 ;;
    --root)     [ $# -ge 2 ] || { echo "缺少目录值" >&2; exit 2; }; STATE="$2"; shift 2 ;;
    --no-open)  NO_OPEN=1; shift ;;
    --plain)    PLAIN=1; shift ;;
    -h|--help)  usage; exit 0 ;;
    *)          echo "未知参数：$1" >&2; usage >&2; exit 2 ;;
  esac
done

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

if ! command -v go >/dev/null 2>&1; then
  echo "错误：未找到 go。请安装 Go 1.27+ 并加入 PATH：https://go.dev/dl/" >&2
  exit 1
fi

GOVER=$(go version 2>/dev/null | awk '{print $3}')
mkdir -p "$STATE/.run"

# .air.toml 固定了 8787 端口与 --no-open，只有全默认时才走热重载，避免参数被静默忽略。
USE_AIR=0
if [ "$PLAIN" = "0" ] && [ "$PORT" = "8787" ] && [ "$NO_OPEN" = "0" ] && [ "$STATE" = ".run-dev" ]; then
  if command -v air >/dev/null 2>&1; then USE_AIR=1; fi
fi

echo
echo "FastLink-tool dev"
echo "  地址: http://127.0.0.1:$PORT/"
echo "  状态: $STATE"
echo "  Go:   ${GOVER:-unknown}"
if [ "$USE_AIR" = "1" ]; then
  echo "  模式: air 热重载（不自动打开浏览器）"
else
  echo "  模式: go run（改 Go 代码需重启）"
fi
echo "  退出: Ctrl+C"
echo

if [ "$USE_AIR" = "1" ]; then
  exec air
fi

if [ "$NO_OPEN" = "1" ]; then
  exec go run ./cmd/fastlink-tool --dev --root "$STATE" --port "$PORT" --no-open
fi
exec go run ./cmd/fastlink-tool --dev --root "$STATE" --port "$PORT"
