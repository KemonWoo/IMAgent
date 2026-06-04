# IMAgent v1 — 部署与使用手册

## 一、系统架构

```
┌──────────┐  MCP/WS   ┌──────────────┐  MCP/WS   ┌──────────┐
│  Hermes  │◄─────────►│  Relay (8099) │◄─────────►│  APK     │
│  Agent   │  JSON-RPC │  8.153.192.3  │  JSON-RPC │  手机     │
└──────────┘           └──────────────┘           └──────────┘
```

## 二、部署

### 一键部署

```bash
cd /home/aidev/repos/IMAgent
SSHPASS=xxx bash bin/deploy.sh
```

### 手动部署

```bash
# 1. 编译 Relay
cd /home/aidev/repos/IMAgent
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/imagent-relay ./cmd/relay/

# 2. 上传到服务器
scp bin/imagent-relay root@8.153.192.3:/usr/local/bin/
ssh root@8.153.192.3 "systemctl restart imagent-relay"

# 3. 上传 APK
scp bin/imagent-v1.apk root@8.153.192.3:/var/www/html/

# 4. 验证
curl http://8.153.192.3:8099/health          # → {"status":"ok"}
curl http://8.153.192.3:8099/dl/imagent-v1.apk  # → HTTP 200
```

## 三、Agent 连接

Agent 通过 WebSocket 连接 Relay：

```
ws://8.153.192.3:8099/mcp
```

### MCP 工具

| 工具 | 说明 |
|------|------|
| `voice_connect` | 生成配对码，人输入到 APK |
| `voice_status` | 查看 APK 在线状态 |
| `voice_speak` | 发文字到手机朗读 (TTS) |
| `voice_chat` | 发文字到手机聊天界面 |
| `voice_reset` | 断开并重新配对 |

### 调用示例

```json
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"voice_connect","arguments":{}}}
→ "Pairing code: BW3S"

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"voice_speak","arguments":{"text":"你好"}}}
→ "Sent to phone."

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"voice_chat","arguments":{"content":"这是文字消息"}}}
→ "Sent to phone."
```

## 四、APK 安装

1. 扫码或打开：http://8.153.192.3:8099/dl/imagent-download.html
2. 下载 `imagent-v1.apk` (460MB)
3. 允许安装未知来源
4. 打开后输入 Agent 调用 `voice_connect` 生成的 4 位配对码
5. 连接成功 → 开始语音对话

## 五、语音对话流程

```
人说话 → sherpa-onnx STT → 文字 → MCP → Agent
                                            ↓
人听到 ← sherpa-onnx TTS ← 文字 ← MCP ← Agent 回复
```

APK 内部 sherpa-onnx 全离线：
- STT: SenseVoice int8 (229MB)
- TTS: vits-melo-tts-zh_en (163MB)
- VAD: silero (629KB)

## 六、服务管理

```bash
# 查看状态
ssh root@8.153.192.3 "systemctl status imagent-relay"

# 重启
ssh root@8.153.192.3 "systemctl restart imagent-relay"

# 查看日志
ssh root@8.153.192.3 "journalctl -u imagent-relay -f"

# 停止
ssh root@8.153.192.3 "systemctl stop imagent-relay"
```

## 七、端口说明

| 端口 | 服务 | 协议 |
|------|------|------|
| 8099 | IMAgent Relay | HTTP/WebSocket |
| 8088 | Trix Relay (旧) | HTTP/WebSocket |

## 八、安全说明

- V1 无 TLS，适合局域网/内网
- 语音数据全本地处理，不出手机
- Relay 不持久化消息
- 配对码一次性使用
