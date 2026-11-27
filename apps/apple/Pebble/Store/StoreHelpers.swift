import Foundation

enum L {
    /// "zh-CN" 或 "en"，由 HubStore 根据偏好同步。
    static var lang = L.systemLang

    static var systemLang: String {
        (Bundle.main.preferredLocalizations.first ?? "en").lowercased().hasPrefix("zh") ? "zh-CN" : "en"
    }

    static func t(_ key: String, _ vars: [String: Any] = [:]) -> String {
        var s = (lang == "en" ? enStrings[key] : zhStrings[key]) ?? zhStrings[key] ?? key
        for (k, v) in vars { s = s.replacingOccurrences(of: "{\(k)}", with: "\(v)") }
        return s
    }
}

enum ConnState { case idle, connecting, connected, unauthorized, error }

func resolveDefaultModel(_ g: String, _ h: String) -> String {
    if g.isEmpty { return "" }
    if !g.contains("/") { return g }
    return h.isEmpty || h == "opencode" || h == "pi" ? g : ""
}

private let isoLock = NSLock()
private let isoFracFormatter: ISO8601DateFormatter = {
    let f = ISO8601DateFormatter()
    f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return f
}()
private let isoPlainFormatter: ISO8601DateFormatter = {
    let f = ISO8601DateFormatter()
    f.formatOptions = [.withInternetDateTime]
    return f
}()
private let hhmmEnFormatter: DateFormatter = {
    let f = DateFormatter()
    f.locale = Locale(identifier: "en_US")
    f.dateFormat = "h:mm a"
    return f
}()
private let hhmmZhFormatter: DateFormatter = {
    let f = DateFormatter()
    f.locale = Locale(identifier: "zh_CN")
    f.dateFormat = "HH:mm"
    return f
}()

func parseISO(_ s: String?) -> Date? {
    guard let s, !s.isEmpty else { return nil }
    isoLock.lock()
    defer { isoLock.unlock() }
    if let d = isoFracFormatter.date(from: s) { return d }
    return isoPlainFormatter.date(from: s)
}

private func hhmm(_ d: Date) -> String {
    isoLock.lock()
    defer { isoLock.unlock() }
    let f = L.lang == "en" ? hhmmEnFormatter : hhmmZhFormatter
    return f.string(from: d)
}

func formatShortTime(_ iso: String?) -> String {
    guard let d = parseISO(iso) else { return "" }
    let cal = Calendar.current
    if cal.isDateInToday(d) { return hhmm(d) }
    if cal.isDateInYesterday(d) { return L.t("yesterday") }
    let c = cal.dateComponents([.month, .day], from: d)
    return "\(c.month ?? 0)/\(c.day ?? 0)"
}

func formatStamp(_ iso: String?) -> String {
    guard let d = parseISO(iso) else { return "" }
    let cal = Calendar.current
    let t = hhmm(d)
    if cal.isDateInToday(d) { return "\(L.t("today")) \(t)" }
    if cal.isDateInYesterday(d) { return "\(L.t("yesterday")) \(t)" }
    let c = cal.dateComponents([.month, .day], from: d)
    return L.lang == "en" ? "\(c.month ?? 0)/\(c.day ?? 0) \(t)" : "\(c.month ?? 0)月\(c.day ?? 0)日 \(t)" // i18n:allow-cjk
}

private let previewCodeRegex = try? NSRegularExpression(pattern: "```[\\s\\S]*?```")
private let previewMdRegex = try? NSRegularExpression(pattern: "[#>*`_\\[\\]]")
private let previewWsRegex = try? NSRegularExpression(pattern: "\\s+")

func plainPreview(_ s: String) -> String {
    var out = s
    if let re = previewCodeRegex {
        out = re.stringByReplacingMatches(in: out, range: NSRange(out.startIndex..., in: out), withTemplate: NSRegularExpression.escapedTemplate(for: L.t("codeTag")))
    }
    if let re = previewMdRegex {
        out = re.stringByReplacingMatches(in: out, range: NSRange(out.startIndex..., in: out), withTemplate: "")
    }
    if let re = previewWsRegex {
        out = re.stringByReplacingMatches(in: out, range: NSRange(out.startIndex..., in: out), withTemplate: " ")
    }
    return out.trimmingCharacters(in: .whitespaces)
}

func formatClockTime(_ iso: String?) -> String {
    guard let d = parseISO(iso) else { return "" }
    return hhmm(d)
}

func countToolCalls(_ tools: [String]?) -> Int {
    guard let tools, !tools.isEmpty else { return 0 }
    var total = 0
    for item in tools {
        if let idx = item.lastIndex(of: "\u{00d7}"), let n = Int(item[item.index(after: idx)...]), n > 0 {
            total += n
        } else {
            total += 1
        }
    }
    return total
}

func formatDurationMs(_ ms: Double?) -> String {
    guard let ms, ms > 0 else { return "" }
    let sec = ms / 1000.0
    if sec < 60 { return String(format: "%.1fs", sec) }
    var mins = Int(sec / 60.0)
    var rem = Int((sec.truncatingRemainder(dividingBy: 60.0)).rounded())
    if rem == 60 {
        mins += 1
        rem = 0
    }
    return "\(mins)m \(rem)s"
}
