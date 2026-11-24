package ai.pebble.android.ui.theme

import android.provider.Settings
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.rotate
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.cos
import kotlin.math.exp
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min
import kotlin.math.sin

private class Mulberry32(private var a: UInt) {
    fun next(): Double {
        a += 0x6d2b79f5u
        var t = a
        t = (t xor (t shr 15)) * (t or 1u)
        t = t xor (t + ((t xor (t shr 7)) * (t or 61u)))
        val out = t xor (t shr 14)
        return out.toDouble() / 4294967296.0
    }
}

private data class PebbleVert(val s: Double, val d: Double, val p: Double)

private data class PebbleParams(
    val n: Int,
    val verts: List<PebbleVert>,
    val rot0: Double,
    val tilt: Double,
    val sx: Double,
    val corner: Double,
    val side: Double,
    val breath: Double,
    val blinkEvery: Double,
    val blinkOff: Double,
    val lookEvery: Double,
    val lookOff: Double,
    val lookSeed: UInt,
    val bob: Double,
)

private data class PebblePressFX(val sx: Double, val sy: Double, val dy: Double, val eye: Double)

private fun pebbleLerp(a: Double, b: Double, t: Double) = a + (b - a) * t

private fun buildPebbleParams(id: String): PebbleParams {
    val key = id.ifEmpty { "?" }
    val r = Mulberry32(fnv1a(key))
    val q = r.next()
    val n = when {
        q < 0.6 -> 4
        q < 0.85 -> 5
        else -> 6
    }
    val verts = List(n) {
        PebbleVert(
            s = 0.86 + r.next() * 0.24,
            d = (r.next() - 0.5) * 0.35,
            p = r.next() * 6.28,
        )
    }
    r.next()
    val sign = if (r.next() < 0.5) -1.0 else 1.0
    val tilt = sign * pebbleLerp(6.0, 12.0, r.next()) / 57.3
    r.next()
    return PebbleParams(
        n = n,
        verts = verts,
        rot0 = PI / 2.0 - PI / n.toDouble(),
        tilt = tilt,
        sx = pebbleLerp(0.95, 1.08, r.next()),
        corner = pebbleLerp(0.36, 0.44, r.next()),
        side = -sign,
        breath = 0.5 + r.next() * 0.4,
        blinkEvery = 2.8 + r.next() * 3.5,
        blinkOff = r.next() * 10.0,
        lookEvery = 2.2 + r.next() * 3.0,
        lookOff = r.next() * 10.0,
        lookSeed = fnv1a("$key#l"),
        bob = r.next() * 6.28,
    )
}

private fun DrawScope.drawPebble(
    params: PebbleParams,
    color: Color,
    eye: Color,
    t: Double,
    busy: Boolean,
    fx: PebblePressFX?,
) {
    val k = min(size.width, size.height) / 100f
    val drawBodyAndEyes: DrawScope.() -> Unit = {
        val circ = when (params.n) {
            3 -> 54.0
            4 -> 49.0
            5 -> 46.0
            else -> 45.0
        }
        val breathe = 1.0 + 0.018 * sin(t * params.breath * PI * (if (busy) 2.6 else 1.0))
        val bobY = sin(t * 1.2 + params.bob) * (if (busy) 1.2 else 0.6)
        val asym = 0.35

        val rawPts = ArrayList<Pair<Double, Double>>(params.n)
        var minX = Double.POSITIVE_INFINITY
        var maxX = Double.NEGATIVE_INFINITY
        var minY = Double.POSITIVE_INFINITY
        var maxY = Double.NEGATIVE_INFINITY
        for (i in 0 until params.n) {
            val v = params.verts[i]
            val a = params.rot0 + (i.toDouble() / params.n.toDouble()) * PI * 2.0 + v.d * asym * 0.5
            val rr = circ * (1.0 + (v.s - 1.0) * asym) * breathe
            val px = cos(a) * rr * params.sx
            val py = sin(a) * rr / params.sx
            minX = min(minX, px); maxX = max(maxX, px)
            minY = min(minY, py); maxY = max(maxY, py)
            rawPts.add(px to py)
        }
        val ox = 50.0 - (minX + maxX) / 2.0
        val oy = 50.0 + bobY - (minY + maxY) / 2.0
        val pts = rawPts.map { (px, py) ->
            Offset(((px + ox) * k).toFloat(), ((py + oy) * k).toFloat())
        }

        val c = min(0.5, params.corner).toFloat()
        fun cut(a: Offset, b: Offset): Offset =
            Offset(a.x + (b.x - a.x) * c, a.y + (b.y - a.y) * c)

        val bodyPath = Path()
        for (i in 0 until params.n) {
            val prev = pts[(i - 1 + params.n) % params.n]
            val curr = pts[i]
            val next = pts[(i + 1) % params.n]
            val p1 = cut(curr, prev)
            val p2 = cut(curr, next)
            if (i == 0) bodyPath.moveTo(p1.x, p1.y) else bodyPath.lineTo(p1.x, p1.y)
            bodyPath.quadraticTo(curr.x, curr.y, p2.x, p2.y)
        }
        bodyPath.close()

        val bodyRotDeg = (params.tilt * 57.3 + sin(t * 0.6 + params.bob) * (if (busy) 3.0 else 1.5)).toFloat()
        rotate(degrees = bodyRotDeg, pivot = Offset(50f * k, 50f * k)) {
            drawPath(bodyPath, color = color)
        }

        val bp = ((t + params.blinkOff) % params.blinkEvery) / params.blinkEvery
        val blink = if (bp > 0.965) max(0.12, abs(cos((bp - 0.965) / 0.035 * PI))) else 1.0

        val lx: Double
        val ly: Double
        if (busy) {
            lx = sin(t * 4.5) * 7.0
            ly = -1.0
        } else {
            val seg = (t + params.lookOff) / params.lookEvery
            val idx = floor(seg).toInt()
            val f = seg - idx.toDouble()
            fun target(i: Int): Pair<Double, Double> {
                val seed = params.lookSeed xor ((i + 1).toUInt() * 2654435761u)
                val q = Mulberry32(seed)
                val r1 = q.next()
                val r2 = q.next()
                val r3 = q.next()
                val x = params.side * (3.0 + r1 * 6.0) - (if (r2 < 0.2) params.side * 8.0 else 0.0)
                val y = -2.0 + r3 * 4.0
                return x to y
            }
            val a = target(idx)
            val b = target(idx + 1)
            val e = if (f < 0.85) 0.0 else (1.0 - cos((f - 0.85) / 0.15 * PI)) / 2.0
            lx = pebbleLerp(a.first, b.first, e)
            ly = pebbleLerp(a.second, b.second, e)
        }

        val ew = 5.0
        val eh = 9.5 * max(0.12, blink * (fx?.eye ?: 1.0))
        val gap = 19.0
        val lean = (-params.side * 14.0).toFloat()
        val ex = 50.0 + lx
        val ey = 47.0 + bobY + ly
        val rx = min(ew, eh)

        for (cx in listOf(ex - gap / 2.0, ex + gap / 2.0)) {
            val pivot = Offset((cx * k).toFloat(), (ey * k).toFloat())
            rotate(degrees = lean, pivot = pivot) {
                drawRoundRect(
                    color = eye,
                    topLeft = Offset(((cx - ew) * k).toFloat(), ((ey - eh) * k).toFloat()),
                    size = Size((ew * 2.0 * k).toFloat(), (eh * 2.0 * k).toFloat()),
                    cornerRadius = CornerRadius((rx * k).toFloat(), (rx * k).toFloat()),
                )
            }
        }
    }

    if (fx != null) {
        translate(left = 0f, top = (fx.dy * k).toFloat()) {
            scale(
                scaleX = fx.sx.toFloat(),
                scaleY = fx.sy.toFloat(),
                pivot = Offset(50f * k, 90f * k),
            ) {
                drawBodyAndEyes()
            }
        }
    } else {
        drawBodyAndEyes()
    }
}

@Composable
fun BotAvatar(
    id: String,
    size: Dp,
    modifier: Modifier = Modifier,
    index: Int = -1,
    avatar: String? = null,
    color: String? = null,
    busy: Boolean = false,
    interactive: Boolean = false,
) {
    val p = LocalPalette.current
    val context = LocalContext.current
    val reduceMotion = remember(context) {
        runCatching {
            Settings.Global.getFloat(context.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f) == 0f
        }.getOrDefault(false)
    }

    val blobColor = remember(id, index, color) { colorFromHex(botColorHex(id, index, color)) }
    val seed = remember(id, avatar) { resolvePebbleSeed(id, avatar) }
    val params = remember(seed) { buildPebbleParams(seed) }

    var startNanos by remember { mutableLongStateOf(0L) }
    var elapsedSec by remember { mutableStateOf(0.0) }
    var pressDown by remember { mutableStateOf(false) }
    var pressActive by remember { mutableStateOf(false) }
    var pressT0 by remember { mutableStateOf(0.0) }
    var pressT1 by remember { mutableStateOf(0.0) }

    LaunchedEffect(reduceMotion, pressActive) {
        if (reduceMotion && !pressActive) {
            elapsedSec = 0.0
            return@LaunchedEffect
        }
        var lastTickNanos = 0L
        val minIntervalNs = if (pressActive) 16_000_000L else 33_000_000L
        while (true) {
            withFrameNanos { frameNanos ->
                if (startNanos == 0L) startNanos = frameNanos
                if (frameNanos - lastTickNanos >= minIntervalNs) {
                    lastTickNanos = frameNanos
                    elapsedSec = (frameNanos - startNanos) / 1_000_000_000.0
                }
            }
        }
    }

    LaunchedEffect(pressDown, pressActive) {
        if (!pressDown && pressActive) {
            delay(1500)
            if (!pressDown) pressActive = false
        }
    }

    val fx: PebblePressFX? = if (pressActive) {
        val now = elapsedSec
        val d = if (pressDown) min(1.0, max(0.0, (now - pressT0) / 0.08)) else 0.0
        val r = if (pressDown) 0.0 else max(0.0, now - pressT1)
        if (!pressDown && r > 1.4) {
            null
        } else {
            val b = if (pressDown) 0.0 else exp(-5.0 * r) * sin(r * 22.0)
            val hop = if (!pressDown && r < 0.32) sin((r / 0.32) * PI) else 0.0
            val eye = if (pressDown) 0.0 else if (r < 0.14) r / 0.14 else 1.0
            PebblePressFX(
                sx = 1.0 + 0.08 * d - 0.07 * b,
                sy = 1.0 - 0.10 * d + 0.09 * b,
                dy = -5.0 * hop,
                eye = eye,
            )
        }
    } else null

    val tapModifier = if (interactive) {
        Modifier.pointerInput(Unit) {
            detectTapGestures(
                onPress = {
                    pressDown = true
                    pressT0 = elapsedSec
                    pressActive = true
                    tryAwaitRelease()
                    pressDown = false
                    pressT1 = elapsedSec
                },
            )
        }
    } else {
        Modifier
    }

    Box(
        modifier = modifier
            .size(size)
            .then(tapModifier),
    ) {
        Canvas(modifier = Modifier.matchParentSize()) {
            drawPebble(
                params = params,
                color = blobColor,
                eye = p.eye,
                t = elapsedSec,
                busy = busy,
                fx = fx,
            )
        }
        if (busy) {
            val dotSize = max(9f, (size.value * 0.24f)).dp
            val inf = rememberInfiniteTransition(label = "busyDot")
            val dotAlpha by inf.animateFloat(
                initialValue = 1f,
                targetValue = 0.55f,
                animationSpec = infiniteRepeatable(
                    animation = tween(600),
                    repeatMode = RepeatMode.Reverse,
                ),
                label = "busyPulse",
            )
            Box(
                modifier = Modifier
                    .align(Alignment.BottomEnd)
                    .size(dotSize)
                    .alpha(if (reduceMotion) 1f else dotAlpha)
                    .background(p.busy, CircleShape)
                    .border(2.dp, p.page, CircleShape),
            )
        }
    }
}
