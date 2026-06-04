# IMAgent APK — 构建指南

## 环境要求

- Android Studio Hedgehog (2023.1) 或更高
- JDK 17
- Android SDK 34

## 构建步骤

```bash
cd android

# 准备语音模型（从 huggingface 下载）
mkdir -p app/src/main/assets
# 将 sherpa-onnx STT/TTS 模型放入 assets/
# 必需模型:
#   - sherpa-onnx-zh-encoder.onnx  (中文 STT)
#   - sherpa-onnx-zh-decoder.onnx
#   - sherpa-onnx-zh-joiner.onnx
#   - sherpa-onnx-zh-tokens.txt
#   - kokoro-82m-v1.0.onnx        (TTS)
#   - kokoro-82m-tokens.txt
#   - kokoro-82m-lexicon.txt
#   - silero-vad.onnx              (VAD, optional)

# 构建
./gradlew assembleDebug

# 产物
# app/build/outputs/apk/debug/app-debug.apk
```

## 连接 Relay

1. 启动 Relay: `./relay -port 8088`
2. Agent 连 `/mcp` → `voice_connect` → 获得配对码
3. APK 设置中填入 Relay 地址 + 配对码
4. 连接后即可文字聊天 / 语音对话

## 项目结构

```
android/
├── build.gradle.kts
├── settings.gradle.kts
├── gradle/wrapper/
└── app/
    ├── build.gradle.kts
    ├── proguard-rules.pro
    └── src/main/
        ├── AndroidManifest.xml
        ├── assets/              ← 语音模型 (.onnx)
        ├── java/com/linscm/imagent/
        │   ├── MainActivity.kt   # 双模式主界面
        │   ├── McpClient.kt      # MCP WebSocket 客户端
        │   └── VoiceBridge.kt    # sherpa-onnx 语音引擎
        └── res/
            ├── layout/activity_main.xml
            ├── drawable/         # 按钮/背景/状态点
            └── values/           # 颜色/主题
```

## 当前进度

- ✅ MCP WebSocket 客户端
- ✅ 双模式 UI (语音 + 文本)
- ✅ 文本聊天完整链路
- ✅ sherpa-onnx 语音引擎集成
- ✅ STT 流式识别 (中文)
- ✅ TTS 语音合成 (Kokoro-82M)
- ✅ VAD 自动断句
- ⬜ 设置页 (Phase 4)
