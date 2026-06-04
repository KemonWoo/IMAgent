package com.linscm.imagent

import android.app.AlertDialog
import android.graphics.drawable.GradientDrawable
import android.os.Bundle
import android.text.InputType
import android.view.Gravity
import android.view.MotionEvent
import android.view.View
import android.widget.*
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.launch

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
    private lateinit var voiceModeBtn: ImageButton

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

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)
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
                        VoiceBridge.State.LISTENING -> subtitleYou.text = "🎤 正在听..."
                        VoiceBridge.State.PROCESSING -> subtitleYou.text = "⏳ 识别中..."
                        VoiceBridge.State.SPEAKING -> subtitleAI.text = "🔊 朗读中..."
                        VoiceBridge.State.IDLE -> {
                            if (subtitleYou.text.startsWith("🎤") || subtitleYou.text.startsWith("⏳"))
                                subtitleYou.text = ""
                            if (subtitleAI.text.startsWith("🔊")) subtitleAI.text = ""
                        }
                    }
                }
            }
            onError = { msg -> runOnUiThread { Toast.makeText(this@MainActivity, msg, Toast.LENGTH_SHORT).show() } }
        }
        lifecycleScope.launch { voice.initialize() }

        setupMcp()
        maybeShowSetup()
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
        micBtn = findViewById(R.id.mic_btn)
        textModeBtn = findViewById(R.id.text_mode_btn)

        sendBtn.setOnClickListener { sendText() }
        voiceModeBtn.setOnClickListener { setMode(true) }
        textModeBtn.setOnClickListener { setMode(false) }
        settingsBtn.setOnClickListener { showSettings() }

        micBtn.setOnTouchListener { _, event ->
            when (event.action) {
                MotionEvent.ACTION_DOWN -> { voice.startListening(); true }
                MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> { voice.stopListening(); true }
                else -> false
            }
        }
        inputText.addTextChangedListener(object : android.text.TextWatcher {
            override fun afterTextChanged(s: android.text.Editable?) { sendBtn.isEnabled = !s.isNullOrBlank() && connected }
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
            hint = "Relay 地址 (例: 192.168.1.5:8088)"
            setText(savedServer)
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI
        }
        layout.addView(serverInput)

        val codeInput = EditText(this).apply {
            hint = "配对码 (6位数字)"
            setText(savedCode)
            inputType = InputType.TYPE_CLASS_NUMBER
            setSingleLine()
            val p = layoutParams as LinearLayout.LayoutParams
            p.topMargin = 16
        }
        layout.addView(codeInput)

        AlertDialog.Builder(this)
            .setTitle("⚙️ 首次配置")
            .setMessage("输入 Relay 服务地址和 Agent 提供的配对码")
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
            hint = "配对码"
            setText(savedCode)
            inputType = InputType.TYPE_CLASS_NUMBER
            setSingleLine()
            val p = layoutParams as LinearLayout.LayoutParams
            p.topMargin = 16
        }
        layout.addView(codeInput)

        val statusInfo = TextView(this).apply {
            text = "状态: ${if (connected) "在线" else "离线"}\n语音引擎: ${if (voice.state != VoiceBridge.State.IDLE) "就绪" else "待初始化"}"
            setTextColor(0xFF888888.toInt())
            textSize = 13f
            val p = layoutParams as LinearLayout.LayoutParams
            p.topMargin = 16
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
            .setNeutralButton("重置连接") { _, _ ->
                mcp.disconnect()
                connect()
            }
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
                }
            },
            onStatus = { s -> runOnUiThread { updateStatus(s) } }
        )
    }

    private fun connect() {
        val prefs = getSharedPreferences("imagent", MODE_PRIVATE)
        val code = prefs.getString("code", "") ?: ""
        if (code.isEmpty()) {
            updateStatus(McpClient.Status.DISCONNECTED)
            return
        }
        mcp.connect(code)
    }

    private fun updateStatus(s: McpClient.Status) {
        when (s) {
            McpClient.Status.CONNECTED -> {
                connected = true
                statusDot.setBackgroundResource(R.drawable.status_dot_online)
                statusText.text = "● 在线"
                sendBtn.isEnabled = inputText.text?.isNotBlank() == true
            }
            McpClient.Status.CONNECTING -> {
                statusDot.setBackgroundResource(R.drawable.status_dot_connecting)
                statusText.text = "连接中..."
            }
            McpClient.Status.DISCONNECTED -> {
                connected = false
                statusDot.setBackgroundResource(R.drawable.status_dot_offline)
                statusText.text = "断开"
                sendBtn.isEnabled = false
            }
            McpClient.Status.ERROR -> {
                connected = false
                statusDot.setBackgroundResource(R.drawable.status_dot_error)
                statusText.text = "错误"
            }
        }
    }

    // ── Mode switching ──

    private fun setMode(voice: Boolean) {
        isVoiceMode = voice
        voiceContainer.visibility = if (voice) View.VISIBLE else View.GONE
        textContainer.visibility = if (voice) View.GONE else View.VISIBLE
        if (!voice) inputText.requestFocus()
    }

    private fun sendText() {
        val text = inputText.text.toString().trim()
        if (text.isEmpty() || !connected) return
        mcp.sendText(text)
        addBubble(text, true)
        inputText.text.clear()
    }

    private fun addBubble(text: String, isUser: Boolean) {
        val bubble = TextView(this).apply {
            setText(text)
            setTextColor(0xFFE0E0E0.toInt())
            setPadding(24, 14, 24, 14)
            textSize = 15f
            background = GradientDrawable().apply {
                setColor(if (isUser) 0xFF1E1E3A.toInt() else 0xFF25254A.toInt())
                cornerRadius = 40f
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

    override fun onDestroy() {
        voice.shutdown()
        mcp.disconnect()
        super.onDestroy()
    }
}
