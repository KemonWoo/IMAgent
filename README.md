# IMAgent — 自托管语音+文字 IM

> **一句话部署：** 告诉你的 Agent：
> *"去 https://github.com/KemonWoo/IMAgent 读 AGENTS.md 并按指示部署"*

Agent 会自动：
1. 判断你在中国还是海外
2. 选择最优内网穿透工具
3. 一键启动 Relay + APK 托管
4. 给你一个二维码，手机扫码即用

---

## 是什么

IMAgent = MCP Relay + Android APK，让你的 Agent 和你通过手机语音/文字实时对话。

- 🔇 **全离线语音**：STT + TTS 纯本地（sherpa-onnx），语音数据不出手机
- 🔗 **MCP 协议**：Agent ↔ Relay ↔ APK，标准 JSON-RPC 2.0
- 🆓 **零成本部署**：内网穿透方案，不依赖任何云服务器
- 📱 **单一 APK**：460MB 含全部模型，装完即用（Android 8.0+ / 鸿蒙）

## 快速开始

```
人类: 去 https://github.com/KemonWoo/IMAgent 读 AGENTS.md 并按指示部署

Agent: [自动判断IP→选穿透工具→编译→启动→给二维码]

人类: 扫码 → 安装 → 说话
```
