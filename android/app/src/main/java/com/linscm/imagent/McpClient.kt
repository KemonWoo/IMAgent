package com.linscm.imagent

import android.os.Handler
import android.os.Looper
import android.util.Log
import com.google.gson.Gson
import com.google.gson.JsonObject
import com.google.gson.JsonParser
import org.java_websocket.client.WebSocketClient
import org.java_websocket.handshake.ServerHandshake
import java.net.URI
import java.security.cert.X509Certificate
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLSocketFactory
import javax.net.ssl.TrustManager
import javax.net.ssl.X509TrustManager

/**
 * MCP WebSocket client for connecting to IMAgent Relay.
 * Handles handshake, message routing, and auto-reconnect.
 */
class McpClient(
    private var relayUrl: String,
    private val onMessage: (String, JsonObject) -> Unit,
    private val onStatus: (Status) -> Unit
) {
    enum class Status { CONNECTING, CONNECTED, DISCONNECTED, ERROR }

    private var ws: WebSocketClient? = null
    private val handler = Handler(Looper.getMainLooper())
    private val gson = Gson()
    private var reconnectAttempts = 0
    private val maxReconnectAttempts = 5

    private var lastCode: String = ""

    fun setRelayUrl(url: String) { relayUrl = url }

    fun connect(code: String) {
        lastCode = code
        disconnect()
        Log.i(TAG, "connect: relayUrl=[$relayUrl] code=[$code]")
        try {
            val uri = buildUri(relayUrl)
            Log.i(TAG, "connect: uri=$uri")
            ws = object : WebSocketClient(uri) {
                override fun onOpen(handshake: ServerHandshake?) {
                    Log.i(TAG, "WS opened, sending handshake")
                    val hs = mapOf("role" to "apk", "code" to code)
                    send(gson.toJson(hs))
                }

                override fun onMessage(message: String?) {
                    message ?: return
                    try {
                        val json = JsonParser.parseString(message).asJsonObject
                        val type = json.get("type")?.asString ?: json.get("status")?.asString

                        when (type) {
                            "connected" -> {
                                reconnectAttempts = 0
                                handler.post { onStatus(Status.CONNECTED) }
                                Log.i(TAG, "Connected to relay")
                            }
                            "code_mismatch" -> {
                                handler.post { onStatus(Status.ERROR) }
                                Log.w(TAG, "Code mismatch")
                            }
                            "chat_response", "tts", "file" -> {
                                val content = json.get("content")?.asString ?: ""
                                handler.post { onMessage(type, json) }
                            }
                            "reset" -> {
                                handler.post { onStatus(Status.DISCONNECTED) }
                            }
                        }
                    } catch (e: Exception) {
                        Log.e(TAG, "Parse error: ${e.message}")
                    }
                }

                override fun onClose(code: Int, reason: String?, remote: Boolean) {
                    Log.w(TAG, "WS closed: $reason")
                    handler.post { onStatus(Status.DISCONNECTED) }
                    scheduleReconnect()
                }

                override fun onError(ex: Exception?) {
                    Log.e(TAG, "WS error: ${ex?.message}")
                    handler.post { onStatus(Status.ERROR) }
                    scheduleReconnect()
                }
            }

            handler.post { onStatus(Status.CONNECTING) }
            if (uri.scheme == "wss") {
                ws?.setSocketFactory(trustAllFactory())
            }
            ws?.connect()
        } catch (e: Exception) {
            Log.e(TAG, "Connect failed: ${e.message}")
            handler.post { onStatus(Status.ERROR) }
        }
    }

    fun sendText(text: String): Boolean {
        val msg = gson.toJson(mapOf("type" to "chat", "content" to text))
        return try {
            ws?.send(msg) != null
        } catch (e: Exception) {
            false
        }
    }

    fun disconnect() {
        reconnectAttempts = maxReconnectAttempts // prevent reconnect
        try { ws?.close() } catch (_: Exception) {}
        ws = null
    }

    private fun scheduleReconnect() {
        reconnectAttempts++
        if (reconnectAttempts > maxReconnectAttempts) {
            Log.w(TAG, "Max reconnect attempts reached")
            return
        }
        val delay = (reconnectAttempts * 2000L).coerceAtMost(10000L)
        handler.postDelayed({
            Log.i(TAG, "Reconnecting (attempt $reconnectAttempts)...")
            connect(lastCode)
        }, delay)
    }

    companion object {
        private const val TAG = "IMAgent-MCP"

        /** Build WebSocket URI: if URL already has ws:// or wss://, use as-is;
         *  otherwise default to wss:// for TLS relay. */
        private fun buildUri(url: String): URI {
            val cleaned = url.trim()
            return if (cleaned.startsWith("ws://") || cleaned.startsWith("wss://")) {
                URI("$cleaned/apk")
            } else {
                URI("wss://$cleaned/apk")
            }
        }

        /** Create an SSLSocketFactory that trusts all certificates (self-signed). */
        private fun trustAllFactory(): SSLSocketFactory {
            val trustAll = arrayOf<TrustManager>(object : X509TrustManager {
                override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) {}
                override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {}
                override fun getAcceptedIssuers(): Array<X509Certificate> = arrayOf()
            })
            val ctx = SSLContext.getInstance("TLS")
            ctx.init(null, trustAll, java.security.SecureRandom())
            return ctx.socketFactory
        }
    }
}
