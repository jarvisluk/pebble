package ai.pebble.android.ui.theme

import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3ExpressiveApi
import androidx.compose.material3.MaterialExpressiveTheme
import androidx.compose.material3.MotionScheme
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlin.math.roundToInt

const val PEBBLE_COLOR_HEX = "#7aaa98"
val BotColors = listOf("#d9774b", "#e0a23a", "#7fa14a", PEBBLE_COLOR_HEX, "#5b8bb5", "#9a7bc0", "#c4605e")
val BotAvatars = listOf("blob", "pebble-1", "pebble-2", "pebble-3", "pebble-4", "pebble-5", "pebble-6", "pebble-7")

private val legacyBotAvatarMap = mapOf(
    "squircle" to "pebble-1",
    "cat" to "pebble-2",
    "bear" to "pebble-3",
    "cloud" to "pebble-4",
    "ghost" to "pebble-5",
    "star" to "pebble-6",
    "sprout" to "pebble-7",
)

private val pebbleVariantSeeds = mapOf(
    "pebble-1" to "pebble-v6",
    "pebble-2" to "pebble-v1",
    "pebble-3" to "pebble-v30",
    "pebble-4" to "pebble-v11",
    "pebble-5" to "pebble-v21",
    "pebble-6" to "pebble-v17",
    "pebble-7" to "pebble-v15",
)

fun fnv1a(s: String): UInt {
    var h = 2166136261u
    for (ch in s) {
        h = (h xor ch.code.toUInt()) * 16777619u
    }
    return h
}

fun normalizeBotAvatar(raw: String?): String {
    val s = raw.orEmpty().trim().lowercase()
    if (s.isEmpty() || s == "blob") return "blob"
    legacyBotAvatarMap[s]?.let { return it }
    return if (s in BotAvatars) s else "blob"
}

fun resolvePebbleSeed(id: String, avatar: String?): String {
    val kind = normalizeBotAvatar(avatar)
    if (kind != "blob") {
        pebbleVariantSeeds[kind]?.let { return it }
    }
    return id.ifEmpty { "?" }
}

fun normalizeBotHex(raw: String?): String? {
    val clean = raw.orEmpty().trim().lowercase()
    if (clean.length != 7 || !clean.startsWith("#")) return null
    val hexPart = clean.substring(1)
    if (!hexPart.all { it in '0'..'9' || it in 'a'..'f' }) return null
    return clean
}

fun botColorHex(id: String, index: Int = -1, custom: String? = null): String {
    normalizeBotHex(custom)?.let { return it }
    val h = fnv1a(id.ifEmpty { "?" })
    val idx = if (index >= 0) index % BotColors.size else (h % BotColors.size.toUInt()).toInt()
    return BotColors[idx]
}

private fun parseRgb(hex: String): Triple<Double, Double, Double> {
    var s = hex.trim().removePrefix("#")
    if (s.length == 3) s = s.map { "$it$it" }.joinToString("")
    val v = s.toLongOrNull(16) ?: 0L
    return Triple(
        ((v shr 16) and 0xff).toDouble(),
        ((v shr 8) and 0xff).toDouble(),
        (v and 0xff).toDouble(),
    )
}

fun colorFromHex(hex: String): Color {
    val (r, g, b) = parseRgb(hex)
    return Color(red = (r / 255.0).toFloat(), green = (g / 255.0).toFloat(), blue = (b / 255.0).toFloat(), alpha = 1f)
}

fun mixHex(a: String, b: String, wa: Double): String {
    val (ar, ag, ab) = parseRgb(a)
    val (br, bg, bb) = parseRgb(b)
    val w = wa.coerceIn(0.0, 1.0)
    fun ch(p: Double, q: Double): String = "%02x".format((p * w + q * (1.0 - w)).roundToInt())
    return "#${ch(ar, br)}${ch(ag, bg)}${ch(ab, bb)}"
}

fun contrastText(hex: String): Color {
    val (r, g, b) = parseRgb(hex)
    val lum = (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255.0
    return colorFromHex(if (lum > 0.62) "#1d1b19" else "#ffffff")
}

@Immutable
data class BotAccent(
    val accent: Color,
    val onAccent: Color,
    val soft: Color,
    val collabBg: Color,
    val collabInk: Color,
)

@Immutable
data class PebblePalette(
    val dark: Boolean,
    val page: Color,
    val card: Color,
    val s1: Color,
    val s2: Color,
    val s3: Color,
    val text: Color,
    val text2: Color,
    val mute: Color,
    val accent: Color,
    val onAccent: Color,
    val accentSoft: Color,
    val sel: Color,
    val pink: Color,
    val pinkInk: Color,
    val lilac: Color,
    val lilacInk: Color,
    val sage: Color,
    val sageInk: Color,
    val busy: Color,
    val danger: Color,
    val eye: Color,
    val stage: Color,
    val stagePill: Color,
    val stageText: Color,
    val stageMute: Color,
) {
    fun bot(id: String, index: Int, custom: String? = null): BotAccent {
        val hex = botColorHex(id, index, custom)
        return BotAccent(
            accent = colorFromHex(hex),
            onAccent = contrastText(hex),
            soft = colorFromHex(if (dark) "#2a2a2a" else mixHex(hex, "#ffffff", 0.16)),
            collabBg = colorFromHex(if (dark) mixHex(hex, "#171717", 0.28) else mixHex(hex, "#ffffff", 0.16)),
            collabInk = colorFromHex(if (dark) mixHex(hex, "#ffffff", 0.72) else mixHex(hex, "#1d1b19", 0.62)),
        )
    }

    companion object {
        fun of(dark: Boolean): PebblePalette = if (dark) {
            PebblePalette(
                dark = true,
                page = colorFromHex("#000000"),
                card = colorFromHex("#171717"),
                s1 = colorFromHex("#232323"),
                s2 = colorFromHex("#303030"),
                s3 = colorFromHex("#404040"),
                text = colorFromHex("#f3f1ee"),
                text2 = colorFromHex("#ccc7c1"),
                mute = colorFromHex("#948e88"),
                accent = colorFromHex("#f3f1ee"),
                onAccent = colorFromHex("#171717"),
                accentSoft = colorFromHex("#2a2a2a"),
                sel = colorFromHex("#262626"),
                pink = colorFromHex("#2c1616"),
                pinkInk = colorFromHex("#ff6961"),
                lilac = colorFromHex("#24203a"),
                lilacInk = colorFromHex("#b7aaf5"),
                sage = colorFromHex("#14261c"),
                sageInk = colorFromHex("#30d158"),
                busy = colorFromHex("#ff9f0a"),
                danger = colorFromHex("#ff453a"),
                eye = colorFromHex("#1c1917"),
                stage = colorFromHex("#050505"),
                stagePill = colorFromHex("#232323"),
                stageText = colorFromHex("#f3f1ee"),
                stageMute = colorFromHex("#9b958f"),
            )
        } else {
            PebblePalette(
                dark = false,
                page = colorFromHex("#efece8"),
                card = colorFromHex("#ffffff"),
                s1 = colorFromHex("#f5f3f0"),
                s2 = colorFromHex("#e9e5e0"),
                s3 = colorFromHex("#dcd7d0"),
                text = colorFromHex("#1d1b19"),
                text2 = colorFromHex("#57524d"),
                mute = colorFromHex("#9b958f"),
                accent = colorFromHex("#1d1b19"),
                onAccent = colorFromHex("#ffffff"),
                accentSoft = colorFromHex("#e9e5e0"),
                sel = colorFromHex("#eeebe6"),
                pink = colorFromHex("#fde8e8"),
                pinkInk = colorFromHex("#dc2626"),
                lilac = colorFromHex("#e0dbfb"),
                lilacInk = colorFromHex("#6a55c9"),
                sage = colorFromHex("#e6f6ec"),
                sageInk = colorFromHex("#16a34a"),
                busy = colorFromHex("#f59e0b"),
                danger = colorFromHex("#dc2626"),
                eye = colorFromHex("#1c1917"),
                stage = colorFromHex("#111110"),
                stagePill = colorFromHex("#1f1d1b"),
                stageText = colorFromHex("#f3f1ee"),
                stageMute = colorFromHex("#a6a09a"),
            )
        }
    }
}

val LocalPalette = staticCompositionLocalOf { PebblePalette.of(false) }

object PebbleDimens {
    val gap = 8.dp
    val pad = 12.dp
    val barHeight = 48.dp
    val cardRadius = 24.dp
    val rowRadius = 12.dp
    val bubbleRadius = 20.dp
    val composerRadius = 26.dp
    val cardShape = RoundedCornerShape(cardRadius)
    val rowShape = RoundedCornerShape(rowRadius)
    val bubbleShape = RoundedCornerShape(bubbleRadius)
    val composerShape = RoundedCornerShape(composerRadius)
}

object PebbleFont {
    val caption = 12.sp
    val secondary = 13.sp
    val body = 15.sp
    val cardTitle = 16.sp
    val title = 17.sp
    val hero = 20.sp
}

private val PebbleTypography = Typography(
    titleLarge = TextStyle(fontSize = PebbleFont.hero, fontWeight = FontWeight.Bold),
    titleMedium = TextStyle(fontSize = PebbleFont.title, fontWeight = FontWeight.Bold),
    titleSmall = TextStyle(fontSize = PebbleFont.cardTitle, fontWeight = FontWeight.SemiBold),
    bodyLarge = TextStyle(fontSize = PebbleFont.body, fontWeight = FontWeight.Normal, lineHeight = 22.sp),
    bodyMedium = TextStyle(fontSize = PebbleFont.secondary, fontWeight = FontWeight.Normal, lineHeight = 19.sp),
    bodySmall = TextStyle(fontSize = PebbleFont.caption, fontWeight = FontWeight.Normal, lineHeight = 16.sp),
    labelLarge = TextStyle(fontSize = PebbleFont.body, fontWeight = FontWeight.SemiBold),
    labelMedium = TextStyle(fontSize = PebbleFont.secondary, fontWeight = FontWeight.SemiBold),
    labelSmall = TextStyle(fontSize = PebbleFont.caption, fontWeight = FontWeight.Normal),
)

val MonoFont = FontFamily.Monospace

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun PebbleTheme(
    dark: Boolean,
    content: @Composable () -> Unit,
) {
    val pal = PebblePalette.of(dark)
    val colorScheme = if (dark) {
        darkColorScheme(
            primary = pal.accent,
            onPrimary = pal.onAccent,
            primaryContainer = pal.accentSoft,
            onPrimaryContainer = pal.text,
            secondary = pal.text2,
            onSecondary = pal.card,
            secondaryContainer = pal.s1,
            onSecondaryContainer = pal.text,
            background = pal.page,
            onBackground = pal.text,
            surface = pal.card,
            onSurface = pal.text,
            surfaceVariant = pal.s1,
            onSurfaceVariant = pal.text2,
            surfaceContainerLowest = pal.page,
            surfaceContainerLow = pal.card,
            surfaceContainer = pal.card,
            surfaceContainerHigh = pal.s1,
            surfaceContainerHighest = pal.s2,
            outline = pal.s2,
            outlineVariant = pal.s2,
            error = pal.danger,
            onError = Color.White,
            errorContainer = pal.pink,
            onErrorContainer = pal.pinkInk,
        )
    } else {
        lightColorScheme(
            primary = pal.accent,
            onPrimary = pal.onAccent,
            primaryContainer = pal.accentSoft,
            onPrimaryContainer = pal.text,
            secondary = pal.text2,
            onSecondary = pal.card,
            secondaryContainer = pal.s1,
            onSecondaryContainer = pal.text,
            background = pal.page,
            onBackground = pal.text,
            surface = pal.card,
            onSurface = pal.text,
            surfaceVariant = pal.s1,
            onSurfaceVariant = pal.text2,
            surfaceContainerLowest = pal.card,
            surfaceContainerLow = pal.card,
            surfaceContainer = pal.card,
            surfaceContainerHigh = pal.s1,
            surfaceContainerHighest = pal.s2,
            outline = pal.s2,
            outlineVariant = pal.s2,
            error = pal.danger,
            onError = Color.White,
            errorContainer = pal.pink,
            onErrorContainer = pal.pinkInk,
        )
    }

    CompositionLocalProvider(LocalPalette provides pal) {
        MaterialExpressiveTheme(
            colorScheme = colorScheme,
            motionScheme = MotionScheme.expressive(),
            typography = PebbleTypography,
            content = content,
        )
    }
}
