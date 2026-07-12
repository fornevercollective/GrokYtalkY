package com.fornevercollective.grokytalky.glyph

import android.app.Service
import android.content.ComponentName
import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.Message
import android.os.Messenger
import com.nothing.ketchum.Glyph
import com.nothing.ketchum.GlyphMatrixManager
import com.nothing.ketchum.GlyphToy

/**
 * Glyph Toy — Siri-sized video walkie burst.
 *
 * Glyph Button:
 *  - action_down → start burst (TX face grid + mic via mesh)
 *  - action_up   → end burst
 *  - change      → reconnect / pulse
 *  - aod         → dim idle face
 *
 * SDK: https://github.com/Nothing-Developer-Programme/GlyphMatrix-Developer-Kit
 */
class BurstToyService : Service() {

    private var gm: GlyphMatrixManager? = null
    private var mesh: MeshClient? = null
    private var renderer: GlyphBurstRenderer? = null
    private var matrixN = 25
    private var tx = false

    private val handler = Handler(Looper.getMainLooper()) { msg ->
        if (msg.what == GlyphToy.MSG_GLYPH_TOY) {
            val event = msg.data?.getString(GlyphToy.MSG_GLYPH_TOY_DATA) ?: return@Handler true
            when (event) {
                GlyphToy.EVENT_CHANGE -> {
                    // long press: reconnect hub
                    mesh?.reconnect()
                    renderer?.pulse()
                }
                // touch-down / touch-up names follow SDK event strings
                "action_down", GlyphToy.EVENT_CHANGE /* fallback */ -> {
                    // some SDK builds map hold via separate events — also try:
                }
                else -> {
                    when (event) {
                        "action_down" -> startBurst()
                        "action_up" -> stopBurst()
                        GlyphToy.EVENT_AOD -> renderer?.idleAod()
                    }
                }
            }
            // explicit down/up (SDK touch events)
            if (event == "action_down") startBurst()
            if (event == "action_up") stopBurst()
        }
        true
    }
    private val messenger = Messenger(handler)

    override fun onBind(intent: Intent?): IBinder {
        initMatrix()
        connectMesh()
        return messenger.binder
    }

    override fun onUnbind(intent: Intent?): Boolean {
        stopBurst()
        mesh?.close()
        mesh = null
        gm?.unInit()
        gm = null
        return false
    }

    private fun initMatrix() {
        gm = GlyphMatrixManager.getInstance(applicationContext)
        gm?.init(object : GlyphMatrixManager.Callback {
            override fun onServiceConnected(name: ComponentName?) {
                // Phone (3) = DEVICE_23112 (25), Phone (4a) Pro = DEVICE_25111p (13)
                try {
                    gm?.register(Glyph.DEVICE_23112)
                    matrixN = 25
                } catch (_: Throwable) {
                    try {
                        gm?.register(Glyph.DEVICE_25111p)
                        matrixN = 13
                    } catch (_: Throwable) {
                    }
                }
                renderer = GlyphBurstRenderer(gm, matrixN)
                renderer?.idle()
            }

            override fun onServiceDisconnected(name: ComponentName?) {}
        })
    }

    private fun connectMesh() {
        val prefs = getSharedPreferences("gy", MODE_PRIVATE)
        val host = prefs.getString("hub", "127.0.0.1:9876") ?: "127.0.0.1:9876"
        val nick = prefs.getString("nick", "glyph") ?: "glyph"
        mesh = MeshClient(
            host = host,
            nick = nick,
            onGlyphFrame = { from, glyph ->
                if (!tx) renderer?.showRemote(from, glyph)
            },
            onBurstEnd = {
                if (!tx) renderer?.idle()
            },
        )
        mesh?.connect()
    }

    private fun startBurst() {
        if (tx) return
        tx = true
        mesh?.sendBurstStart()
        // Camera capture + mic should be started here (CameraX + AudioRecord).
        // For the scaffold we push a synthetic pulse glyph; replace with real frames.
        renderer?.showTx()
        handler.post(object : Runnable {
            override fun run() {
                if (!tx) return
                val frame = renderer?.captureLocalGlyph() ?: return
                mesh?.sendBurstFrame(frame, matrixN)
                handler.postDelayed(this, 160L)
            }
        })
    }

    private fun stopBurst() {
        if (!tx) return
        tx = false
        mesh?.sendBurstEnd()
        renderer?.idle()
    }
}
