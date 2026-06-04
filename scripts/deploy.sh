#!/usr/bin/env bash
# IMAgent Relay 一键部署脚本
# 用法: ./deploy.sh [user@host] [--port 8099]
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'; NC='\033[0m'

echo -e "${CYAN}═══ IMAgent Relay 部署工具 ═══${NC}"

# ── 参数解析 ──
HOST=""
PORT=8099
while [[ $# -gt 0 ]]; do
    case "$1" in
        --port) PORT="$2"; shift 2 ;;
        *) HOST="$1"; shift ;;
    esac
done

# ── 编译 Relay ──
echo -e "\n${CYAN}[1/4] 编译 Relay...${NC}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
cd "$REPO_DIR"

CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/imagent-relay ./cmd/relay/
echo -e "${GREEN}  ✅ 编译完成: bin/imagent-relay ($(du -h bin/imagent-relay | cut -f1))${NC}"

# ── 确定部署目标 ──
if [[ -z "$HOST" ]]; then
    echo -e "\n${CYAN}[2/4] 部署目标${NC}"
    echo "  本地部署: 直接启动"
    echo "  远程部署: ./deploy.sh user@host"
    echo ""
    read -rp "  按 Enter 本地启动，或输入 user@host 远程部署: " input
    HOST="${input:-local}"
fi

if [[ "$HOST" == "local" ]]; then
    echo -e "\n${CYAN}[3/4] 本地启动...${NC}"
    mkdir -p /var/imagent-uploads /var/www/html
    nohup ./bin/imagent-relay -port "$PORT" \
        -www /var/www/html \
        -uploads /var/imagent-uploads \
        > /var/log/imagent-relay.log 2>&1 &
    echo -e "${GREEN}  ✅ Relay 已启动 (PID $!, 端口 $PORT)${NC}"
else
    echo -e "\n${CYAN}[3/4] 远程部署到 $HOST...${NC}"
    ssh "$HOST" "mkdir -p /var/imagent-uploads /var/www/html"
    scp bin/imagent-relay "$HOST:/usr/local/bin/imagent-relay"

    # 安装 systemd 服务
    ssh "$HOST" "cat > /etc/systemd/system/imagent-relay.service << 'EOF'
[Unit]
Description=IMAgent Relay Server
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/imagent-relay -port $PORT \\
    -www /var/www/html \\
    -uploads /var/imagent-uploads
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now imagent-relay"

    echo -e "${GREEN}  ✅ 已部署到 $HOST:$PORT${NC}"
fi

# ── 验证 ──
echo -e "\n${CYAN}[4/4] 验证...${NC}"
sleep 2
if [[ "$HOST" == "local" ]]; then
    CHECK_URL="http://localhost:$PORT/health"
else
    CHECK_URL="http://${HOST#*@}:$PORT/health"
fi

if curl -sf "$CHECK_URL" > /dev/null 2>&1; then
    echo -e "${GREEN}  ✅ Relay 运行正常: $CHECK_URL${NC}"
    echo -e "\n${GREEN}═══ 部署成功 ═══${NC}"
    echo -e "  Agent 连接:   ws://${HOST##*@}:$PORT/mcp"
    echo -e "  APK Web:      http://${HOST##*@}:$PORT/dl/"
    echo -e "  健康检查:     $CHECK_URL"
else
    echo -e "${RED}  ❌ 健康检查失败，请检查日志${NC}"
    exit 1
fi
