package ai.pebble.android.data

import ai.pebble.android.i18n.L
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import kotlin.math.roundToInt

enum class ConnState { IDLE, CONNECTING, CONNECTED, UNAUTHORIZED, ERROR }
enum class BotSortMode(val raw: String) { RECENT("recent"), MANUAL("manual") }

fun resolveDefaultModel(g: String, h: String): String {
    if (g.isEmpty()) return ""
    if (g.startsWith("magpie/")) return if (h == "claude") g.removePrefix("magpie/") else g
    if (!g.contains("/")) return g
    return if (h.isEmpty() || h == "opencode" || h == "pi") g else ""
}

fun parseIsoInstant(s: String?): Instant? {
    if (s.isNullOrBlank()) return null
    return runCatching { Instant.parse(s) }.getOrNull()
}

private val hhmmEnFormatter: DateTimeFormatter = DateTimeFormatter.ofPattern("h:mm a", Locale.US)
private val hhmmZhFormatter: DateTimeFormatter = DateTimeFormatter.ofPattern("HH:mm", Locale.CHINA)
private val previewCodeRegex = Regex("```[\\s\\S]*?```")
private val previewMdRegex = Regex("[#>*`_\\[\\]]")
private val previewWsRegex = Regex("\\s+")

private fun formatHhMm(instant: Instant): String {
    val zdt = instant.atZone(ZoneId.systemDefault())
    val fmt = if (L.lang == "en") hhmmEnFormatter else hhmmZhFormatter
    return fmt.format(zdt)
}

fun formatShortTime(iso: String?): String {
    val inst = parseIsoInstant(iso) ?: return ""
    val zone = ZoneId.systemDefault()
    val date = inst.atZone(zone).toLocalDate()
    val today = LocalDate.now(zone)
    if (date == today) return formatHhMm(inst)
    if (date == today.minusDays(1)) return L.t("yesterday")
    return "${date.monthValue}/${date.dayOfMonth}"
}

fun formatStamp(iso: String?): String {
    val inst = parseIsoInstant(iso) ?: return ""
    val zone = ZoneId.systemDefault()
    val date = inst.atZone(zone).toLocalDate()
    val today = LocalDate.now(zone)
    val t = formatHhMm(inst)
    if (date == today) return "${L.t("today")} $t"
    if (date == today.minusDays(1)) return "${L.t("yesterday")} $t"
    return if (L.lang == "en") {
        "${date.monthValue}/${date.dayOfMonth} $t"
    } else {
        "${date.monthValue}月${date.dayOfMonth}日 $t" // i18n:allow-cjk
    }
}

fun formatClockTime(iso: String?): String {
    val inst = parseIsoInstant(iso) ?: return ""
    return formatHhMm(inst)
}

fun plainPreview(s: String): String {
    var out = s.replace(previewCodeRegex, L.t("codeTag"))
    out = out.replace(previewMdRegex, "")
    out = out.replace(previewWsRegex, " ")
    return out.trim()
}

fun countToolCalls(tools: List<String>?): Int {
    if (tools.isNullOrEmpty()) return 0
    var total = 0
    for (item in tools) {
        val idx = item.lastIndexOf('\u00d7')
        val n = if (idx >= 0) item.substring(idx + 1).toIntOrNull() else null
        total += if (n != null && n > 0) n else 1
    }
    return total
}

fun formatDurationMs(ms: Double?): String {
    if (ms == null || ms <= 0.0) return ""
    val sec = ms / 1000.0
    if (sec < 60.0) return String.format(Locale.US, "%.1fs", sec)
    var mins = (sec / 60.0).toInt()
    var rem = (sec % 60.0).roundToInt()
    if (rem == 60) {
        mins += 1
        rem = 0
    }
    return "${mins}m ${rem}s"
}
