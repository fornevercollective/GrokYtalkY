package com.fornevercollective.grokytalky.glyph

import android.app.Activity
import android.content.ComponentName
import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import kotlin.concurrent.thread

/**
 * Hub config + same-WiFi discovery + deep-link into Glyph Toys manager.
 *
 * Phone → terminal: discover hub on LAN (UDP GYWHO1 /api/lan), save ws URL,
 * then hold Glyph Button to cast hexlum/vburst to gy join peers.
 */
class BurstIntroActivity : Activity() {
    private val main = Handler(Looper.getMainLooper())

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val prefs = getSharedPreferences("gy", MODE_PRIVATE)

        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(48, 48, 48, 48)
        }
        val title = TextView(this).apply {
            text = "GrokYtalkY · phone → terminal"
            textSize = 22f
        }
        val blurb = TextView(this).apply {
            text = "Same Wi‑Fi as the laptop running gy serve.\n" +
                "Discover fills hub, or paste LAN IP from the terminal banner.\n" +
                "Hold Glyph Button = cast face grid + mesh hexlum to terminal."
            textSize = 14f
        }
        val hub = EditText(this).apply {
            setText(prefs.getString("hub", "192.168.1.1:9876"))
            hint = "host:port or ws://LAN:9876/"
        }
        val nick = EditText(this).apply {
            setText(prefs.getString("nick", "glyph"))
            hint = "nick"
        }
        val status = TextView(this).apply {
            text = "idle"
            textSize = 13f
        }
        val discover = Button(this).apply {
            text = "Discover on Wi‑Fi"
            setOnClickListener {
                status.text = "scanning UDP 9877…"
                thread {
                    val ws = MeshClient.discoverHub(2000)
                    main.post {
                        if (ws.isNotEmpty()) {
                            hub.setText(ws)
                            status.text = "found $ws"
                            prefs.edit().putString("hub", ws).apply()
                            Toast.makeText(this@BurstIntroActivity, "Hub found", Toast.LENGTH_SHORT).show()
                        } else {
                            // try HTTP if user already typed an IP
                            val typed = hub.text.toString().trim()
                            val httpBase = typed
                                .removePrefix("ws://")
                                .removePrefix("wss://")
                                .removePrefix("http://")
                                .removePrefix("https://")
                                .substringBefore("/")
                            val viaHttp = if (httpBase.isNotEmpty()) {
                                MeshClient.fetchLanWs("http://$httpBase")
                            } else {
                                ""
                            }
                            if (viaHttp.isNotEmpty()) {
                                hub.setText(viaHttp)
                                status.text = "found via /api/lan $viaHttp"
                                prefs.edit().putString("hub", viaHttp).apply()
                            } else {
                                status.text = "no hub · run gy serve on laptop (same Wi‑Fi)"
                            }
                        }
                    }
                }
            }
        }
        val save = Button(this).apply {
            text = "Save hub"
            setOnClickListener {
                prefs.edit()
                    .putString("hub", hub.text.toString().trim())
                    .putString("nick", nick.text.toString().trim())
                    .apply()
                status.text = "saved"
            }
        }
        val web = Button(this).apply {
            text = "Open phone cast (browser)"
            setOnClickListener {
                val h = hub.text.toString().trim()
                    .removePrefix("ws://")
                    .removePrefix("wss://")
                    .removePrefix("http://")
                    .removePrefix("https://")
                    .substringBefore("/")
                    .ifEmpty { "127.0.0.1:9876" }
                try {
                    val i = Intent(Intent.ACTION_VIEW, android.net.Uri.parse("http://$h/phone.html"))
                    startActivity(i)
                } catch (t: Throwable) {
                    status.text = "open browser: ${t.message}"
                }
            }
        }
        val toys = Button(this).apply {
            text = "Open Glyph Toys manager"
            setOnClickListener {
                try {
                    val i = Intent()
                    i.component = ComponentName(
                        "com.nothing.thirdparty",
                        "com.nothing.thirdparty.matrix.toys.manager.ToysManagerActivity",
                    )
                    startActivity(i)
                } catch (_: Throwable) {
                    blurb.text = "Glyph Toys manager not found — use browser cast on any phone:\n" +
                        "open http://LAPTOP_IP:9876/phone.html"
                }
            }
        }

        root.addView(title)
        root.addView(blurb)
        root.addView(hub)
        root.addView(nick)
        root.addView(discover)
        root.addView(save)
        root.addView(web)
        root.addView(toys)
        root.addView(status)
        setContentView(root)
    }
}
