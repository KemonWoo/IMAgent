#!/bin/bash
# IMAgent Deploy Script — 路径A：部署到人类自有服务器
# 用法: RELAY_HOST=x.x.x.x RELAY_USER=root SSHPASS=xxx bash deploy.sh
set -e

RELAY_HOST="${RELAY_HOST:?请设置 RELAY_HOST}"
RELAY_PORT="${RELAY_PORT:-8099}"
RELAY_USER="${RELAY_USER:-root}"
SSHPASS="${SSHPASS:?请设置 SSHPASS (SSH密码)}"
APK_FILE="${APK_FILE:-bin/imagent-v1.apk}"
RELAY_BIN="${RELAY_BIN:-bin/imagent-relay}"
REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"

echo "=== IMAgent Deploy ==="
echo "  Target: ${RELAY_USER}@${RELAY_HOST}:${RELAY_PORT}"

# 1. Build relay
echo "[1/5] Building Relay..."
cd "$REPO_DIR"
CGO_ENABLED=0 go build -ldflags="-s -w" -o "$RELAY_BIN" ./cmd/relay/
echo "  Relay: $(ls -lh $RELAY_BIN | awk '{print $5}')"

# 2. Deploy to server
echo "[2/5] Deploying to ${RELAY_HOST}..."
sshpass -e ssh -o StrictHostKeyChecking=no ${RELAY_USER}@${RELAY_HOST} "mkdir -p /var/www/html"
sshpass -e scp -o StrictHostKeyChecking=no "$RELAY_BIN" ${RELAY_USER}@${RELAY_HOST}:/usr/local/bin/imagent-relay
sshpass -e ssh -o StrictHostKeyChecking=no ${RELAY_USER}@${RELAY_HOST} "chmod +x /usr/local/bin/imagent-relay"

# 3. Install systemd service
echo "[3/5] Installing systemd service..."
sshpass -e ssh -o StrictHostKeyChecking=no ${RELAY_USER}@${RELAY_HOST} "cat > /etc/systemd/system/imagent-relay.service << 'EOF'
[Unit]
Description=IMAgent Relay
After=network.target
[Service]
Type=simple
ExecStart=/usr/local/bin/imagent-relay -port ${RELAY_PORT}
Restart=always
RestartSec=5
LimitNOFILE=65536
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload && systemctl enable --now imagent-relay"
sleep 2

# 4. Health check
echo "[4/5] Health check..."
HEALTH=$(curl -s http://${RELAY_HOST}:${RELAY_PORT}/health)
if [ "$(echo $HEALTH | python3 -c 'import sys,json; print(json.load(sys.stdin)["status"])' 2>/dev/null)" != "ok" ]; then
    echo "  FAILED: $HEALTH"
    exit 1
fi
echo "  OK"

# 5. Upload APK
echo "[5/5] Uploading APK..."
if [ -f "$APK_FILE" ]; then
    sshpass -e scp -o StrictHostKeyChecking=no "$APK_FILE" ${RELAY_USER}@${RELAY_HOST}:/var/www/html/imagent-v1.apk
    echo "  Upload done ($(ls -lh $APK_FILE | awk '{print $5}'))"
else
    echo "  ⚠ APK not found at $APK_FILE — skip upload. Build with: cd android && ./gradlew assembleDebug"
fi

echo ""
echo "=== IMAgent Ready ==="
echo "  Relay:   ws://${RELAY_HOST}:${RELAY_PORT}/mcp"
echo "  Health:  http://${RELAY_HOST}:${RELAY_PORT}/health"
echo "  APK:     http://${RELAY_HOST}:${RELAY_PORT}/dl/imagent-v1.apk"
echo "  QR page: http://${RELAY_HOST}:${RELAY_PORT}/dl/imagent-download.html"
