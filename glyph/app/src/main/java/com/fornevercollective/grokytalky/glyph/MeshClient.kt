package com.fornevercollective.grokytalky.glyph

import android.os.Handler
import android.os.Looper
import org.json.JSONArray
import org.json.JSONObject
import java.io.BufferedInputStream
import java.io.BufferedOutputStream
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.HttpURLConnection
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.Socket
import java.net.URI
import java.net.URL
import java.nio.ByteBuffer
import java.nio.charset.StandardCharsets
import java.util.Base64
import java.util.concurrent.Executors
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.concurrent.thread

/**
 * WebSocket mesh client for GrokYtalkY phone → terminal on same Wi‑Fi.
 *
 * Wire: join · vburst-start/frame/end · ptt · gyst hexlum (dual).
 * Discovery: GET http://IP:9876/api/lan · UDP GYWHO1 on port 9877.
 *
 * Minimal RFC6455 text client (no OkHttp required). Prefer OkHttp in production.
 */
class MeshClient(
    private var host: String,
    private val nick: String,
    private val onGlyphFrame: (from: String, glyph: IntArray) -> Unit,
    private val onBurstEnd: () -> Unit,
    private val onStatus: (String) -> Unit = {},
) {
    private val main = Handler(Looper.getMainLooper())
    private val exec = Executors.newSingleThreadExecutor()
    private val open = AtomicBoolean(false)
    private val outQ = LinkedBlockingQueue<String>(64)
    @Volatile private var socket: Socket? = null
    @Volatile private var out: BufferedOutputStream? = null

    fun connect() {
        exec.execute {
            try {
                closeInternal()
                val url = wsUrl()
                onStatusMain("dial $url")
                val uri = URI(url)
                val port = if (uri.port > 0) uri.port else 9876
                val hostName = uri.host ?: "127.0.0.1"
                val path = buildString {
                    append(if (uri.rawPath.isNullOrEmpty()) "/" else uri.rawPath)
                    if (!uri.rawQuery.isNullOrEmpty()) append("?").append(uri.rawQuery)
                }
                val sock = Socket()
                sock.tcpNoDelay = true
                sock.connect(InetSocketAddress(hostName, port), 8_000)
                val inp = BufferedInputStream(sock.getInputStream())
                val oup = BufferedOutputStream(sock.getOutputStream())
                val key = Base64.getEncoder().encodeToString(
                    ByteArray(16).also { java.security.SecureRandom().nextBytes(it) },
                )
                val req = buildString {
                    append("GET $path HTTP/1.1\r\n")
                    append("Host: $hostName:$port\r\n")
                    append("Upgrade: websocket\r\n")
                    append("Connection: Upgrade\r\n")
                    append("Sec-WebSocket-Key: $key\r\n")
                    append("Sec-WebSocket-Version: 13\r\n")
                    append("\r\n")
                }
                oup.write(req.toByteArray(StandardCharsets.US_ASCII))
                oup.flush()
                // read handshake
                val hdr = readHttpHeaders(inp)
                if (!hdr.contains("101") && !hdr.lowercase().contains("upgrade")) {
                    sock.close()
                    onStatusMain("handshake fail")
                    return@execute
                }
                socket = sock
                out = oup
                open.set(true)
                onStatusMain("hub ok")
                // writer
                thread(name = "gy-ws-write", isDaemon = true) {
                    while (open.get()) {
                        val msg = try {
                            outQ.take()
                        } catch (_: InterruptedException) {
                            break
                        }
                        try {
                            writeTextFrame(oup, msg)
                        } catch (_: Throwable) {
                            open.set(false)
                            break
                        }
                    }
                }
                // join
                send(
                    JSONObject()
                        .put("type", "join")
                        .put("nick", nick)
                        .put("role", "phone")
                        .put(
                            "cap",
                            JSONObject()
                                .put("class", "glyph-iot")
                                .put("role", "phone")
                                .put("glyph_n", 25),
                        )
                        .toString(),
                )
                // reader
                while (open.get()) {
                    val text = readTextFrame(inp) ?: break
                    handleMessage(text)
                }
            } catch (t: Throwable) {
                onStatusMain("connect: ${t.message}")
            } finally {
                open.set(false)
                closeInternal()
            }
        }
    }

    fun reconnect() {
        close()
        connect()
    }

    fun close() {
        open.set(false)
        exec.execute { closeInternal() }
    }

    private fun closeInternal() {
        try {
            out?.close()
        } catch (_: Throwable) {
        }
        try {
            socket?.close()
        } catch (_: Throwable) {
        }
        out = null
        socket = null
        outQ.clear()
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

    /** TX glyph lattice as vburst + formal gyst hexlum (terminal / SFU). */
    fun sendBurstFrame(glyph: IntArray, n: Int) {
        val arr = JSONArray()
        for (v in glyph) arr.put(v.coerceIn(0, 255))
        val t = System.currentTimeMillis()
        send(
            JSONObject()
                .put("type", "vburst-frame")
                .put("from", nick)
                .put("fmt", "hexlum")
                .put("glyph", arr)
                .put("glyphN", n)
                .put("w", n)
                .put("h", n)
                .put("t", t)
                .put("via", "android-phone")
                .toString(),
        )
        // dual: gyst hexlum for gy join peers + sfu-bridge
        send(
            JSONObject()
                .put("type", "gyst")
                .put("from", nick)
                .put("kind", "hexlum")
                .put("w", n)
                .put("h", n)
                .put("seq", t % Int.MAX_VALUE)
                .put("t", t)
                .put("data", arr)
                .put("glyphN", n)
                .put("lane", "hex")
                .put("via", "android-phone")
                .toString(),
        )
    }

    private fun send(json: String) {
        if (!open.get()) return
        if (!outQ.offer(json)) {
            outQ.poll()
            outQ.offer(json)
        }
    }

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
                "gyst", "gyst-frame" -> {
                    if (o.optString("kind") == "hexlum") {
                        val arr = o.optJSONArray("data") ?: return
                        val glyph = IntArray(arr.length()) { arr.getInt(it) }
                        main.post { onGlyphFrame(from, glyph) }
                    }
                }
                "vburst-end" -> main.post { onBurstEnd() }
            }
        } catch (_: Throwable) {
        }
    }

    fun wsUrl(): String {
        var h = host.trim()
        if (h.startsWith("http://")) h = "ws://" + h.removePrefix("http://")
        if (h.startsWith("https://")) h = "wss://" + h.removePrefix("https://")
        if (!h.startsWith("ws")) h = "ws://$h"
        if (!h.contains("?")) {
            if (!h.endsWith("/")) h += "/"
        }
        val sep = if (h.contains("?")) "&" else "?"
        return "$h${sep}role=phone&nick=${URI(null, null, nick, null).rawPath ?: nick}"
    }

    fun setHost(h: String) {
        host = h
    }

    private fun onStatusMain(s: String) {
        main.post { onStatus(s) }
    }

    companion object {
        private const val WHO = "GYWHO1"
        private const val HUB = "GYHUB1"

        /**
         * Discover hubs on the same Wi‑Fi via UDP broadcast + /api/lan fallback.
         * Returns first preferred ws:// URL or empty.
         */
        @JvmStatic
        fun discoverHub(timeoutMs: Int = 1500): String {
            // UDP broadcast probe
            try {
                DatagramSocket().use { sock ->
                    sock.broadcast = true
                    sock.soTimeout = timeoutMs
                    val payload = WHO.toByteArray(StandardCharsets.US_ASCII)
                    val bcast = DatagramPacket(
                        payload,
                        payload.size,
                        InetAddress.getByName("255.255.255.255"),
                        9877,
                    )
                    sock.send(bcast)
                    // multicast group used by hub
                    try {
                        val mcast = DatagramPacket(
                            payload,
                            payload.size,
                            InetAddress.getByName("239.255.76.67"),
                            9877,
                        )
                        sock.send(mcast)
                    } catch (_: Throwable) {
                    }
                    val buf = ByteArray(4096)
                    val resp = DatagramPacket(buf, buf.size)
                    sock.receive(resp)
                    val raw = String(resp.data, 0, resp.length, StandardCharsets.UTF_8)
                    if (raw.startsWith(HUB)) {
                        val json = JSONObject(raw.removePrefix(HUB))
                        val ws = json.optString("ws")
                        if (ws.isNotEmpty()) return ws
                    }
                }
            } catch (_: Throwable) {
            }
            return ""
        }

        /** HTTP GET /api/lan on a known host:port */
        @JvmStatic
        fun fetchLanWs(httpBase: String): String {
            return try {
                var base = httpBase.trim()
                if (!base.startsWith("http")) base = "http://$base"
                val url = URL(base.trimEnd('/') + "/api/lan")
                val c = url.openConnection() as HttpURLConnection
                c.connectTimeout = 2000
                c.readTimeout = 2000
                c.requestMethod = "GET"
                c.setRequestProperty("Accept", "application/json")
                c.inputStream.bufferedReader().use { r ->
                    val o = JSONObject(r.readText())
                    o.optString("ws")
                }
            } catch (_: Throwable) {
                ""
            }
        }

        private fun readHttpHeaders(inp: BufferedInputStream): String {
            val sb = StringBuilder()
            while (true) {
                val line = readLine(inp) ?: break
                sb.append(line).append('\n')
                if (line.isEmpty()) break
            }
            return sb.toString()
        }

        private fun readLine(inp: BufferedInputStream): String? {
            val sb = StringBuilder()
            while (true) {
                val b = inp.read()
                if (b < 0) return if (sb.isEmpty()) null else sb.toString()
                if (b == '\n'.code) break
                if (b != '\r'.code) sb.append(b.toChar())
            }
            return sb.toString()
        }

        private fun writeTextFrame(out: BufferedOutputStream, text: String) {
            val data = text.toByteArray(StandardCharsets.UTF_8)
            val mask = ByteArray(4).also { java.security.SecureRandom().nextBytes(it) }
            val len = data.size
            out.write(0x81) // FIN + text
            when {
                len < 126 -> out.write(0x80 or len)
                len < 65536 -> {
                    out.write(0x80 or 126)
                    out.write((len shr 8) and 0xff)
                    out.write(len and 0xff)
                }
                else -> {
                    out.write(0x80 or 127)
                    val bb = ByteBuffer.allocate(8).putLong(len.toLong())
                    out.write(bb.array())
                }
            }
            out.write(mask)
            for (i in data.indices) {
                out.write(data[i].toInt() xor mask[i % 4].toInt())
            }
            out.flush()
        }

        private fun readTextFrame(inp: BufferedInputStream): String? {
            val b0 = inp.read()
            if (b0 < 0) return null
            val b1 = inp.read()
            if (b1 < 0) return null
            val masked = (b1 and 0x80) != 0
            var len = b1 and 0x7f
            when (len) {
                126 -> {
                    val hi = inp.read()
                    val lo = inp.read()
                    len = (hi shl 8) or lo
                }
                127 -> {
                    var l = 0L
                    repeat(8) { l = (l shl 8) or inp.read().toLong() }
                    len = l.toInt()
                }
            }
            val mask = ByteArray(4)
            if (masked) {
                if (inp.read(mask) != 4) return null
            }
            val data = ByteArray(len)
            var off = 0
            while (off < len) {
                val n = inp.read(data, off, len - off)
                if (n < 0) return null
                off += n
            }
            if (masked) {
                for (i in data.indices) data[i] = (data[i].toInt() xor mask[i % 4].toInt()).toByte()
            }
            val opcode = b0 and 0x0f
            if (opcode == 0x8) return null // close
            if (opcode != 0x1 && opcode != 0x0) return "" // skip binary/ping
            return String(data, StandardCharsets.UTF_8)
        }
    }
}
