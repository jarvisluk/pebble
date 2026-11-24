import SwiftUI

struct CreateBotView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p

    @State private var name = ""
    @State private var botId = ""
    @State private var idTouched = false
    @State private var desc = ""
    @State private var avatar = ""
    @State private var color = ""
    @State private var harness = "opencode"
    @State private var harnessTouched = false
    @State private var model = ""
    @State private var loadingModels = false
    @State private var instructions = ""
    @State private var submitting = false
    @State private var err = ""

    private var harnesses: [HarnessInfo] {
        if let h = hub.settings?.harnesses, !h.isEmpty { return h }
        return ["opencode", "codebuddy", "qoder", "codex", "claude", "grok", "pi"].map { HarnessInfo(id: $0, label: $0) }
    }

    private func slug(_ s: String) -> String {
        s.lowercased()
            .replacingOccurrences(of: "[^a-z0-9_-]+", with: "-", options: .regularExpression)
            .replacingOccurrences(of: "^-+|-+$", with: "", options: .regularExpression)
    }

    var body: some View {
        let ph = resolveDefaultModel(hub.settings?.defaultModel ?? "", harness)
        let form = Form {
            #if os(iOS)
            Section {
                HStack {
                    Spacer()
                    BotAvatar(id: botId.isEmpty ? (name.isEmpty ? "new" : name) : botId, size: 68, avatar: avatar, color: color)
                    Spacer()
                }
                .listRowBackground(Color.clear)
            }
            #endif
            Section {
                #if os(macOS)
                HStack(spacing: 12) {
                    Text(hub.t("botName")).frame(width: 118, alignment: .leading).foregroundStyle(p.text)
                    TextField("", text: Binding(get: { name }, set: {
                        name = $0
                        if !idTouched { botId = slug($0) }
                    }), prompt: Text(hub.t("botNameExamplePh")))
                        .labelsHidden()
                }
                HStack(spacing: 12) {
                    Text("ID").frame(width: 118, alignment: .leading).foregroundStyle(p.text)
                    TextField("", text: Binding(get: { botId }, set: { idTouched = true; botId = $0 }), prompt: Text(hub.t("botIdPh")))
                        .labelsHidden()
                        .monoInput()
                }
                HStack(spacing: 12) {
                    Text(hub.t("descOptional")).frame(width: 118, alignment: .leading).foregroundStyle(p.text)
                    TextField("", text: $desc, prompt: Text(hub.t("botDescCreatePh")))
                        .labelsHidden()
                }
                #else
                TextField(hub.t("botName"), text: Binding(get: { name }, set: {
                    name = $0
                    if !idTouched { botId = slug($0) }
                }), prompt: Text(hub.t("botNameExamplePh")))
                TextField("ID", text: Binding(get: { botId }, set: { idTouched = true; botId = $0 }), prompt: Text(hub.t("botIdPh")))
                    .monoInput()
                TextField(hub.t("descOptional"), text: $desc, prompt: Text(hub.t("botDescCreatePh")))
                #endif
            }
            Section(hub.t("appearance")) {
                BotAppearancePicker(
                    botId: botId.isEmpty ? (name.isEmpty ? "new" : name) : botId,
                    avatar: $avatar,
                    color: $color
                )
            }
            Section {
                #if os(macOS)
                HStack(spacing: 12) {
                    Text(hub.t("harnessBackend")).frame(width: 118, alignment: .leading).foregroundStyle(p.text)
                    Picker("", selection: Binding(
                        get: { harness },
                        set: { harness = $0; harnessTouched = true }
                    )) {
                        ForEach(harnesses) { Text($0.title).tag($0.id) }
                    }
                    .labelsHidden()
                    .fixedSize()
                    Spacer()
                }
                let cands = hub.models[harness] ?? []
                HStack(spacing: 12) {
                    Text(hub.t("modelOptional")).frame(width: 118, alignment: .leading).foregroundStyle(p.text)
                    TextField("", text: $model, prompt: Text(ph.isEmpty ? hub.t("inheritDefaultPh") : ph))
                        .labelsHidden()
                        .monoInput()
                    Menu {
                        if !model.trimmingCharacters(in: .whitespaces).isEmpty {
                            Button(hub.t("clear")) { model = "" }
                            Divider()
                        }
                        if cands.isEmpty {
                            Button(loadingModels ? hub.t("loading") : hub.t("optionalModels")) {
                                loadCands(for: harness)
                            }
                            .disabled(loadingModels)
                        } else {
                            ForEach(Array(cands.prefix(40)), id: \.self) { m in
                                Button(m) { model = m }
                            }
                        }
                    } label: {
                        Label(hub.t("optionalModels"), systemImage: "list.bullet").labelStyle(.iconOnly)
                    }
                    .menuIndicator(.hidden)
                    .fixedSize()
                }
                #else
                Picker(hub.t("harnessBackend"), selection: Binding(
                    get: { harness },
                    set: { harness = $0; harnessTouched = true }
                )) {
                    ForEach(harnesses) { Text($0.title).tag($0.id) }
                }
                let cands = hub.models[harness] ?? []
                HStack(spacing: 8) {
                    TextField(hub.t("modelOptional"), text: $model, prompt: Text(ph.isEmpty ? hub.t("inheritDefaultPh") : ph)).monoInput()
                    Menu {
                        if !model.trimmingCharacters(in: .whitespaces).isEmpty {
                            Button(hub.t("clear")) { model = "" }
                            Divider()
                        }
                        if cands.isEmpty {
                            Button(loadingModels ? hub.t("loading") : hub.t("optionalModels")) {
                                loadCands(for: harness)
                            }
                            .disabled(loadingModels)
                        } else {
                            ForEach(Array(cands.prefix(40)), id: \.self) { m in
                                Button(m) { model = m }
                            }
                        }
                    } label: {
                        Label(hub.t("optionalModels"), systemImage: "list.bullet").labelStyle(.iconOnly)
                    }
                    .menuIndicator(.hidden)
                    .fixedSize()
                }
                #endif
            }
            Section(hub.t("agentsCreateLbl")) {
                TextField(hub.t("agentsCreateLbl"), text: $instructions, prompt: Text(hub.t("agentsCreatePh")), axis: .vertical)
                    .lineLimit(4...16)
                    .labelsHidden()
            }
            if !err.isEmpty { Section { StatusText(text: err) } }
        }
        .hubForm()

        #if os(macOS)
        VStack(spacing: 0) {
            HStack {
                Text(hub.t("newBot"))
                    .font(.system(size: AppFont.title, weight: .semibold))
                    .foregroundStyle(p.text)
                Spacer()
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 14)
            .background(p.page)
            Rectangle().fill(p.s2).frame(height: 1)

            HStack {
                Spacer()
                BotAvatar(id: botId.isEmpty ? (name.isEmpty ? "new" : name) : botId, size: 54, avatar: avatar, color: color)
                Spacer()
            }
            .padding(.top, 12)
            .padding(.bottom, 4)
            .background(p.page)

            form

            Rectangle().fill(p.s2).frame(height: 1)
            HStack(spacing: 10) {
                Spacer()
                Button(hub.t("close")) { router.sheet = nil }
                    .buttonStyle(.bordered)
                if submitting {
                    ProgressView().controlSize(.small)
                } else {
                    Button { create() } label: {
                        Text(hub.t("create"))
                            .font(.system(size: AppFont.secondary, weight: .semibold))
                            .foregroundStyle(p.card)
                            .padding(.horizontal, 16)
                            .padding(.vertical, 6)
                            .background(p.accent, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 12)
            .background(p.page)
        }
        .background(p.page)
        .task {
            if hub.settings == nil { _ = try? await hub.loadSettings() }
            if !harnessTouched {
                harness = hub.settings?.defaultHarness ?? harnesses.first?.id ?? "opencode"
            }
        }
        .task(id: harness) {
            if hub.isConnected && (hub.models[harness] ?? []).isEmpty {
                loadCands(for: harness)
            }
        }
        #else
        form
            .navigationTitle(hub.t("newBot"))
            .inlineNavTitle()
            .toolbar { SheetToolbar(actionTitle: hub.t("create"), loading: submitting) { create() } }
            .task {
                if hub.settings == nil { _ = try? await hub.loadSettings() }
                if !harnessTouched {
                    harness = hub.settings?.defaultHarness ?? harnesses.first?.id ?? "opencode"
                }
            }
            .task(id: harness) {
                if hub.isConnected && (hub.models[harness] ?? []).isEmpty {
                    loadCands(for: harness)
                }
            }
        #endif
    }

    private func loadCands(for target: String) {
        guard !loadingModels else { return }
        loadingModels = true
        Task {
            _ = try? await hub.loadModels(target)
            loadingModels = false
        }
    }

    private func create() {
        err = ""
        let n = name.trimmingCharacters(in: .whitespaces)
        let id = botId.trimmingCharacters(in: .whitespaces)
        if n.isEmpty { err = hub.t("fillBotName"); return }
        if id.range(of: "^[a-zA-Z0-9][a-zA-Z0-9_-]*$", options: .regularExpression) == nil { err = hub.t("idInvalid"); return }
        var body: [String: Any] = ["id": id, "name": n, "harness": harness]
        let d = desc.trimmingCharacters(in: .whitespaces), m = model.trimmingCharacters(in: .whitespaces)
        let ins = instructions.trimmingCharacters(in: .whitespacesAndNewlines)
        if !d.isEmpty { body["description"] = d }
        if !avatar.isEmpty { body["avatar"] = avatar }
        if !color.isEmpty { body["color"] = color }
        if !m.isEmpty { body["model"] = m }
        if !ins.isEmpty { body["instructions"] = ins }
        submitting = true
        Task {
            do {
                let st = try await hub.createBot(body)
                router.sheet = nil
                router.openBot(st.id)
            } catch {
                err = errText(error, hub.t("createFailed"))
                submitting = false
            }
        }
    }
}
