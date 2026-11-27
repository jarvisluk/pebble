package ai.pebble.android.i18n

import java.util.Locale

object L {
    @Volatile
    var lang: String = systemLang

    val systemLang: String
        get() = if (Locale.getDefault().language.lowercase().startsWith("zh")) "zh-CN" else "en"

    fun t(key: String, vars: Map<String, Any> = emptyMap()): String {
        var s = (if (lang == "en") enStrings[key] else zhStrings[key]) ?: zhStrings[key] ?: key
        for ((k, v) in vars) {
            s = s.replace("{$k}", v.toString())
        }
        return s
    }
}
