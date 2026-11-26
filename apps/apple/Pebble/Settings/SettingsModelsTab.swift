import SwiftUI

struct SettingsModelsTab: View {
    @Environment(HubStore.self) var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) var p
    @Environment(\.openURL) var openURL

    @Binding var error: String
    @Binding var saved: String
    @Binding var confirm: ConfirmItem?

    struct BotDraft: Equatable {
        var harness: String
        var model: String
    }

    @State private var defaultHarness = "opencode"
    @State private var defaultHarnessTouched = false
    @State private var defaultPermMode = "auto"
    @State private var defaultPermModeTouched = false
    @State private var defaultModel = ""
    @State private var defaultTouched = false
    @State private var perHarness: [String: String] = [:]
    @State private var perTouched = false
    @State private var drafts: [String: BotDraft] = [:]
    @State private var touchedBots: Set<String> = []

    @State var moreEnginesOpen = false
    @State private var perOverridesOpen = false
    @State var expandedInstall: Set<String> = []
    @State var installTasks: [String: SetupTaskStatus] = [:]
    @State var loginTasks: [String: SetupTaskStatus] = [:]
    @State var pollTasks: [String: Task<Void, Never>] = [:]
    @State var copiedKey = ""
    @State var busy = ""

    var harnesses: [HarnessInfo] { settingsHarnessList(from: hub) }
    private var primaryHarness: String { harnesses.first?.id ?? "opencode" }

    private var baseDefaultHarness: String {
        if let dh = hub.settings?.defaultHarness, !dh.isEmpty { return dh }
        return primaryHarness
    }

    private var baseDefaultPermMode: String {
        let m = (hub.settings?.defaultPermissionMode ?? "").trimmingCharacters(in: .whitespaces)
        return m.isEmpty ? "auto" : m
    }

    private func clean(_ m: [String: String]?) -> [String: String] {
        (m ?? [:]).compactMapValues {
            let t = $0.trimmingCharacters(in: .whitespaces)
            return t.isEmpty ? nil : t
        }
    }

    private func origModel(_ b: BotStatus) -> String {
        (b.modelInherited == true ? "" : (b.model ?? "")).trimmingCharacters(in: .whitespaces)
    }

    private var botSnapshotKeys: [String] {
        hub.bots.map { "\($0.id)|\($0.harness ?? "")|\($0.model ?? "")|\($0.modelInherited ?? false)" }
    }

    private var botUpdates: [(id: String, harness: String, model: String)] {
        hub.bots.compactMap { b in
            guard touchedBots.contains(b.id), let d = drafts[b.id] else { return nil }
            let m = d.model.trimmingCharacters(in: .whitespaces)
            return d.harness != (b.harness ?? "") || m != origModel(b) ? (b.id, d.harness, m) : nil
        }
    }

    private var settingsChanged: Bool {
        guard let s = hub.settings else { return false }
        return (defaultHarnessTouched && defaultHarness != baseDefaultHarness)
            || (defaultPermModeTouched && defaultPermMode != baseDefaultPermMode)
            || (defaultTouched && defaultModel.trimmingCharacters(in: .whitespaces) != (s.defaultModel ?? "").trimmingCharacters(in: .whitespaces))
            || (perTouched && clean(perHarness) != clean(s.defaultModels))
    }

    private var dirty: Bool { settingsChanged || !botUpdates.isEmpty }

    var installedHarnesses: [HarnessInfo] {
        harnesses.filter { $0.installed == true }
    }

    var uninstalledHarnesses: [HarnessInfo] {
        harnesses.filter { $0.installed != true }
    }

    var body: some View {
        defaultsAndBotsSection
        enginesSection
            .task(id: hub.isConnected) {
                guard hub.isConnected else { return }
                _ = try? await hub.loadSettings()
                try? await hub.refreshBots()
                sync()
            }
            .task(id: "\(hub.isConnected)|\(hub.modelsGeneration)|\(harnesses.map(\.id).joined(separator: ","))") {
                guard hub.isConnected else { return }
                await preloadAllModels(force: false)
            }
            .onAppear {
                sync()
                registerHandlers()
            }
            .onDisappear {
                for (_, t) in pollTasks { t.cancel() }
                pollTasks.removeAll()
                router.settingsDirty = false
                router.settingsSaveAction = nil
                router.settingsDiscardAction = nil
            }
            .onChange(of: dirty) { _, v in router.settingsDirty = v }
            .onChange(of: hub.settings?.defaultHarness) { _, _ in sync() }
            .onChange(of: hub.settings?.defaultPermissionMode) { _, _ in sync() }
            .onChange(of: hub.settings?.defaultModel) { _, _ in sync() }
            .onChange(of: hub.settings?.defaultModels) { _, _ in sync() }
            .onChange(of: hub.settings?.harnesses) { _, _ in sync() }
            .onChange(of: botSnapshotKeys) { _, _ in syncDrafts() }
    }

    // MARK: Card 3 - Defaults & Bot Overrides

    private var defaultsAndBotsSection: some View {
        Section(hub.t("globalAndBotModels")) {
            HubMenuPicker(
                title: hub.t("defaultHarness"),
                selection: Binding(
                    get: { defaultHarness },
                    set: { next in
                        guard next != defaultHarness else { return }
                        defaultHarness = next
                        defaultHarnessTouched = true
                        saved = ""
                        Task { _ = try? await hub.loadModels(next) }
                    }
                ),
                options: harnesses.map { ($0.id, $0.title) }
            )

            HubMenuPicker(
                title: hub.t("permDefaultMode"),
                selection: Binding(
                    get: { defaultPermMode },
                    set: {
                        guard $0 != defaultPermMode else { return }
                        defaultPermMode = $0
                        defaultPermModeTouched = true
                        saved = ""
                    }
                ),
                options: ["auto", "edit", "ask", "plan"].map { ($0, hub.t("permMode_\($0)")) }
            )
            Text(hub.t("permDefaultModeDesc"))
                .font(.system(size: AppFont.caption))
                .foregroundStyle(p.mute)

            HubFormField(label: hub.t("globalDefaultModel")) {
                GlobalModelFieldRow(
                    title: hub.t("globalDefaultModel"),
                    defaultHarness: defaultHarness,
                    harnesses: harnesses,
                    text: Binding(
                        get: { defaultModel },
                        set: {
                            guard $0 != defaultModel else { return }
                            defaultModel = $0
                            defaultTouched = true
                            saved = ""
                        }
                    ),
                    placeholder: hub.t("globalDefaultPh"),
                    onError: { error = $0 }
                )
            }

            if !installedHarnesses.isEmpty {
                let overrideCount = installedHarnesses.filter { !(perHarness[$0.id] ?? "").trimmingCharacters(in: .whitespaces).isEmpty }.count
                DisclosureGroup(
                    overrideCount > 0 ? hub.t("perHarnessOverridesCount", ["n": overrideCount]) : hub.t("perHarnessOverrides"),
                    isExpanded: $perOverridesOpen
                ) {
                    ForEach(installedHarnesses) { h in
                        let ph = defaultModel.trimmingCharacters(in: .whitespaces).isEmpty
                            ? hub.t("engineModelPh")
                            : resolveDefaultModel(defaultModel.trimmingCharacters(in: .whitespaces), h.id)
                        HubFormField(label: h.title) {
                            ModelFieldRow(
                                title: h.title,
                                harness: h.id,
                                text: Binding(
                                    get: { perHarness[h.id] ?? "" },
                                    set: {
                                        guard $0 != (perHarness[h.id] ?? "") else { return }
                                        perHarness[h.id] = $0
                                        perTouched = true
                                        saved = ""
                                    }
                                ),
                                placeholder: ph,
                                onError: { error = $0 }
                            )
                        }
                    }
                }
            }

            if hub.bots.isEmpty {
                Text(hub.t("noBotsInSettings")).foregroundStyle(.secondary)
            } else {
                ForEach(Array(hub.bots.enumerated()), id: \.element.id) { i, b in
                    let d = drafts[b.id] ?? BotDraft(harness: b.harness ?? "", model: origModel(b))
                    let per = (perHarness[d.harness] ?? "").trimmingCharacters(in: .whitespaces)
                    let ph = per.isEmpty ? resolveDefaultModel(defaultModel.trimmingCharacters(in: .whitespaces), d.harness) : per
                    VStack(alignment: .leading, spacing: 8) {
                        HStack(spacing: 10) {
                            BotAvatar(id: b.id, size: 28, index: i, avatar: b.avatar, color: b.color)
                            VStack(alignment: .leading, spacing: 0) {
                                Text(b.displayName).font(.system(size: AppFont.body, weight: .semibold))
                                Text("@\(b.id)").font(.system(size: AppFont.caption, design: .monospaced)).foregroundStyle(.secondary)
                            }
                            Spacer()
                            Menu {
                                if !harnesses.contains(where: { $0.id == d.harness }) {
                                    Button(d.harness.isEmpty ? "-" : d.harness) {}
                                }
                                ForEach(harnesses) { h in
                                    Button {
                                        guard h.id != d.harness else { return }
                                        drafts[b.id] = BotDraft(harness: h.id, model: "")
                                        touchedBots.insert(b.id)
                                        saved = ""
                                        Task { _ = try? await hub.loadModels(h.id) }
                                    } label: {
                                        if h.id == d.harness {
                                            Label(h.title, systemImage: "checkmark")
                                        } else {
                                            Text(h.title)
                                        }
                                    }
                                }
                            } label: {
                                let lbl = harnesses.first(where: { $0.id == d.harness })?.title ?? (d.harness.isEmpty ? "-" : d.harness)
                                (Text(lbl).foregroundStyle(p.text2)
                                    + Text(" ")
                                    + Text(Image(systemName: "chevron.up.chevron.down"))
                                        .font(.system(size: AppFont.caption - 1, weight: .medium))
                                        .foregroundStyle(p.mute))
                            }
                            .buttonStyle(.plain)
                            .hubBorderlessMenu()
                            .menuIndicator(.hidden)
                            .fixedSize()
                        }
                        ModelFieldRow(
                            title: hub.t("statModel"),
                            harness: d.harness,
                            text: Binding(
                                get: { d.model },
                                set: {
                                    guard $0 != d.model else { return }
                                    var n = d
                                    n.model = $0
                                    drafts[b.id] = n
                                    touchedBots.insert(b.id)
                                    saved = ""
                                }
                            ),
                            placeholder: ph.isEmpty ? hub.t("inheritDefaultPh") : ph,
                            onError: { error = $0 }
                        )
                    }
                    .padding(.vertical, 2)
                }
            }

            Button { router.sheet = .createBot } label: {
                Label(hub.t("newBot"), systemImage: "plus")
            }
        }
    }

    // MARK: Actions & Sync

    private func sync() {
        guard let s = hub.settings else { return }
        if !defaultHarnessTouched { defaultHarness = baseDefaultHarness }
        if !defaultPermModeTouched { defaultPermMode = baseDefaultPermMode }
        if !defaultTouched { defaultModel = (s.defaultModel ?? "").trimmingCharacters(in: .whitespaces) }
        if !perTouched { perHarness = clean(s.defaultModels) }
        syncDrafts()
    }

    private func syncDrafts() {
        let validIds = Set(hub.bots.map(\.id))
        for id in drafts.keys where !validIds.contains(id) {
            drafts.removeValue(forKey: id)
        }
        for b in hub.bots where !touchedBots.contains(b.id) {
            drafts[b.id] = BotDraft(harness: b.harness ?? "", model: origModel(b))
        }
    }

    private func registerHandlers() {
        router.settingsDirty = dirty
        router.settingsSaveAction = { await saveAll() }
        router.settingsDiscardAction = {
            defaultHarnessTouched = false
            defaultPermModeTouched = false
            defaultTouched = false
            perTouched = false
            touchedBots = []
            drafts = [:]
            sync()
        }
    }

    private func saveAll() async -> Bool {
        error = ""
        saved = ""
        do {
            try await hub.saveModelsSettings(
                changed: settingsChanged,
                defaultHarness: defaultHarness,
                defaultPermissionMode: defaultPermMode,
                defaultModel: defaultModel.trimmingCharacters(in: .whitespaces),
                defaultModels: clean(perHarness),
                botUpdates: botUpdates
            )
            defaultHarnessTouched = false
            defaultPermModeTouched = false
            defaultTouched = false
            perTouched = false
            touchedBots = []
            drafts = [:]
            sync()
            router.settingsDirty = false
            saved = hub.t("settingsSaved")
            return true
        } catch {
            self.error = errText(error, hub.t("saveFailed"))
            return false
        }
    }

    func preloadAllModels(force: Bool) async {
        let ids = harnesses.filter { $0.installed != false }.map(\.id)
        await withTaskGroup(of: Void.self) { group in
            for id in ids {
                group.addTask { @MainActor in
                    _ = try? await hub.loadModels(id, force: force)
                }
            }
        }
    }

    func run(_ key: String, _ fail: String, _ op: @escaping () async throws -> String?) {
        error = ""
        saved = ""
        busy = key
        Task {
            do {
                if let msg = try await op() { saved = msg }
            } catch {
                self.error = errText(error, hub.t(fail))
            }
            busy = ""
        }
    }
}
