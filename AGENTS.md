# AGENTS.md — IMAgent 自部署指南

> **Agent 读取此文件即可完成 IMAgent 的全自动部署。**

## 你是 Agent，要做的事

人类说"给我部署一个语音 IM"→ 你执行以下流程：

```
1. 克隆仓库
2. 读本文 → 选部署路径
3. 问人类必要信息 → 自动部署
4. 编译 APK → 托管 → 生成二维码
5. 人类扫码 → 完成
```

---

## 路径选择

根据人类拥有的基础设施，选择以下三种路径之一：

### 路径 A：人类自有服务器（推荐）

**人类需要提供：**
- 服务器 IP 或域名
- SSH 用户名 + 密码（或密钥路径）
- 端口 8099 可对外访问

**Agent 自动执行：**
```bash
# 1. 编译 Relay（单文件 Go 二进制，无依赖）
cd imagent && CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/imagent-relay ./cmd/relay/

# 2. 上传 + 安装 systemd 服务
scp bin/imagent-relay $USER@$HOST:/usr/local/bin/
ssh $USER@$HOST "cat > /etc/systemd/system/imagent-relay.service << 'EOF'
[Unit]
Description=IMAgent Relay
After=network.target
[Service]
Type=simple
ExecStart=/usr/local/bin/imagent-relay -port 8099
Restart=always
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload && systemctl enable --now imagent-relay"

# 3. 托管 APK 到同一服务器
scp bin/imagent-v1.apk $USER@$HOST:/var/www/html/
# Relay 自动提供 /dl/ 路径下载

# 4. 验证
curl http://$HOST:8099/health  # → {"status":"ok"}
```

### 路径 B：免费云服务

**Agent 主动提供以下线索供人类选择：**

| 平台 | 说明 | 限制 |
|------|------|------|
| **Railway** | `railway.app`，免费额度 500h/月 | 需 GitHub 账号 |
| **Fly.io** | `fly.io`，免费 3 个 VM | 需信用卡验证 |
| **Render** | `render.com`，免费 Web Service | 15 分钟无流量休眠 |
| **Oracle Cloud** | 永久免费 ARM VM (4C24G) | 注册较复杂 |
| **Zeabur** | `zeabur.com`，免费 5 美元额度 | 国内可能慢 |

**Agent 执行流程：**
1. 让人类选一个平台并完成注册
2. 帮人类在该平台部署 Relay（提供平台对应的 Dockerfile 或部署配置）
3. Relay 就绪后，APK 托管到同一平台

### 路径 C：本地 Docker（无公网服务器）

**人类需要：** 安装了 Docker 的机器

**Agent 自动执行：**
```bash
# 1. 构建镜像
cat > Dockerfile << 'EOF'
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o relay ./cmd/relay/

FROM alpine:3.19
COPY --from=builder /app/relay /relay
RUN mkdir -p /var/www/html
EXPOSE 8099
CMD ["/relay", "-port", "8099"]
EOF

docker build -t imagent-relay .
docker run -d --name imagent -p 8099:8099 -v /tmp/imagent-apk:/var/www/html imagent-relay

# 2. 复制 APK 到挂载目录
cp bin/imagent-v1.apk /tmp/imagent-apk/

# 3. 局域网内使用（手机和 Docker 主机同网络）
# 下载地址：http://<DOCKER_HOST_IP>:8099/dl/imagent-v1.apk
```

> **V1 限制：** 无公网 IP 时，仅限手机与 Agent 在同一局域网使用。

---

## MCP 连接说明

Agent 部署完 Relay 后，通过 WebSocket 连接并使用 MCP 工具：

```
连接地址: ws://<RELAY_HOST>:8099/mcp
协议:    JSON-RPC 2.0 (MCP 2024-11-05)

工具列表:
  voice_connect  → 生成配对码
  voice_status   → 查询 APK 在线状态
  voice_speak    → 发送文字让手机朗读
  voice_chat     → 发送文字消息到手机
  voice_reset    → 断开配对
```

---

## APK 编译

如需要重新编译 APK：

```bash
# 前置条件
export ANDROID_HOME=/path/to/sdk
export ANDROID_SDK_ROOT=$ANDROID_HOME

# 下载模型（仅首次，413MB）
# ASR: https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-int8-2024-07-17.tar.bz2
# TTS: https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/vits-melo-tts-zh_en.tar.bz2
# VAD: https://github.com/snakers4/silero-vad/raw/master/files/silero_vad.onnx
# AAR: https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.2/sherpa-onnx-1.13.2.aar
# 解压后放入 android/app/src/main/assets/models/ 和 android/libs/

# 编译
cd android && ./gradlew assembleDebug
# 产物: app/build/outputs/apk/debug/app-debug.apk
```

---

## 仓库地址

```
GitHub: https://github.com/KemonWoo/IMAgent
克隆:   git clone https://github.com/KemonWoo/IMAgent.git
```

**Agent 注意：此仓库仅含源码。模型文件需按上述链接下载。**
