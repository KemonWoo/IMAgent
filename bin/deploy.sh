#!/bin/bash
# IMAgent Deploy Script — automated relay + APK hosting setup
# Run on the Agent host (where Hermes lives)
set -e

RELAY_HOST="${RELAY_HOST:-8.153.192.3}"
RELAY_PORT="${RELAY_PORT:-8099}"
RELAY_USER="${RELAY_USER:-root}"
APK_FILE="${APK_FILE:-/home/aidev/repos/IMAgent/bin/imagent-v1.apk}"

echo "=== IMAgent Deploy ==="

# 1. Build relay if needed
echo "[1/5] Building Relay..."
cd /home/aidev/repos/IMAgent
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/imagent-relay ./cmd/relay/
echo "  Relay: $(ls -lh bin/imagent-relay | awk '{print $5}')"

# 2. Deploy relay to server
echo "[2/5] Deploying Relay to ${RELAY_HOST}..."
sshpass -e ssh -o StrictHostKeyChecking=no ${RELAY_USER}@${RELAY_HOST} "systemctl stop imagent-relay 2>/dev/null; true"
sshpass -e scp -o StrictHostKeyChecking=no bin/imagent-relay ${RELAY_USER}@${RELAY_HOST}:/usr/local/bin/
sshpass -e ssh -o StrictHostKeyChecking=no ${RELAY_USER}@${RELAY_HOST} "systemctl start imagent-relay"
sleep 1

# 3. Health check
echo "[3/5] Health check..."
HEALTH=$(curl -s http://${RELAY_HOST}:${RELAY_PORT}/health)
if [ "$(echo $HEALTH | jq -r .status)" != "ok" ]; then
    echo "  FAILED: $HEALTH"
    exit 1
fi
echo "  OK"

# 4. Upload APK
echo "[4/5] Uploading APK (may take minutes)..."
sshpass -e scp -o StrictHostKeyChecking=no ${APK_FILE} ${RELAY_USER}@${RELAY_HOST}:/var/www/html/imagent-v1.apk
echo "  Upload done"

# 5. Verify
echo "[5/5] Verifying download..."
DL_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://${RELAY_HOST}:${RELAY_PORT}/dl/imagent-v1.apk)
if [ "$DL_CODE" = "200" ]; then
    echo "  APK download URL: http://${RELAY_HOST}:${RELAY_PORT}/dl/imagent-v1.apk"
    echo "  QR page: http://${RELAY_HOST}:${RELAY_PORT}/dl/imagent-download.html"
else
    echo "  APK download check returned HTTP ${DL_CODE}"
fi

echo ""
echo "=== IMAgent Ready ==="
echo "  Relay:   ws://${RELAY_HOST}:${RELAY_PORT}/mcp"
echo "  APK:     http://${RELAY_HOST}:${RELAY_PORT}/dl/imagent-v1.apk"
echo "  QR page: http://${RELAY_HOST}:${RELAY_PORT}/dl/imagent-download.html"
