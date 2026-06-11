package com.linscm.imagent

import android.content.Context
import android.content.res.AssetManager
import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.AudioTrack
import android.media.MediaRecorder
import android.util.Log
import com.k2fsa.sherpa.onnx.*
import kotlinx.coroutines.*
import java.io.File

/**
 * VoiceBridge — wraps sherpa-onnx 1.13.2 for on-device STT + TTS + VAD.
 * Models are loaded directly from APK assets via AssetManager — no extraction needed.
 *
 * Models in assets/models/:
 *   ASR: SenseVoice int8 (zh+en+ja+ko+yue) — model.int8.onnx + tokens.txt
 *   TTS: vits-melo-tts-zh_en — model.onnx + tokens.txt + lexicon.txt
 *   VAD: silero_vad.onnx
 */
class VoiceBridge(private val ctx: Context) {

    enum class State { IDLE, LISTENING, PROCESSING, SPEAKING }

    var state: State = State.IDLE
        private set

    var onTranscript: ((String) -> Unit)? = null
    var onStateChange: ((State) -> Unit)? = null
    var onError: ((String) -> Unit)? = null
    var onSpeakComplete: (() -> Unit)? = null

    // sherpa-onnx 1.13.x engines (all take AssetManager)
    private var recognizer: OfflineRecognizer? = null
    private var tts: OfflineTts? = null
    private var vad: Vad? = null

    // Audio pipeline
    private var audioRecord: AudioRecord? = null
    private var audioTrack: AudioTrack? = null
    private var isRecording = false

    private val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())

    data class VoiceSettings(
        val speed: Float = 0.85f,
        val sid: Int = 0,
        val language: String = "zh"
    )

    var settings: VoiceSettings = VoiceSettings()
        set(value) {
            field = value
            if (tts != null) scope.launch { initTts() }
        }

    // ── Asset paths (relative to assets/) ──
    private val asrModelPath  = "models/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-int8-2024-07-17/model.int8.onnx"
    private val asrTokensPath = "models/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-int8-2024-07-17/tokens.txt"
    private val ttsModelPath  = "models/vits-icefall-zh-aishell3/model.onnx"
    private val ttsTokensPath = "models/vits-icefall-zh-aishell3/tokens.txt"
    private val ttsLexiconPath = "models/vits-icefall-zh-aishell3/lexicon.txt"
    private val ttsDataDir    = "models/vits-icefall-zh-aishell3"
    private val vadModelPath  = "models/silero_vad.onnx"

    // ── Init ──

    suspend fun initialize(): Boolean = withContext(Dispatchers.IO) {
        try {
            val assets = ctx.assets
            val sttOk = initStt(assets)
            val ttsOk = initTts()
            initVad(assets)
            val ready = sttOk || ttsOk
            Log.i(TAG, "VoiceBridge init: STT=$sttOk TTS=$ttsOk VAD=${vad != null}")
            ready
        } catch (e: Exception) {
            Log.e(TAG, "VoiceBridge init failed", e)
            onError?.let { withContext(Dispatchers.Main) { it("语音初始化失败: ${e.message}") } }
            false
        }
    }

    private fun initStt(assets: AssetManager): Boolean {
        return try {
            val senseVoiceCfg = OfflineSenseVoiceModelConfig(
                model = asrModelPath,
                language = "zh",
                useInverseTextNormalization = true,
                qnnConfig = QnnConfig()
            )
            val modelCfg = OfflineModelConfig(
                senseVoice = senseVoiceCfg,
                tokens = asrTokensPath,
                numThreads = 2,
                debug = false
            )
            val featCfg = FeatureConfig(sampleRate = 16000, featureDim = 80)
            val config = OfflineRecognizerConfig(
                featConfig = featCfg,
                modelConfig = modelCfg,
                decodingMethod = "greedy_search"
            )
            recognizer = OfflineRecognizer(assets, config)
            Log.i(TAG, "STT OK (SenseVoice)")
            true
        } catch (e: Exception) {
            Log.e(TAG, "STT init failed", e)
            recognizer = null
            false
        }
    }

    private fun initTts(): Boolean {
        return try {
            val vitsCfg = OfflineTtsVitsModelConfig(
                model = ttsModelPath,
                lexicon = ttsLexiconPath,
                tokens = ttsTokensPath,
                dataDir = ttsDataDir,
                noiseScale = 0.667f,
                noiseScaleW = 0.8f,
                lengthScale = 1.0f / settings.speed
            )
            val modelCfg = OfflineTtsModelConfig(vits = vitsCfg, numThreads = 2, debug = false)
            val config = OfflineTtsConfig(model = modelCfg)
            if (tts != null) { tts?.release() }
            tts = OfflineTts(ctx.assets, config)
            Log.i(TAG, "TTS OK (vits-icefall, 174 speakers)")
            true
        } catch (e: Exception) {
            Log.e(TAG, "TTS init failed", e)
            tts = null
            false
        }
    }

    private fun initVad(assets: AssetManager) {
        try {
            val sileroCfg = SileroVadModelConfig(
                model = vadModelPath,
                threshold = 0.5f,
                minSilenceDuration = 0.5f,
                minSpeechDuration = 0.25f,
                windowSize = 512
            )
            val vadCfg = VadModelConfig(
                sileroVadModelConfig = sileroCfg,
                sampleRate = 16000,
                numThreads = 1,
                debug = false
            )
            vad = Vad(assets, vadCfg)
            Log.i(TAG, "VAD OK (silero)")
        } catch (e: Exception) {
            Log.w(TAG, "VAD init failed, using energy fallback", e)
            vad = null
        }
    }

    // ── Recording ──

    fun startListening() {
        if (isRecording) return
        isRecording = true
        scope.launch {
            setState(State.LISTENING)
            val sr = 16000
            val bs = AudioRecord.getMinBufferSize(sr, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT)
                .coerceAtLeast(sr / 10)
            audioRecord = AudioRecord(MediaRecorder.AudioSource.MIC, sr, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT, bs)
            try {
                audioRecord?.startRecording()
                if (recognizer != null) recordWithAsr(sr, bs) else recordSimple(sr, bs)
            } catch (e: Exception) {
                Log.e(TAG, "Record error", e)
                onError?.let { withContext(Dispatchers.Main) { it("录音错误: ${e.message}") } }
            } finally {
                audioRecord?.stop(); audioRecord?.release(); audioRecord = null
                if (isRecording) setState(State.IDLE)
            }
        }
    }

    private suspend fun recordWithAsr(sampleRate: Int, bufferSize: Int) {
        val rec = recognizer ?: return
        val buffer = ShortArray(bufferSize / 2)
        val allSamples = mutableListOf<Short>()
        var silenceFrames = 0; var speechFrames = 0
        val silenceThresh = 25; val minSpeech = 8

        while (isRecording && audioRecord != null) {
            val read = audioRecord?.read(buffer, 0, buffer.size) ?: -1
            if (read <= 0) continue
            val energy = buffer.sliceArray(0 until read).map { kotlin.math.abs(it.toInt()) }.average()
            if (energy > 120) {
                silenceFrames = 0; speechFrames++
                for (i in 0 until read) allSamples.add(buffer[i])
                setState(State.LISTENING)
            } else if (speechFrames > 0) {
                silenceFrames++
                for (i in 0 until read) allSamples.add(buffer[i])
                if (silenceFrames >= silenceThresh && speechFrames >= minSpeech) break
            }
            // VAD check
            vad?.acceptWaveform(buffer.sliceArray(0 until read).map { it.toFloat() }.toFloatArray())
            yield()
        }

        if (allSamples.isNotEmpty()) {
            setState(State.PROCESSING)
            val samples = allSamples.map { it.toFloat() / Short.MAX_VALUE }.toFloatArray()
            val stream = rec.createStream()
            stream.acceptWaveform(samples, sampleRate)
            rec.decode(stream)
            val text = rec.getResult(stream).text
            if (text.isNotBlank()) {
                withContext(Dispatchers.Main) { onTranscript?.invoke(text.trim()) }
            }
            stream.release()
        }
    }

    private suspend fun recordSimple(sampleRate: Int, bufferSize: Int) {
        val buffer = ShortArray(bufferSize / 2)
        val allSamples = mutableListOf<Short>()
        var silenceFrames = 0; var speechFrames = 0
        while (isRecording && audioRecord != null) {
            val read = audioRecord?.read(buffer, 0, buffer.size) ?: -1
            if (read <= 0) continue
            val energy = buffer.slice(0 until read).map { kotlin.math.abs(it.toInt()) }.average()
            if (energy > 100) { silenceFrames = 0; speechFrames++; for (i in 0 until read) allSamples.add(buffer[i]) }
            else if (speechFrames > 0) { silenceFrames++; for (i in 0 until read) allSamples.add(buffer[i]); if (silenceFrames >= 20 && speechFrames >= 10) break }
            yield()
        }
        if (allSamples.isNotEmpty()) {
            withContext(Dispatchers.Main) { onTranscript?.invoke("[语音 ${allSamples.size / sampleRate}s]") }
        }
    }

    fun stopListening() { isRecording = false; scope.launch { delay(200); if (state == State.LISTENING) setState(State.PROCESSING) } }

    // ── TTS ──

    fun speak(text: String) {
        if (text.isBlank()) return
        scope.launch {
            setState(State.SPEAKING)
            try {
                val t = tts
                if (t != null) {
                    val audio = t.generate(text, sid = settings.sid, speed = settings.speed)
                    playAudio(audio.samples, audio.sampleRate)
                } else { delay(1500) }
            } catch (e: Exception) {
                Log.e(TAG, "TTS error", e)
                withContext(Dispatchers.Main) { onError?.invoke("语音合成失败") }
            } finally {
            if (state == State.SPEAKING) setState(State.IDLE)
            withContext(Dispatchers.Main) { onSpeakComplete?.invoke() }
        }
        }
    }

    private suspend fun playAudio(samples: FloatArray, sampleRate: Int) = withContext(Dispatchers.IO) {
        try {
            val shorts = ShortArray(samples.size) { (samples[it] * Short.MAX_VALUE).toInt().coerceIn(Short.MIN_VALUE.toInt(), Short.MAX_VALUE.toInt()).toShort() }
            val bs = AudioTrack.getMinBufferSize(sampleRate, AudioFormat.CHANNEL_OUT_MONO, AudioFormat.ENCODING_PCM_16BIT)
                .coerceAtLeast(4096)
            audioTrack = AudioTrack.Builder()
                .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_MEDIA).setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
                .setAudioFormat(AudioFormat.Builder().setEncoding(AudioFormat.ENCODING_PCM_16BIT).setSampleRate(sampleRate).setChannelMask(AudioFormat.CHANNEL_OUT_MONO).build())
                .setBufferSizeInBytes(bs * 2)
                .setTransferMode(AudioTrack.MODE_STREAM)
                .build()
            audioTrack?.play()
            // Write in chunks to avoid buffer overflow
            var offset = 0
            val chunkSize = 4096
            while (offset < shorts.size) {
                val len = minOf(chunkSize, shorts.size - offset)
                audioTrack?.write(shorts, offset, len)
                offset += len
            }
            audioTrack?.stop(); audioTrack?.release(); audioTrack = null
        } catch (e: Exception) {
            Log.e(TAG, "Playback error", e)
            audioTrack?.release(); audioTrack = null
        }
    }

    private fun setState(s: State) { state = s; scope.launch(Dispatchers.Main) { onStateChange?.invoke(s) } }

    fun shutdown() {
        isRecording = false; scope.cancel()
        recognizer?.release(); recognizer = null
        tts?.release(); tts = null
        vad?.release(); vad = null
        audioRecord?.release(); audioRecord = null
        audioTrack?.release(); audioTrack = null
        state = State.IDLE
    }

    companion object { private const val TAG = "IMAgent-Voice" }
}
