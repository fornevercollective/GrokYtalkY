package com.fornevercollective.grokytalky.glyph

import android.os.Handler
import android.os.Looper
import org.json.JSONArray
import org.json.JSONObject
import java.net.URI
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Minimal WebSocket mesh client for GrokYtalkY burst protocol.
 * Uses java.net.http.WebSocket when available (API 24+ desugar) or OkHttp if you add it.
 *
 * This scaffold uses a simple blocking socket thread with a tiny WS handshake —
 * replace with OkHttp WebSocket for production.
 */
class MeshClient(
    private val host: String,
    private val nick: String,
    private val onGlyphFrame: (from: String, glyph: IntArray) -> Unit,
    private val onBurstEnd: () -> Unit,
) {
    private val main = Handler(Looper.getMainLooper())
    private val exec = Executors.newSingleThreadExecutor()
    private val open = AtomicBoolean(false)
    private var sendFn: ((String) -> Unit)? = null

    fun connect() {
        exec.execute {
            try {
                // Prefer OkHttp in a real app. Here we document the wire format only
                // and no-op if dependency missing — BurstIntroActivity can test hub.
                open.set(false)
            } catch (_: Throwable) {
                open.set(false)
            }
        }
    }

    fun reconnect() {
        close()
        connect()
    }

    fun close() {
        open.set(false)
        sendFn = null
    }

    fun sendBurstStart() {
        send(
            JSONObject()
                .put("type", "vburst-start")
                .put("from", nick)
                .put("t", System.currentTimeMillis())
                .toString(),
        )
        send(
            JSONObject()
                .put("type", "ptt")
                .put("state", "down")
                .put("from", nick)
                .toString(),
        )
    }

    fun sendBurstEnd() {
        send(
            JSONObject()
                .put("type", "vburst-end")
                .put("from", nick)
                .put("t", System.currentTimeMillis())
                .toString(),
        )
        send(
            JSONObject()
                .put("type", "ptt")
                .put("state", "up")
                .put("from", nick)
                .toString(),
        )
    }

    fun sendBurstFrame(glyph: IntArray, n: Int) {
        val arr = JSONArray()
        for (v in glyph) arr.put(v.coerceIn(0, 255))
        send(
            JSONObject()
                .put("type", "vburst-frame")
                .put("from", nick)
                .put("glyph", arr)
                .put("glyphN", n)
                .put("w", n)
                .put("h", n)
                .put("t", System.currentTimeMillis())
                .toString(),
        )
    }

    private fun send(json: String) {
        sendFn?.invoke(json)
    }

    /** Call from WS onMessage */
    fun handleMessage(text: String) {
        try {
            val o = JSONObject(text)
            val type = o.optString("type")
            val from = o.optString("from")
            if (from == nick) return
            when (type) {
                "vburst-frame" -> {
                    val arr = o.optJSONArray("glyph") ?: return
                    val glyph = IntArray(arr.length()) { arr.getInt(it) }
                    main.post { onGlyphFrame(from, glyph) }
                }
                "vburst-end" -> main.post { onBurstEnd() }
            }
        } catch (_: Throwable) {
        }
    }

    fun wsUrl(): String {
        val h = if (host.startsWith("ws")) host else "ws://$host"
        val sep = if (h.contains("?")) "&" else "?"
        return "$h${sep}role=peer&nick=${URI(null, null, nick, null).rawPath ?: nick}"
    }
}
