#!/usr/bin/env bash
# Teamix 一键构建 + 启动（Linux，公共部署服务器用）
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
WORKSPACE="${1:-$ROOT}"

echo "=== 1/3 构建前端 ==="
(cd "$ROOT/web" && pnpm build)

echo "=== 2/3 构建后端 ==="
(cd "$ROOT" && go build -o teamix ./cmd/reasonix/)

echo "=== 3/3 启动 serve（工作区: $WORKSPACE）==="
pkill -f "teamix serve" 2>/dev/null || true
sleep 0.5
exec "$ROOT/teamix" serve --project "$WORKSPACE"
