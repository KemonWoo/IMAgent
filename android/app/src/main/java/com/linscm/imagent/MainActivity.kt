package com.linscm.imagent

import android.Manifest
import android.app.AlertDialog
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.drawable.GradientDrawable
import android.net.Uri
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.text.InputType
import android.view.Gravity
import android.view.MotionEvent
import android.view.View
import android.widget.*
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import com.google.zxing.integration.android.IntentIntegrator
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.*
import java.net.HttpURLConnection
import java.net.URL
import java.security.cert.X509Certificate
import javax.net.ssl.*

class MainActivity : AppCompatActivity() {

    private lateinit var mcp: McpClient
    private lateinit var voice: VoiceBridge
    private var isVoiceMode = false
    private var connected = false

    // Text mode
    private lateinit var textContainer: LinearLayout
    private lateinit var chatMessages: LinearLayout
    private lateinit var inputText: EditText
    private lateinit var sendBtn: ImageButton
    private lateinit var imageBtn: ImageButton
    private lateinit var fileBtn: ImageButton
    private lateinit var voiceModeBtn: ImageButton

    // Image picker
    private val pickImageLauncher = registerForActivityResult(
        ActivityResultContracts.GetContent()
    ) { uri: Uri? -> uri?.let { uploadFile(it, isImage = true) } }

    // File picker
    private val pickFileLauncher = registerForActivityResult(
        ActivityResultContracts.GetContent()
    ) { uri: Uri? -> uri?.let { uploadFile(it, isImage = false) } }

    // QR scanner launcher
    private val scanLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        val scanResult = IntentIntegrator.parseActivityResult(
            IntentIntegrator.REQUEST_CODE, result.resultCode, result.data
        )
        scanResult?.contents?.let { contents ->
            handleScannedURI(contents)
        }
    }

    // Voice mode
    private lateinit var voiceContainer: LinearLayout
    private lateinit var subtitleYou: TextView
    private lateinit var subtitleAI: TextView
    private lateinit var micBtn: Button
    private lateinit var textModeBtn: TextView

    // Status
    private lateinit var statusDot: View
    private lateinit var statusText: TextView
    private lateinit var settingsBtn: TextView

    // ── Mode switching ──

    private var voiceActive = false
    private val handler = Handler(Looper.getMainLooper())

    private fun autoListen() {
        if (!voiceActive && isVoiceMode) {
            voice.startListening()
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        // Request audio permission first
        if (checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(arrayOf(Manifest.permission.RECORD_AUDIO), 100)
        }

        bindViews()

        voice = VoiceBridge(this).apply {
            onTranscript = { text ->
                runOnUiThread {
                    subtitleYou.text = "你: $text"
                    mcp.sendText(text)
                    addBubble(text, true)
                }
            }
            onStateChange = { state ->
                runOnUiThread {
                    when (state) {
                        VoiceBridge.State.LISTENING -> {
                            subtitleYou.text = "🎤 正在听..."
                            voiceActive = true
                            micBtn.text = "⏹"
                        }
                        VoiceBridge.State.PROCESSING -> subtitleYou.text = "⏳ 识别中.."
                        VoiceBridge.State.SPEAKING -> subtitleAI.text = "🔊 朗读中..."
                        VoiceBridge.State.IDLE -> {
                            if (subtitleYou.text.startsWith("🎤") || subtitleYou.text.startsWith("⏳"))
                                subtitleYou.text = ""
                            if (subtitleAI.text.startsWith("🔊")) subtitleAI.text = ""
                            voiceActive = false
                            micBtn.text = "🎤"
                        }
                    }
                }
            }
            onSpeakComplete = {
                // After TTS finishes → auto-restart listening if still in voice mode
                runOnUiThread {
                    if (isVoiceMode) {
                        handler.postDelayed({ autoListen() }, 600)
                    }
                }
            }
            onError = { msg -> runOnUiThread { Toast.makeText(this@MainActivity, msg, Toast.LENGTH_SHORT).show() } }
        }
        lifecycleScope.launch { voice.initialize() }

        // Restore saved voice speed
        val savedSpeed = getSharedPreferences("imagent", MODE_PRIVATE).getFloat("voice_speed", 0.85f)
        voice.settings = VoiceBridge.VoiceSettings(speed = savedSpeed)

        setupMcp()

        // Check for deep link intent
        handleDeepLink(intent)

        maybeShowSetup()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handleDeepLink(intent)
    }

    private fun handleDeepLink(intent: Intent) {
        val uri = intent.data ?: return
        if (uri.scheme == "imagent" && uri.host == "pair") {
            handleScannedURI(uri.toString())
        }
    }

    private fun handleScannedURI(contents: String) {
        // Parse: imagent://pair?r=relay_addr&c=code
        val uri = Uri.parse(contents)
        val relay = uri.getQueryParameter("r") ?: return
        val code = uri.getQueryParameter("c") ?: ""
        android.util.Log.d("IMAgent", "QR scanned: relay=$relay code=$code")

        val prefs = getSharedPreferences("imagent", MODE_PRIVATE)
        prefs.edit()
            .putString("server", relay)
            .putString("code", code)
            .apply()

        Toast.makeText(this, "已扫描: $relay", Toast.LENGTH_SHORT).show()
        connect()
    }

    private fun startQRScan() {
        val integrator = IntentIntegrator(this)
        integrator.setDesiredBarcodeFormats(IntentIntegrator.QR_CODE)
        integrator.setPrompt("扫描 IMAgent 配对二维码")
        integrator.setCameraId(0)
        integrator.setBeepEnabled(false)
        integrator.setBarcodeImageEnabled(false)
        scanLauncher.launch(integrator.createScanIntent())
    }

    private fun bindViews() {
        statusDot = findViewById(R.id.status_dot)
        statusText = findViewById(R.id.status_text)
        settingsBtn = findViewById(R.id.settings_btn)
        textContainer = findViewById(R.id.text_mode_container)
        chatMessages = findViewById(R.id.chat_messages)
        inputText = findViewById(R.id.input_text)
        sendBtn = findViewById(R.id.send_btn)
        voiceModeBtn = findViewById(R.id.voice_mode_btn)
        voiceContainer = findViewById(R.id.voice_mode_container)
        subtitleYou = findViewById(R.id.subtitle_user)
        subtitleAI = findViewById(R.id.subtitle_ai)
        sendBtn = findViewById(R.id.send_btn)
        imageBtn = findViewById(R.id.image_btn)
        voiceModeBtn = findViewById(R.id.voice_mode_btn)
        voiceContainer = findViewById(R.id.voice_mode_container)
        subtitleYou = findViewById(R.id.subtitle_user)
        subtitleAI = findViewById(R.id.subtitle_ai)
        micBtn = findViewById(R.id.mic_btn)
        textModeBtn = findViewById(R.id.text_mode_btn)
        sendBtn = findViewById(R.id.send_btn)
        imageBtn = findViewById(R.id.image_btn)
        fileBtn = findViewById(R.id.file_btn)
        voiceModeBtn = findViewById(R.id.voice_mode_btn)

        sendBtn.setOnClickListener { sendText() }
        imageBtn.setOnClickListener { pickImageLauncher.launch("image/*") }
        fileBtn.setOnClickListener { pickFileLauncher.launch("*/*") }
        voiceModeBtn.setOnClickListener { setMode(true) }
        textModeBtn.setOnClickListener { setMode(false) }
        settingsBtn.setOnClickListener { showSettings() }

        micBtn.setOnClickListener { toggleVoice() }
        inputText.addTextChangedListener(object : android.text.TextWatcher {
            override fun afterTextChanged(s: android.text.Editable?) { }
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
        })
    }

    // ── First-launch / Setup ──

    private fun maybeShowSetup() {
        val prefs = getSharedPreferences("imagent", MODE_PRIVATE)
        if (prefs.getString("server", "").isNullOrBlank()) {
            showSetupDialog()
        } else {
            connect()
        }
    }

    private fun showSetupDialog() {
        val prefs = getSharedPreferences("imagent", MODE_PRIVATE)
        val savedServer = prefs.getString("server", "") ?: ""
        val savedCode = prefs.getString("code", "") ?: ""

        val layout = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(48, 24, 48, 8)
        }

        val serverInput = EditText(this).apply {
            hint = "Relay 地址 (例: 8.153.192.3:8099 或 wss://域名)"
            setText(savedServer)
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI
        }
        layout.addView(serverInput)

        val codeInput = EditText(this).apply {
            hint = "配对码 (4位字母+数字)"
            setText(savedCode)
            setSingleLine()
        }
        val codeParams = LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        ).apply { topMargin = 16 }
        layout.addView(codeInput, codeParams)

        AlertDialog.Builder(this)
            .setTitle("⚙️ 首次配置")
            .setMessage("输入 Relay 服务地址和 Agent 提供的配对码\n或点击「扫码连接」扫描 Agent 生成的二维码")
            .setView(layout)
            .setCancelable(false)
            .setPositiveButton("连接") { _, _ ->
                val server = serverInput.text.toString().trim()
                val code = codeInput.text.toString().trim()
                if (server.isNotBlank() && code.length >= 4) {
                    prefs.edit().putString("server", server).putString("code", code).apply()
                    connect()
                } else {
                    Toast.makeText(this, "请填写完整信息", Toast.LENGTH_SHORT).show()
                }
            }
            .setNeutralButton("扫码连接") { _, _ -> startQRScan() }
            .show()
    }

    private fun showSettings() {
        val prefs = getSharedPreferences("imagent", MODE_PRIVATE)
        val savedServer = prefs.getString("server", "") ?: ""
        val savedCode = prefs.getString("code", "") ?: ""

        val layout = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(48, 24, 48, 8)
        }

        val serverInput = EditText(this).apply {
            hint = "Relay 地址"
            setText(savedServer)
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI
        }
        layout.addView(serverInput)

        val codeInput = EditText(this).apply {
            hint = "配对码 (4位字母+数字)"
            setText(savedCode)
            setSingleLine()
        }
        val codeParams = LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        ).apply { topMargin = 16 }
        layout.addView(codeInput, codeParams)

        // ── Voice tuning ──
        val voiceLabel = TextView(this).apply {
            text = "🎵 语音语速: %.1fx".format(voice.settings.speed)
            setTextColor(0xFFCBD5E1.toInt())
            textSize = 14f
            layoutParams = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT
            ).apply { topMargin = 20 }
        }
        layout.addView(voiceLabel)

        val speedBar = SeekBar(this).apply {
            max = 15  // 0.5 to 2.0: 16 steps of 0.1
            progress = ((voice.settings.speed - 0.5f) * 10).toInt().coerceIn(0, 15)
            layoutParams = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT
            )
            setOnSeekBarChangeListener(object : SeekBar.OnSeekBarChangeListener {
                override fun onProgressChanged(seekBar: SeekBar?, progress: Int, fromUser: Boolean) {
                    val spd = 0.5f + progress * 0.1f
                    voiceLabel.text = "🎵 语音语速: %.1fx".format(spd)
                }
                override fun onStartTrackingTouch(seekBar: SeekBar?) {}
                override fun onStopTrackingTouch(seekBar: SeekBar?) {
                    val spd = 0.5f + (seekBar?.progress ?: 3) * 0.1f
                    voice.settings = VoiceBridge.VoiceSettings(speed = spd)
                    getSharedPreferences("imagent", MODE_PRIVATE).edit()
                        .putFloat("voice_speed", spd).apply()
                }
            })
        }
        layout.addView(speedBar)

        val statusInfo = TextView(this).apply {
            text = "状态: ${if (connected) "在线" else "离线"}\n语音引擎: sherpa-onnx vits-melo (zh_en)"
            setTextColor(0xFF888888.toInt())
            textSize = 13f
            layoutParams = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT
            ).apply { topMargin = 16 }
        }
        layout.addView(statusInfo)

        AlertDialog.Builder(this)
            .setTitle("⚙️ 设置")
            .setView(layout)
            .setPositiveButton("保存") { _, _ ->
                val server = serverInput.text.toString().trim()
                val code = codeInput.text.toString().trim()
                if (server.isNotBlank()) {
                    prefs.edit().putString("server", server).putString("code", code).apply()
                    Toast.makeText(this, "已保存，重新连接中...", Toast.LENGTH_SHORT).show()
                    mcp.disconnect()
                    connect()
                }
            }
            .setNegativeButton("取消", null)
            .setNeutralButton("扫码") { _, _ -> startQRScan() }
            .show()
    }

    // ── MCP ──

    private fun setupMcp() {
        val prefs = getSharedPreferences("imagent", MODE_PRIVATE)
        val server = prefs.getString("server", "") ?: ""

        mcp = McpClient(
            relayUrl = server,
            onMessage = { type, json ->
                when (type) {
                    "chat_response" -> {
                        val content = json.get("content")?.asString ?: return@McpClient
                        runOnUiThread {
                            addBubble(content, false)
                            if (isVoiceMode) { subtitleAI.text = content; voice.speak(content) }
                        }
                    }
                    "tts" -> {
                        val content = json.get("content")?.asString ?: return@McpClient
                        runOnUiThread { voice.speak(content) }
                    }
                    "reset" -> runOnUiThread {
                        connected = false
                        updateStatus(McpClient.Status.DISCONNECTED)
                    }
                    "file" -> {
                        val file = json.getAsJsonObject("file") ?: return@McpClient
                        val name = file.get("name")?.asString ?: "unknown"
                        val url = file.get("url")?.asString ?: ""
                        val size = file.get("size")?.asLong ?: 0
                        val mime = file.get("mime")?.asString ?: ""
                        val ftype = file.get("type")?.asString ?: "file"
                        runOnUiThread {
                            if (ftype == "image") {
                                addImageBubble(url, name, size, null, false)
                            } else {
                                val sizeStr = when {
                                    size > 1_000_000 -> "%.1fMB".format(size / 1_000_000.0)
                                    size > 1_000 -> "%.1fKB".format(size / 1_000.0)
                                    else -> "${size}B"
                                }
                                val emoji = when (ftype) {
                                    "audio" -> "🎵"
                                    "video" -> "🎬"
                                    "document" -> "📄"
                                    else -> "📎"
                                }
                                addBubble("$emoji $name ($sizeStr)\n$url", false)
                            }
                        }
                    }
                }
            },
            onStatus = { s -> runOnUiThread { updateStatus(s) } }
        )
    }

    private fun connect() {
        val prefs = getSharedPreferences("imagent", MODE_PRIVATE)
        val server = prefs.getString("server", "") ?: ""
        val code = prefs.getString("code", "") ?: ""
        android.util.Log.d("IMAgent", "connect() called: server=[$server] code=[$code]")
        if (server.isEmpty() || code.isEmpty()) {
            android.util.Log.w("IMAgent", "connect() aborted: empty server or code")
            updateStatus(McpClient.Status.DISCONNECTED)
            return
        }
        mcp.setRelayUrl(server)
        mcp.connect(code)
    }

    private fun updateStatus(s: McpClient.Status) {
        when (s) {
            McpClient.Status.CONNECTED -> {
                connected = true
                statusDot.setBackgroundResource(R.drawable.status_dot_online)
                statusText.text = "在线"
            }
            McpClient.Status.CONNECTING -> {
                statusDot.setBackgroundResource(R.drawable.status_dot_connecting)
                statusText.text = "连接中..."
            }
            McpClient.Status.DISCONNECTED -> {
                connected = false
                statusDot.setBackgroundResource(R.drawable.status_dot_offline)
                statusText.text = "断开"
            }
            McpClient.Status.ERROR -> {
                connected = false
                statusDot.setBackgroundResource(R.drawable.status_dot_error)
                statusText.text = "错误"
            }
        }
    }

    // ── Mode switching ──

    private fun toggleVoice() {
        if (!voiceActive) {
            voice.startListening()
            voiceActive = true
            micBtn.text = "⏹"
            subtitleYou.text = "🎤 正在听..."
        } else {
            voice.stopListening()
            voiceActive = false
            micBtn.text = "🎤"
            subtitleYou.text = ""
        }
    }

    private fun setMode(voiceMode: Boolean) {
        isVoiceMode = voiceMode
        voiceContainer.visibility = if (voiceMode) View.VISIBLE else View.GONE
        textContainer.visibility = if (voiceMode) View.GONE else View.VISIBLE
        if (voiceMode) {
            handler.postDelayed({ autoListen() }, 500)
        } else {
            voice.stopListening()
            voiceActive = false
            inputText.requestFocus()
        }
    }

    private fun sendText() {
        val text = inputText.text.toString().trim()
        if (text.isEmpty()) {
            Toast.makeText(this, "请输入消息", Toast.LENGTH_SHORT).show()
            return
        }
        if (!connected) {
            Toast.makeText(this, "未连接", Toast.LENGTH_SHORT).show()
            return
        }
        mcp.sendText(text)
        addBubble(text, true)
        inputText.text.clear()
    }

    private fun addBubble(text: String, isUser: Boolean) {
        val bubble = TextView(this).apply {
            setText(text)
            setPadding(24, 14, 24, 14)
            textSize = 15f
            if (isUser) {
                setTextColor(0xFFFFFFFF.toInt())
                background = GradientDrawable().apply {
                    setColor(0xFF7C5CFC.toInt())
                    cornerRadius = 48f
                }
            } else {
                setTextColor(0xFFCBD5E1.toInt())
                background = GradientDrawable().apply {
                    setColor(0xFF1A1A2E.toInt())
                    setStroke(1, 0xFF25254A.toInt())
                    cornerRadius = 48f
                }
            }
        }
        val params = LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.WRAP_CONTENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        ).apply {
            setMargins(16, 8, 16, 8)
            gravity = if (isUser) Gravity.END else Gravity.START
        }
        chatMessages.addView(bubble, params)
        (chatMessages.parent as? ScrollView)?.post {
            (chatMessages.parent as ScrollView).fullScroll(View.FOCUS_DOWN)
        }
    }

    // ── Image upload ──

    private fun uploadFile(uri: Uri, isImage: Boolean) {
        val prefs = getSharedPreferences("imagent", MODE_PRIVATE)
        val server = prefs.getString("server", "") ?: ""
        if (server.isEmpty()) return

        val label = if (isImage) "📷" else "📎"
        addBubble("$label 上传中...", true)

        lifecycleScope.launch {
            try {
                // Get file name
                val fileName = withContext(Dispatchers.IO) {
                    val cursor = contentResolver.query(uri, null, null, null, null)
                    cursor?.use {
                        if (it.moveToFirst()) {
                            val idx = it.getColumnIndex(android.provider.OpenableColumns.DISPLAY_NAME)
                            if (idx >= 0) it.getString(idx) else "file.bin"
                        } else "file.bin"
                    } ?: "file.bin"
                }

                // Compress only for images
                var bitmap: Bitmap? = null
                val uploadBytes: ByteArray
                val mimeType: String

                if (isImage) {
                    bitmap = withContext(Dispatchers.IO) {
                        val input = contentResolver.openInputStream(uri) ?: return@withContext null
                        BitmapFactory.decodeStream(input)?.also { input.close() }
                    }
                    if (bitmap == null) {
                        runOnUiThread { 
                            chatMessages.removeViewAt(chatMessages.childCount - 1)
                            Toast.makeText(this@MainActivity, "无法读取图片", Toast.LENGTH_SHORT).show()
                        }
                        return@launch
                    }
                    val imageBitmap = bitmap
                    uploadBytes = withContext(Dispatchers.IO) {
                        val (w, h) = imageBitmap.width to imageBitmap.height
                        val ratio = 1024.0 / maxOf(w, h)
                        val bmp = if (ratio < 1.0) {
                            Bitmap.createScaledBitmap(imageBitmap, (w * ratio).toInt(), (h * ratio).toInt(), true)
                        } else imageBitmap
                        val bos = ByteArrayOutputStream()
                        bmp.compress(Bitmap.CompressFormat.JPEG, 80, bos)
                        bos.toByteArray()
                    }
                    mimeType = "image/jpeg"
                } else {
                    bitmap = null
                    uploadBytes = withContext(Dispatchers.IO) {
                        contentResolver.openInputStream(uri)?.use { it.readBytes() } ?: ByteArray(0)
                    }
                    mimeType = contentResolver.getType(uri) ?: "application/octet-stream"
                }

                // Save to temp file
                val tmpFile = withContext(Dispatchers.IO) {
                    val ext = if (isImage) ".jpg" else fileName.substringAfterLast('.', "")
                    val f = File(cacheDir, "upload_${System.currentTimeMillis()}$ext")
                    FileOutputStream(f).use { it.write(uploadBytes) }
                    f
                }

                // Upload
                val urlStr = "https://${server}/upload"
                val boundary = "Boundary-${System.currentTimeMillis()}"
                val conn = withContext(Dispatchers.IO) {
                    val u = URL(urlStr)
                    val c = u.openConnection()
                    (c as? HttpsURLConnection)?.sslSocketFactory = trustAllSSLSocketFactory()
                    (c as? HttpsURLConnection)?.hostnameVerifier = HostnameVerifier { _, _ -> true }
                    (c as HttpURLConnection).apply {
                        requestMethod = "POST"
                        doOutput = true
                        setRequestProperty("Content-Type", "multipart/form-data; boundary=$boundary")
                    }
                    val out = DataOutputStream(c.outputStream)
                    out.writeBytes("--$boundary\r\n")
                    out.writeBytes("Content-Disposition: form-data; name=\"file\"; filename=\"$fileName\"\r\n")
                    out.writeBytes("Content-Type: $mimeType\r\n\r\n")
                    out.write(uploadBytes)
                    out.writeBytes("\r\n--$boundary--\r\n")
                    out.flush()
                    out.close()
                    c
                }
                val respCode = conn.responseCode
                val respBody = if (respCode in 200..299)
                    conn.inputStream.bufferedReader().readText()
                else
                    conn.errorStream?.bufferedReader()?.readText() ?: ""
                conn.disconnect()
                tmpFile.delete()

                if (respCode !in 200..299) {
                    runOnUiThread {
                        chatMessages.removeViewAt(chatMessages.childCount - 1)
                        addBubble("❌ 上传失败: $respCode", false)
                    }
                    return@launch
                }

                val json = com.google.gson.JsonParser.parseString(respBody).asJsonObject
                val dlUrl = json.get("url")?.asString ?: ""
                val dlName = json.get("original")?.asString ?: fileName
                val fileSize = json.get("size")?.asLong ?: uploadBytes.size.toLong()

                val showBitmap = bitmap
                runOnUiThread {
                    chatMessages.removeViewAt(chatMessages.childCount - 1)
                    if (isImage) {
                        addImageBubble(dlUrl, dlName, fileSize, showBitmap, true)
                    } else {
                        val emoji = when {
                            mimeType.startsWith("audio/") -> "🎵"
                            mimeType.startsWith("video/") -> "🎬"
                            mimeType == "application/pdf" -> "📄"
                            else -> "📎"
                        }
                        addBubble("$emoji $dlName (${formatSize(fileSize)})\n$dlUrl", true)
                    }
                    mcp.sendText(if (isImage) "[图片]" else "[文件] $dlName ($dlUrl)")
                }
            } catch (e: Exception) {
                runOnUiThread {
                    if (chatMessages.childCount > 0)
                        chatMessages.removeViewAt(chatMessages.childCount - 1)
                    addBubble("❌ 上传失败: ${e.message}", false)
                }
            }
        }
    }

    private fun formatSize(size: Long): String = when {
        size > 1_000_000 -> "%.1fMB".format(size / 1_000_000.0)
        size > 1_000 -> "%.1fKB".format(size / 1_000.0)
        else -> "${size}B"
    }

    private fun addImageBubble(url: String, name: String, size: Long, bitmap: Bitmap?, isUser: Boolean) {
        val container = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER
            setPadding(8, 8, 8, 8)
            background = GradientDrawable().apply {
                if (isUser) {
                    setColor(0xFF7C5CFC.toInt())
                } else {
                    setColor(0xFF1A1A2E.toInt())
                    setStroke(1, 0xFF25254A.toInt())
                }
                cornerRadius = 24f
            }
        }

        if (bitmap != null) {
            val thumb = Bitmap.createScaledBitmap(bitmap, 200, 200.coerceAtMost(
                (200.0 * bitmap.height / bitmap.width).toInt()
            ), true)
            val img = ImageView(this).apply {
                setImageBitmap(thumb)
                scaleType = ImageView.ScaleType.CENTER_CROP
                layoutParams = LinearLayout.LayoutParams(200, 200)
            }
            container.addView(img)
        } else {
            val icon = TextView(this).apply {
                text = "🖼️"
                textSize = 48f
                gravity = Gravity.CENTER
            }
            container.addView(icon)
        }

        val sizeStr = when {
            size > 1_000_000 -> "%.1fMB".format(size / 1_000_000.0)
            size > 1_000 -> "%.1fKB".format(size / 1_000.0)
            else -> "${size}B"
        }
        val info = TextView(this).apply {
            text = "$name ($sizeStr)"
            setTextColor(0xFFAAAAAA.toInt())
            textSize = 12f
            setPadding(8, 4, 8, 4)
            gravity = Gravity.CENTER
        }
        container.addView(info)

        val params = LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.WRAP_CONTENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        ).apply {
            setMargins(16, 8, 16, 8)
            gravity = if (isUser) Gravity.END else Gravity.START
        }
        chatMessages.addView(container, params)
        (chatMessages.parent as? ScrollView)?.post {
            (chatMessages.parent as ScrollView).fullScroll(View.FOCUS_DOWN)
        }
    }

    override fun onDestroy() {
        voice.shutdown()
        mcp.disconnect()
        super.onDestroy()
    }

    companion object {
        private val trustAllSSLContext: SSLContext by lazy {
            val tm = arrayOf<TrustManager>(object : X509TrustManager {
                override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) {}
                override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {}
                override fun getAcceptedIssuers(): Array<X509Certificate> = arrayOf()
            })
            SSLContext.getInstance("TLS").apply { init(null, tm, java.security.SecureRandom()) }
        }

        fun trustAllSSLSocketFactory(): SSLSocketFactory = trustAllSSLContext.socketFactory
    }
}
