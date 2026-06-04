# IMAgent Relay — 使用指南

## 编译

```bash
go build -o relay ./cmd/relay/
```

## 运行

```bash
./relay -port 8088
```

输出：
```
IMAgent Relay listening on :8088
  Agent MCP endpoint: ws://0.0.0.0:8088/mcp
  APK endpoint:        ws://0.0.0.0:8088/apk
  Health check:        http://0.0.0.0:8088/health
```

## systemd 部署

```bash
sudo cp deploy/relay.service /etc/systemd/system/imagent-relay.service
sudo systemctl daemon-reload
sudo systemctl enable --now imagent-relay
```

## MCP 工具列表

| 工具 | 用途 | 参数 |
|------|------|------|
| `voice_connect` | 注册 Agent，生成配对码 | 无 |
| `voice_status` | 查询手机连接状态 | 无 |
| `voice_speak` | 推送 TTS 文本到手机 | `text` (string) |
| `voice_chat` | 推送文字消息到手机 | `content` (string) |
| `voice_reset` | 断开配对 | 无 |

## 端点

| 路径 | 协议 | 用途 |
|------|------|------|
| `/mcp` | WebSocket | Agent MCP 连接 |
| `/apk` | WebSocket | 手机 APK 连接 |
| `/health` | HTTP GET | 健康检查 |
| `/upload` | HTTP POST | 文件上传（V2 预留） |

## 连接流程

1. Agent 连接 `/mcp` → 发送 `initialize` → `tools/list`
2. Agent 调用 `voice_connect` → 获得配对码（如 `ABCD`）
3. 人打开 APK → 自动连 `/apk` → 发送 `{"role":"apk","code":"ABCD"}`
4. Relay 验证配对码 → 回复 `{"status":"connected"}`
5. Agent 调用 `voice_chat` / `voice_speak` → 消息实时推送到 APK
6. APK 发送文字消息 → 自动路由到 Agent

## 配置

| flag | 默认 | 说明 |
|------|------|------|
| `-port` | 8088 | HTTP/WS 监听端口 |
