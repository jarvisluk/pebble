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
    @State private var harness = ""
    @State private var harnessTouched = false
    @State private var permMode = "auto"
    @State private var permModeTouched = false
    @State private var model = ""
    @State private var instructions = ""
    @State private var submitting = false
    @State private var err = ""

    private var harnesses: [HarnessInfo] { installedHarnessList(from: hub) }

    private func slug(_ s: String) -> String {
        s.lowercased()
            .replacingOccurrences(of: "[^a-z0-9_-]+", with: "-", options: .regularExpression)
            .replacingOccurrences(of: "^-+|-+$", with: "", options: .regularExpression)
    }

    var body: some View {
        let per = (hub.settings?.defaultModels?[harness] ?? "").trimmingCharacters(in: .whitespaces)
        let ph = per.isEmpty ? resolveDefaultModel(hub.settings?.defaultModel ?? "", harness) : per
        let form = Form {
            Section {
                HubFormField(label: hub.t("botName")) {
                    TextField("", text: Binding(get: { name }, set: {
                        name = $0
                        if !idTouched { botId = slug($0) }
                    }), prompt: Text(hub.t("botNameExamplePh")))
                }
                HubFormField(label: "ID") {
                    TextField("", text: Binding(get: { botId }, set: { idTouched = true; botId = $0 }), prompt: Text(hub.t("botIdPh")))
                        .monoInput()
                }
                HubFormField(label: hub.t("descOptional")) {
                    TextField("", text: $desc, prompt: Text(hub.t("botDescCreatePh")))
                }
            } header: {
                HStack {
                    Spacer()
                    BotAvatar(id: botId.isEmpty ? (name.isEmpty ? "new" : name) : botId, size: 68, avatar: avatar, color: color)
                    Spacer()
                }
                .padding(.top, 2)
                .padding(.bottom, 8)
            }
            Section(hub.t("appearance")) {
                BotAppearancePicker(
                    botId: botId.isEmpty ? (name.isEmpty ? "new" : name) : botId,
                    avatar: $avatar,
                    color: $color
                )
            }
            Section {
                VStack(alignment: .leading, spacing: 3) {
                    HubMenuPicker(
                        title: hub.t("harnessBackend"),
                        selection: Binding(
                            get: { harness },
                            set: { next in
                                guard next != harness else { return }
                                harness = next
                                harnessTouched = true
                                model = ""
                            }
                        ),
                        options: harnesses.map { ($0.id, $0.title) },
                        fallbackLabel: harnessLabelForSelection(harness, from: hub),
                        disabled: harnesses.isEmpty
                    )
                    if harnesses.isEmpty {
                        Text(hub.t("engineNoInstalled"))
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(p.mute)
                    }
                }
                let supportedModes = hub.supportedPermissionModes(for: harness)
                let activePerm = supportedModes.contains(permMode) ? permMode : hub.clampPermissionMode(permMode, harnessId: harness)
                if supportedModes.count <= 1 {
                    let onlyMode = supportedModes.first ?? "auto"
                    VStack(alignment: .leading, spacing: 3) {
                        LabeledContent(hub.t("permMode")) {
                            Text(hub.t("permMode_\(onlyMode)")).foregroundStyle(p.text2)
                        }
                        Text(hub.t("permModeUnsupported", ["mode": hub.t("permMode_\(onlyMode)")]))
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(p.mute)
                    }
                } else {
                    VStack(alignment: .leading, spacing: 3) {
                        HubMenuPicker(
                            title: hub.t("permMode"),
                            selection: Binding(
                                get: { activePerm },
                                set: { permMode = $0; permModeTouched = true }
                            ),
                            options: supportedModes.map { ($0, hub.t("permMode_\($0)")) }
                        )
                        Text(hub.t("permDesc_\(activePerm)"))
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(p.mute)
                    }
                }
                HubFormField(label: hub.t("modelOptional")) {
                    ModelFieldRow(
                        title: hub.t("modelOptional"),
                        harness: harness,
                        text: $model,
                        placeholder: ph.isEmpty ? hub.t("inheritDefaultPh") : ph,
                        onError: { err = $0 }
                    )
                }
            }
            Section(hub.t("agentsCreateLbl")) {
                TextField(hub.t("agentsCreateLbl"), text: $instructions, prompt: Text(hub.t("agentsCreatePh")), axis: .vertical)
                    .lineLimit(4...16)
                    .labelsHidden()
            }
            if !err.isEmpty { Section { StatusText(text: err) } }
        }
        .hubForm()

        HubSheetScaffold(
            title: hub.t("newBot"),
            onClose: { router.sheet = nil },
            actionTitle: hub.t("create"),
            loading: submitting,
            onAction: { create() }
        ) {
            form
        }
        .task {
                if hub.settings == nil { _ = try? await hub.loadSettings() }
                if !harnessTouched {
                    let preferred = hub.settings?.defaultHarness ?? ""
                    if !preferred.isEmpty && harnesses.contains(where: { $0.id == preferred }) {
                        harness = preferred
                    } else {
                        harness = harnesses.first?.id ?? ""
                    }
                }
                if !permModeTouched {
                    permMode = hub.clampPermissionMode(hub.settings?.defaultPermissionMode, harnessId: harness)
                }
            }
            .task(id: harness) {
                permMode = hub.clampPermissionMode(permMode, harnessId: harness)
            }
    }

    private func create() {
        err = ""
        let n = name.trimmingCharacters(in: .whitespaces)
        let id = botId.trimmingCharacters(in: .whitespaces)
        if n.isEmpty { err = hub.t("fillBotName"); return }
        if id.range(of: "^[a-zA-Z0-9][a-zA-Z0-9_-]*$", options: .regularExpression) == nil { err = hub.t("idInvalid"); return }
        guard !harness.isEmpty && harnesses.contains(where: { $0.id == harness }) else {
            err = hub.t("engineNoInstalled")
            return
        }
        var body: [String: Any] = ["id": id, "name": n, "harness": harness]
        let supportedModes = hub.supportedPermissionModes(for: harness)
        body["permissionMode"] = supportedModes.contains(permMode) ? permMode : hub.clampPermissionMode(permMode, harnessId: harness)
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
