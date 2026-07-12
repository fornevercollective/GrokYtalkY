package com.fornevercollective.grokytalky.glyph

import android.app.Activity
import android.content.ComponentName
import android.content.Intent
import android.os.Bundle
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.TextView

/**
 * Lightweight hub config + deep-link into Glyph Toys manager.
 */
class BurstIntroActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val prefs = getSharedPreferences("gy", MODE_PRIVATE)

        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(48, 48, 48, 48)
        }
        val title = TextView(this).apply {
            text = "GrokYtalkY Burst"
            textSize = 22f
        }
        val blurb = TextView(this).apply {
            text = "Siri-sized video walkie on the Glyph Matrix.\n" +
                "Hold Glyph Button = TX face grid + voice (mesh).\n" +
                "Hub default: 127.0.0.1:9876 (use LAN IP for real devices)."
            textSize = 14f
        }
        val hub = EditText(this).apply {
            setText(prefs.getString("hub", "127.0.0.1:9876"))
            hint = "host:port"
        }
        val nick = EditText(this).apply {
            setText(prefs.getString("nick", "glyph"))
            hint = "nick"
        }
        val save = Button(this).apply {
            text = "Save hub"
            setOnClickListener {
                prefs.edit()
                    .putString("hub", hub.text.toString().trim())
                    .putString("nick", nick.text.toString().trim())
                    .apply()
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
                    blurb.text = "Glyph Toys manager not found — install on a Nothing Phone (3)."
                }
            }
        }

        root.addView(title)
        root.addView(blurb)
        root.addView(hub)
        root.addView(nick)
        root.addView(save)
        root.addView(toys)
        setContentView(root)
    }
}
