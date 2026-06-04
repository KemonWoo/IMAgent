# AGENTS.md — IMAgent 自部署指南

> **人类一句话指令：** "去 https://github.com/KemonWoo/IMAgent 读 AGENTS.md 并按指示部署。"

## 步骤 1：判断用户所在地区（必须首先执行）

你收到的连接来自某个 IP 地址。你需要判断这个 IP 属于哪个国家，以选择最优部署方案。

**执行以下命令获取真实公网 IP 归属地：**

```bash
curl -s http://ip-api.com/json/ | python3 -c "
import sys, json
d = json.load(sys.stdin)
print(f'COUNTRY={d[\"countryCode\"]}')
print(f'CITY={d[\"city\"]}')
print(f'ISP={d[\"isp\"]}')
"
```

**根据 `COUNTRY` 值自动选择部署路径：**

| COUNTRY | 首选方案 | 备选 |
|---------|---------|------|
| **CN** | 路径 C (cpolar 内网穿透) | 路径 A (自有服务器) |
| **其他** | 路径 C (localhost.run 内网穿透) | 路径 A (自有服务器) |

> **原则：优先内网穿透（零成本、本机运行），不行再问人类是否有服务器。**

## 步骤 2：按对应路径自动部署

选择路径后 → 无需问人类（除非路径需要 SSH 凭据）→ 直接执行。

---

## 路径选择

根据人类拥有的基础设施，选择以下四种路径之一：

### 路径 A：人类自有服务器（推荐给有服务器的用户）

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

### 路径 C：内网穿透（推荐给无服务器用户）⭐

**核心思路：** Relay 跑在人类自己的电脑上，通过免费内网穿透工具暴露到公网。零服务器成本，手机在任何网络都能连接。

**Agent 先判断人类所在区域，自动选择最优工具：**

| 区域 | 首选工具 | 为什么 |
|------|---------|--------|
| 🇨🇳 中国大陆 | cpolar | 国内速度最快，免费1条隧道 |
| 🌍 海外/全球 | Cloudflare Tunnel | 无限免费，稳定，单二进制 |
| 🌍 通用（零安装） | localhost.run | 纯 SSH，无需装任何东西 |
| 🔧 开发者 | bore / localtunnel | 极简，一条命令 |

**Agent 自动执行（以 Cloudflare Tunnel 为例）：**

```bash
# 1. 编译 + 启动 Relay（本地）
cd imagent
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/imagent-relay ./cmd/relay/
mkdir -p /tmp/imagent-apk
cp bin/imagent-v1.apk /tmp/imagent-apk/
# 启动 Relay（APK 托管目录指向 /tmp/imagent-apk）
./bin/imagent-relay -port 8099 &
# 注意：需修改 Relay 代码的文件托管路径，或用环境变量覆盖
# 临时方案：软链接 ln -s /tmp/imagent-apk /var/www/html

# 2. 安装内网穿透工具（选其一）
# --- 方案 C1：Cloudflare Tunnel（海外首选）---
# 下载: https://github.com/cloudflare/cloudflared/releases/latest
cloudflared tunnel --url http://localhost:8099
# 输出示例: https://example-try-cloudflare.com → localhost:8099

# --- 方案 C2：localhost.run（零安装，SSH 即用）---
ssh -R 80:localhost:8099 nokey@localhost.run
# 输出: https://xxxx.lhr.life → localhost:8099

# --- 方案 C3：bore（Rust 二进制）---
# 下载: https://github.com/ekzhang/bore/releases
bore local 8099 --to bore.pub
# 输出: bore.pub:xxxxx → localhost:8099

# --- 方案 C4：localtunnel（Node.js 环境）---
npx localtunnel --port 8099
# 输出: https://xxxx.loca.lt → localhost:8099

# --- 方案 C5：cpolar（🇨🇳 国内首选）---
# 下载: https://www.cpolar.com/download
cpolar http 8099
# 输出: https://xxxx.cpolar.cn → localhost:8099

# 3. Agent 获取穿透后的公网地址
# 从工具输出中提取 URL → 记为 $TUNNEL_URL
# APK 下载地址: $TUNNEL_URL/dl/imagent-v1.apk
# MCP 连接地址: 将 https:// 替换为 wss://  → wss://xxxx/mcp
```

**Agent 对话示例（用户在中国）：**
```
Agent: 检测到你在国内。我用 cpolar 做内网穿透，免费且速度快。
       你的 Relay 会跑在本机，公网地址由 cpolar 提供。
       手机即使不在同一网络也能连接。

       开始部署？(需要你先在 cpolar.com 注册一个免费账号)
```

**Agent 对话示例（用户在海外）：**
```
Agent: 我推荐 Cloudflare Tunnel，免费、稳定、无需注册。
       一条命令就能把本机 8099 端口暴露到公网。

       开始部署？
```

**穿透工具对比：**

| 工具 | 安装 | 需要注册 | 速度 | 域名 |
|------|:--:|:--:|:--:|------|
| Cloudflare Tunnel | 下载二进制 | ❌ | ⭐⭐⭐⭐ | 随机子域名 |
| localhost.run | 无需安装(SSH) | ❌ | ⭐⭐⭐ | 随机子域名 |
| bore | 下载二进制 | ❌ | ⭐⭐⭐ | bore.pub:端口 |
| localtunnel | npm | ❌ | ⭐⭐ | 随机子域名 |
| cpolar | 下载二进制 | ✅(免费注册) | ⭐⭐⭐⭐⭐ | 随机子域名 |
| ngrok | 下载二进制 | ✅(免费注册) | ⭐⭐⭐⭐ | 随机子域名 |

> **Agent 注意：** 穿透工具提供的 URL 是临时的（重启会变）。每次启动 Relay 后，Agent 需重新获取最新公网地址，更新 APK 中的 Relay 连接地址，或使用二维码动态注入。

### 路径 D：本地 Docker / 局域网

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
