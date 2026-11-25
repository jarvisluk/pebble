import SwiftUI

struct SettingsMemoryTab: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p

    @Binding var error: String
    @Binding var saved: String
    @Binding var confirm: ConfirmItem?

    @State private var userMd = ""
    @State private var userMdTouched = false
    @State private var userMdSaving = false

    @State private var mem: MemoryList?
    @State private var memScope = "global"
    @State private var memFilterKind = ""
    @State private var memQuery = ""
    @State private var memAdd = false
    @State private var memKind = "fact"
    @State private var memText = ""
    @State private var memTags = ""
    @State private var busy = ""

    private static let memKindKeys = ["fact", "preference", "rule", "reference", "profile", "procedure", "episode"]

    private var userMdDirty: Bool {
        guard let s = hub.settings else { return false }
        func rtrim(_ x: String) -> String {
            x.replacingOccurrences(of: "\\s+$", with: "", options: .regularExpression)
        }
        return userMdTouched && rtrim(userMd) != rtrim(s.sharedUserMd ?? "")
    }

    var body: some View {
        sharedUserSection
        magpieMemorySection
            .task(id: hub.isConnected) {
                guard hub.isConnected else { return }
                _ = try? await hub.loadSettings()
                syncUserMd()
                await loadMem(memQuery)
            }
            .onAppear {
                syncUserMd()
                registerHandlers()
            }
            .onDisappear {
                router.settingsDirty = false
                router.settingsSaveAction = nil
                router.settingsDiscardAction = nil
            }
            .onChange(of: hub.settings?.sharedUserMd) { _, _ in
                syncUserMd()
            }
            .onChange(of: userMdDirty) { _, dirty in
                router.settingsDirty = dirty
            }
    }

    private var sharedUserSection: some View {
        Section {
            TextField(
                hub.t("globalSharedMem"),
                text: Binding(
                    get: { userMd },
                    set: {
                        guard $0 != userMd else { return }
                        userMd = $0
                        userMdTouched = true
                        saved = ""
                    }
                ),
                prompt: Text(hub.t("userMdPh")),
                axis: .vertical
            )
            .lineLimit(4...20)
            .labelsHidden()

            HStack {
                Text("_shared/USER.md")
                    .font(.system(size: AppFont.secondary, design: .monospaced))
                    .foregroundStyle(.secondary)
                Spacer()
                if userMdSaving {
                    ProgressView().controlSize(.small)
                } else {
                    Button(hub.t("save")) {
                        Task { _ = await saveSharedUserMd() }
                    }
                    .buttonStyle(HubPrimaryButtonStyle())
                    .disabled(!userMdDirty || !busy.isEmpty)
                }
            }
        } header: {
            Text(hub.t("globalSharedMem"))
        }
    }

    private var magpieMemorySection: some View {
        let kindFilterOptions: [(String, String)] = [("", hub.t("memKindAll"))] + Self.memKindKeys.map { ($0, localizedMemoryKind($0, hub: hub)) }
        let kindAddOptions: [(String, String)] = Self.memKindKeys.map { ($0, localizedMemoryKind($0, hub: hub)) }

        return Section {
            if mem?.installed == true, let sem = mem?.semantic {
                HStack(spacing: 8) {
                    Tag(
                        text: hub.t(sem.active == true ? "memSemanticOn" : "memSemanticOff"),
                        bg: sem.active == true ? p.sage : p.s1,
                        fg: sem.active == true ? p.sageInk : .secondary
                    )
                    Spacer()
                    if sem.active != true {
                        Button(hub.t("openMagpie")) {
                            run("magpie:open", "opFailed") { try await hub.openMagpie(); return nil }
                        }
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                    }
                }
                if sem.active != true {
                    Text(hub.t("memSemanticHintOff"))
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(.secondary)
                }
            }

            HubSegmentedPicker(
                selection: Binding(
                    get: { memScope },
                    set: { next in
                        memScope = next
                        Task { await loadMem(memQuery, scope: next) }
                    }
                ),
                options: [
                    ("global", hub.t("memScopeGlobal")),
                    ("all", hub.t("memScopeAll")),
                ]
            )

            HubMenuPicker(
                title: hub.t("memType"),
                selection: Binding(
                    get: { memFilterKind },
                    set: { next in
                        memFilterKind = next
                        Task { await loadMem(memQuery, kind: next) }
                    }
                ),
                options: kindFilterOptions
            )

            HStack {
                TextField(
                    hub.t("search"),
                    text: $memQuery,
                    prompt: Text(hub.t(mem?.semantic?.active == true ? "searchSemanticMemPh" : "searchGlobalMemPh"))
                )
                .labelsHidden()
                .onSubmit { Task { await loadMem(memQuery) } }
                if !memQuery.isEmpty {
                    Button {
                        memQuery = ""
                        Task { await loadMem("") }
                    } label: {
                        Image(systemName: "xmark.circle.fill").foregroundStyle(.secondary)
                    }
                    .buttonStyle(.borderless)
                }
                Button { Task { await loadMem(memQuery) } } label: {
                    Label(hub.t("search"), systemImage: "magnifyingglass").labelStyle(.iconOnly)
                }
                .buttonStyle(.borderless)
            }

            if mem?.installed != true {
                Text(hub.t("hostNoMagpieGlobal")).foregroundStyle(.secondary)
                Button {
                    run("magpie:install", "opFailed") {
                        try await hub.installMagpie()
                        await loadMem(memQuery)
                        return hub.t("mpInstallDone")
                    }
                } label: {
                    if busy == "magpie:install" {
                        HStack(spacing: 8) {
                            ProgressView().controlSize(.small)
                            Text(hub.t("mpInstalling"))
                        }
                    } else {
                        Text(hub.t("mpInstallOnHost"))
                    }
                }
                .disabled(!busy.isEmpty)
            } else if mem?.available != true {
                Text(hub.t("magpieNoResp") + ((mem?.error ?? "").isEmpty ? "" : ": \(mem!.error!)"))
                    .foregroundStyle(p.danger)
            } else if (mem?.items ?? []).isEmpty {
                Text(hub.t("noGlobalMem")).foregroundStyle(.secondary)
            } else {
                ForEach(mem?.items ?? []) { it in
                    MemoryRow(item: it, showTags: true, showScope: memScope == "all") {
                        confirm = ConfirmItem(title: hub.t("deleteMemTitle"), message: hub.t("deleteMemConfirm")) {
                            do {
                                try await hub.deleteMemory(it.id)
                                await loadMem(memQuery)
                            } catch {
                                self.error = errText(error, hub.t("delFailed"))
                            }
                        }
                    }
                }
            }

            DisclosureGroup(hub.t("addShort"), isExpanded: $memAdd) {
                HubMenuPicker(title: hub.t("memType"), selection: $memKind, options: kindAddOptions)
                TextField(hub.t("magpieGlobalMem"), text: $memText, prompt: Text(hub.t("globalMemPh")), axis: .vertical)
                    .lineLimit(2...8)
                TextField(hub.t("tagsPh"), text: $memTags)
                Button(hub.t("saveMemory")) {
                    let txt = memText.trimmingCharacters(in: .whitespacesAndNewlines)
                    guard !txt.isEmpty else {
                        error = hub.t("memContentReq")
                        return
                    }
                    let tags = memTags
                        .components(separatedBy: CharacterSet(charactersIn: ",，"))
                        .map { $0.trimmingCharacters(in: .whitespaces) }
                        .filter { !$0.isEmpty }
                    run("mem:save", "saveMemFailed") {
                        try await hub.saveMemory(["scope": "global", "kind": memKind, "text": txt, "tags": tags])
                        memText = ""
                        memTags = ""
                        memAdd = false
                        await loadMem(memQuery)
                        return hub.t("globalMemSaved")
                    }
                }
                .buttonStyle(HubPrimaryButtonStyle())
                .disabled(busy == "mem:save")
            }
        } header: {
            Text(hub.t("magpieGlobalMem"))
        }
    }

    private func syncUserMd() {
        guard !userMdTouched, let s = hub.settings else { return }
        userMd = s.sharedUserMd ?? ""
    }

    private func registerHandlers() {
        router.settingsDirty = userMdDirty
        router.settingsSaveAction = { await saveSharedUserMd() }
        router.settingsDiscardAction = {
            userMdTouched = false
            userMd = hub.settings?.sharedUserMd ?? ""
        }
    }

    private func saveSharedUserMd() async -> Bool {
        error = ""
        saved = ""
        userMdSaving = true
        defer { userMdSaving = false }
        do {
            try await hub.saveSharedUserMd(userMd)
            userMdTouched = false
            userMd = hub.settings?.sharedUserMd ?? ""
            router.settingsDirty = false
            saved = hub.t("settingsSaved")
            return true
        } catch {
            self.error = errText(error, hub.t("saveFailed"))
            return false
        }
    }

    private func loadMem(_ q: String, scope: String? = nil, kind: String? = nil) async {
        let sc = scope ?? memScope
        let kd = kind ?? memFilterKind
        do {
            mem = try await hub.fetchMemories(scope: sc, query: q, kind: kd, limit: 30)
        } catch {
            self.error = errText(error, hub.t("opFailed"))
        }
    }

    private func run(_ key: String, _ fail: String, _ op: @escaping () async throws -> String?) {
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
