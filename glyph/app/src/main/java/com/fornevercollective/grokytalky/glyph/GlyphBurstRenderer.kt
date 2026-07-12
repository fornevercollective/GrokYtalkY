package com.fornevercollective.grokytalky.glyph

import com.nothing.ketchum.GlyphMatrixManager
import kotlin.math.cos
import kotlin.math.sin
import kotlin.math.sqrt

/**
 * Maps burst face grids onto GlyphMatrixManager frames.
 * Prefer setMatrixFrame inside a Glyph Toy; setAppMatrixFrame from Activities.
 */
class GlyphBurstRenderer(
    private val gm: GlyphMatrixManager?,
    private val n: Int,
) {
    private var phase = 0f

    fun idle() {
        val data = IntArray(n * n)
        val cx = (n - 1) / 2f
        val cy = (n - 1) / 2f
        val r = n * 0.42f
        for (y in 0 until n) {
            for (x in 0 until n) {
                val d = sqrt((x - cx) * (x - cx) + (y - cy) * (y - cy))
                val ring = if (d > r - 1.2f && d < r + 0.8f) 80 else 8
                data[y * n + x] = ring
            }
        }
        // soft center dot
        data[(n / 2) * n + n / 2] = 120
        push(data)
    }

    fun idleAod() {
        // dimmer idle for AOD minute ticks
        val data = IntArray(n * n) { 4 }
        data[(n / 2) * n + n / 2] = 40
        push(data)
    }

    fun showTx() {
        pulse()
    }

    fun pulse() {
        phase += 0.35f
        val data = IntArray(n * n)
        val cx = (n - 1) / 2f
        val cy = (n - 1) / 2f
        for (y in 0 until n) {
            for (x in 0 until n) {
                val d = sqrt((x - cx) * (x - cx) + (y - cy) * (y - cy))
                val wave = (sin(d * 0.9f - phase) * 0.5f + 0.5f)
                val v = (wave * 180).toInt().coerceIn(0, 255)
                data[y * n + x] = if (d < n * 0.48f) v else (v * 0.3f).toInt()
            }
        }
        push(data)
    }

    fun showRemote(from: String, glyph: IntArray) {
        if (glyph.size == n * n) {
            push(glyph)
            return
        }
        // resample arbitrary length → n×n
        val srcN = sqrt(glyph.size.toDouble()).toInt().coerceAtLeast(1)
        val data = IntArray(n * n)
        for (y in 0 until n) {
            for (x in 0 until n) {
                val sx = x * srcN / n
                val sy = y * srcN / n
                data[y * n + x] = glyph.getOrElse(sy * srcN + sx) { 0 }.coerceIn(0, 255)
            }
        }
        push(data)
    }

    /** Placeholder local glyph — replace with CameraX → luminance N×N. */
    fun captureLocalGlyph(): IntArray {
        phase += 0.5f
        val data = IntArray(n * n)
        val cx = (n - 1) / 2f + cos(phase) * 1.2f
        val cy = (n - 1) / 2f
        for (y in 0 until n) {
            for (x in 0 until n) {
                val d = sqrt((x - cx) * (x - cx) + (y - cy) * (y - cy))
                var v = if (d < n * 0.28f) 200 else 30
                // eyes
                if (sqrt((x - (cx - 3)) * (x - (cx - 3)) + (y - (cy - 2)) * (y - (cy - 2))) < 1.3f) v = 10
                if (sqrt((x - (cx + 3)) * (x - (cx + 3)) + (y - (cy - 2)) * (y - (cy - 2))) < 1.3f) v = 10
                data[y * n + x] = v
            }
        }
        push(data)
        return data
    }

    private fun push(data: IntArray) {
        try {
            // Toy context: setMatrixFrame. App context would use setAppMatrixFrame.
            gm?.setMatrixFrame(data)
        } catch (_: Throwable) {
            try {
                gm?.setAppMatrixFrame(data)
            } catch (_: Throwable) {
            }
        }
    }
}
