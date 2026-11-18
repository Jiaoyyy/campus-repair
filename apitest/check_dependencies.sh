#!/bin/sh
# 依赖安全检查：Go 已调用漏洞 + 前端依赖审计。
# 用法：sh apitest/check_dependencies.sh
# 需要本机已安装 docker；Go 扫描在固定版本容器内执行，避免污染宿主。
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
GO_IMAGE=${GO_IMAGE:-golang:1.26-alpine}
VULNCHECK=${VULNCHECK:-golang.org/x/vuln/cmd/govulncheck@v1.1.4}

echo "== Go 依赖与标准库 =="
docker run --rm -v "$ROOT/backend:/src" -w /src "$GO_IMAGE" \
  sh -c "go run $VULNCHECK ./..." && echo "PASS go vulnerabilities: 0"

echo
echo "== 前端依赖 =="
cd "$ROOT/frontend"
if npm audit --omit=dev >/dev/null 2>&1; then
  echo "PASS npm audit (生产依赖): 0"
else
  echo "npm audit (生产依赖) 有发现："
  npm audit --omit=dev || true
fi
echo "开发依赖审计（构建期）："
npm audit || true
