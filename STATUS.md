# IMAgent Phase 3 — sherpa-onnx 集成完成

**日期**: 2026-06-04  
**状态**: ✅ APK 编译成功

## 交付物

| 文件 | 大小 | 说明 |
|------|------|------|
| `android/app/build/outputs/apk/debug/app-debug.apk` | 460MB | 含全离线语音引擎的 APK |

## 模型选型

| 用途 | 模型 | 大小 | 来源 |
|------|------|:--:|------|
| **STT** | SenseVoice int8 (中英日韩粤) | 229MB | GitHub asr-models |
| **TTS** | vits-melo-tts-zh_en (中英) | 163MB | GitHub tts-models |
| **VAD** | silero_vad | 629KB | GitHub asr-models |
| **SDK** | sherpa-onnx 1.13.2 AAR | 54MB | GitHub releases |

## 技术要点

- sherpa-onnx 1.13.2 所有引擎构造函数需 `AssetManager` 参数 — 模型从 APK assets 直接加载，不提取到内部存储
- 修复了 `flatDir` 覆盖全局仓库的问题
- VoiceBridge 适配了 1.13.x API：`Vad` (非 VoiceActivityDetector)、`SileroVadModelConfig`、`OfflineRecognizer(assets, config)`、`OfflineTts(assets, config)`

## 编译环境

- Android SDK 34 + build-tools 34.0.0 + NDK 25.2
- Gradle 8.5 + AGP 8.2.0 + Kotlin 1.9.22
- Java 17 (OpenJDK)
- Mihomo 代理用于 GitHub/Google 下载

## 已知问题

- APK 460MB（模型嵌入式），后续可改为首次启动下载
- SenseVoice 使用离线（非流式）识别，等待完整语音段落后解码
- 首次编译慢（约 5 分钟 Gradle 依赖下载 + 400MB 资产打包）

## 后续工作（Phase 4）

- WebView UI 与语音交互集成
- MCP 文本消息桥接
- 全双工语音对话流程
