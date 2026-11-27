import SwiftUI

@MainActor
func harnessDisplayLabel(_ h: HarnessInfo, hub: HubStore) -> String {
    h.title
}

func orderedGlobalHarnesses(harnesses: [HarnessInfo], defaultHarness: String, models: [String: [String]]) -> [HarnessInfo] {
    var seen: Set<String> = []
    var out: [HarnessInfo] = []
    let anyInstalled = harnesses.contains { $0.installed == true }
    func appendIfNeeded(_ h: HarnessInfo) {
        guard seen.insert(h.id).inserted else { return }
        out.append(h)
    }
    if let def = harnesses.first(where: { $0.id == defaultHarness }), !anyInstalled || def.installed == true {
        appendIfNeeded(def)
    }
    for h in harnesses where !anyInstalled || h.installed == true {
        appendIfNeeded(h)
    }
    for h in harnesses where !anyInstalled || (h.installed == true && !(models[h.id] ?? []).isEmpty) {
        appendIfNeeded(h)
    }
    return out
}

@MainActor
func settingsHarnessList(from hub: HubStore) -> [HarnessInfo] {
    if let h = hub.settings?.harnesses, !h.isEmpty { return h }
    return ["opencode", "codebuddy", "qoder", "codex", "claude", "grok", "trae", "pi", "dsh", "agy"].map { HarnessInfo(id: $0, label: $0) }
}

@MainActor
func installedHarnessList(from hub: HubStore) -> [HarnessInfo] {
    (hub.settings?.harnesses ?? []).filter { $0.installed == true }
}

@MainActor
func harnessLabelForSelection(_ id: String, from hub: HubStore) -> String {
    if id.isEmpty { return hub.t("notInstalled") }
    let h = hub.settings?.harnesses?.first(where: { $0.id == id })
    let title = h?.title ?? id
    return h?.installed == true ? title : "\(title) (\(hub.t("notInstalled")))"
}

@MainActor
func localizedMemoryKind(_ raw: String?, hub: HubStore) -> String {
    let k = (raw ?? "fact").trimmingCharacters(in: .whitespacesAndNewlines)
    switch k {
    case "fact": return hub.t("kindFact")
    case "preference": return hub.t("kindPreference")
    case "rule": return hub.t("kindRule")
    case "decision": return hub.t("kindDecision")
    case "reference": return hub.t("kindReference")
    case "profile": return hub.t("kindProfile")
    case "procedure": return hub.t("kindProcedure")
    case "episode": return hub.t("kindEpisode")
    default: return k.isEmpty ? "memory" : k
    }
}

struct ModelOptionsMenuContent: View {
    @Environment(HubStore.self) private var hub
    var harness: String
    var current: String
    var isLoading: Bool
    var onRefresh: () -> Void
    var onSelect: (String) -> Void

    var body: some View {
        let cands = hub.models[harness] ?? []
        let trimmedCurrent = current.trimmingCharacters(in: .whitespaces)
        if cands.isEmpty {
            if isLoading {
                Text(hub.t("loading"))
            } else {
                Text(hub.t("noModels"))
                Button {
                    onRefresh()
                } label: {
                    Label(hub.t("refresh"), systemImage: "arrow.clockwise")
                }
            }
        } else {
            ForEach(cands, id: \.self) { m in
                Button {
                    onSelect(m)
                } label: {
                    if m == trimmedCurrent {
                        Label(m, systemImage: "checkmark")
                    } else {
                        Text(m)
                    }
                }
            }
            Divider()
            Button {
                onRefresh()
            } label: {
                Label(hub.t("refresh"), systemImage: "arrow.clockwise")
            }
        }
    }
}

struct ModelFieldRow: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    var title: String
    var harness: String
    @Binding var text: String
    var placeholder: String
    var onError: ((String) -> Void)? = nil

    @State private var loading = false

    var body: some View {
        HStack(spacing: 6) {
            TextField(title, text: $text, prompt: Text(placeholder))
                .monoInput()
                .labelsHidden()
                .frame(maxWidth: .infinity, alignment: .leading)
            if !text.trimmingCharacters(in: .whitespaces).isEmpty {
                Button {
                    text = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(p.mute)
                }
                .buttonStyle(.plain)
            }
            Menu {
                if !text.trimmingCharacters(in: .whitespaces).isEmpty {
                    Button(hub.t("clear")) { text = "" }
                    Divider()
                }
                ModelOptionsMenuContent(
                    harness: harness,
                    current: text,
                    isLoading: loading,
                    onRefresh: { Task { await load(force: true) } },
                    onSelect: { text = $0 }
                )
            } label: {
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: AppFont.caption, weight: .medium))
                    .foregroundStyle(p.text2)
                    .frame(width: 24, height: 24)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .hubBorderlessMenu()
            .menuIndicator(.hidden)
            .fixedSize()
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
        .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .stroke(p.s2, lineWidth: 1)
        )
        .task(id: "\(harness)|\(hub.isConnected)|\(hub.modelsGeneration)") {
            guard hub.isConnected, !harness.isEmpty, (hub.models[harness] ?? []).isEmpty else { return }
            await load(force: false)
        }
    }

    private func load(force: Bool) async {
        guard !harness.isEmpty else { return }
        if !force, !(hub.models[harness] ?? []).isEmpty { return }
        loading = true
        defer { loading = false }
        do {
            _ = try await hub.loadModels(harness, force: force)
        } catch {
            if force {
                onError?(errText(error, hub.t("loadModelsFailed")))
            }
        }
    }
}

struct GlobalModelFieldRow: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    var title: String
    var defaultHarness: String
    var harnesses: [HarnessInfo]
    @Binding var text: String
    var placeholder: String
    var onError: ((String) -> Void)? = nil

    @State private var loadingAll = false

    var body: some View {
        let targets = orderedGlobalHarnesses(harnesses: harnesses, defaultHarness: defaultHarness, models: hub.models)
        let trimmedCurrent = text.trimmingCharacters(in: .whitespaces)
        HStack(spacing: 6) {
            TextField(title, text: $text, prompt: Text(placeholder))
                .monoInput()
                .labelsHidden()
                .frame(maxWidth: .infinity, alignment: .leading)
            if !trimmedCurrent.isEmpty {
                Button {
                    text = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(p.mute)
                }
                .buttonStyle(.plain)
            }
            Menu {
                if !trimmedCurrent.isEmpty {
                    Button(hub.t("clear")) { text = "" }
                    Divider()
                }
                if targets.count == 1, let only = targets.first {
                    ModelOptionsMenuContent(
                        harness: only.id,
                        current: text,
                        isLoading: loadingAll,
                        onRefresh: { Task { await refreshAll() } },
                        onSelect: { text = $0 }
                    )
                } else {
                    ForEach(targets) { h in
                        let baseLabel = harnessDisplayLabel(h, hub: hub)
                        let label = h.id == defaultHarness ? "\(baseLabel) (\(hub.t("defaultTag")))" : baseLabel
                        Menu(label) {
                            ModelOptionsMenuContent(
                                harness: h.id,
                                current: text,
                                isLoading: loadingAll,
                                onRefresh: { Task { await refreshAll() } },
                                onSelect: { text = $0 }
                            )
                        }
                    }
                    Divider()
                    Button {
                        Task { await refreshAll() }
                    } label: {
                        Label(hub.t("refresh"), systemImage: "arrow.clockwise")
                    }
                }
            } label: {
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: AppFont.caption, weight: .medium))
                    .foregroundStyle(p.text2)
                    .frame(width: 24, height: 24)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .hubBorderlessMenu()
            .menuIndicator(.hidden)
            .fixedSize()
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
        .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .stroke(p.s2, lineWidth: 1)
        )
    }

    private func refreshAll() async {
        loadingAll = true
        defer { loadingAll = false }
        let installed = harnesses.filter { $0.installed == true }
        for h in installed {
            do {
                _ = try await hub.loadModels(h.id, force: true)
            } catch {
                onError?(errText(error, hub.t("loadModelsFailed")))
            }
        }
    }
}
