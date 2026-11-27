import SwiftUI

struct SettingsDesktopTab: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p

    @Binding var error: String
    @Binding var saved: String

    @State private var permTarget = "local"
    @State private var deskPerms: [DesktopPermissionItem] = []
    @State private var deskPermsLoading = false
    @State private var permBusyId = ""

    @State private var deskHarness = ""
    @State private var deskModel = ""
    @State private var deskTouched = false
    @State private var deskSaving = false

    private var nonVirtualTargets: [DesktopTarget] {
        hub.desktopTargets.filter { $0.virtual != true }
    }

    private var targetOptions: [(String, String)] {
        if nonVirtualTargets.isEmpty {
            return [("local", hub.t("localDesktopName"))]
        }
        return nonVirtualTargets.map { tr in
            let lbl = tr.id == "local" ? hub.t("localDesktopName") : ((tr.name ?? "").isEmpty ? tr.id : tr.name!)
            return (tr.id, lbl)
        }
    }

    private var localEngineText: String {
        if let local = hub.desktopTargets.first(where: { $0.id == "local" }) {
            if let bk = local.backend, !bk.isEmpty { return bk }
            if local.supported == false { return hub.t("unavailable") }
        }
        return hub.isConnected ? "WebRTC / H.265·H.264" : hub.t("checking")
    }

    private var deskDirty: Bool {
        guard let s = hub.settings else { return false }
        return deskTouched
            && (deskHarness != (s.desktopHarness ?? "")
                || deskModel.trimmingCharacters(in: .whitespaces) != (s.desktopModel ?? "").trimmingCharacters(in: .whitespaces))
    }

    var body: some View {
        desktopPermissionsSection
        desktopWorkerSection
            .task(id: hub.isConnected) {
                guard hub.isConnected else { return }
                _ = try? await hub.loadSettings()
                _ = try? await hub.refreshDesktopTargets()
                if !targetOptions.contains(where: { $0.0 == permTarget }) {
                    permTarget = targetOptions.first?.0 ?? "local"
                }
                syncWorker()
                await loadPerms(for: permTarget)
            }
            .onAppear {
                syncWorker()
                registerHandlers()
            }
            .onDisappear {
                router.settingsDirty = false
                router.settingsSaveAction = nil
                router.settingsDiscardAction = nil
            }
            .onChange(of: hub.settings?.desktopHarness) { _, _ in syncWorker() }
            .onChange(of: hub.settings?.desktopModel) { _, _ in syncWorker() }
            .onChange(of: deskDirty) { _, dirty in
                router.settingsDirty = dirty
            }
    }

    private var desktopPermissionsSection: some View {
        Section {
            if targetOptions.count > 1 {
                HubMenuPicker(
                    title: hub.t("deskPermTarget"),
                    selection: Binding(
                        get: { permTarget },
                        set: { next in
                            guard next != permTarget else { return }
                            permTarget = next
                            Task { await loadPerms(for: next) }
                        }
                    ),
                    options: targetOptions
                )
            }

            if deskPerms.isEmpty {
                if deskPermsLoading {
                    ProgressView()
                } else {
                    Text(hub.t("deskPermsDesc")).foregroundStyle(.secondary)
                }
            } else {
                ForEach(deskPerms) { item in
                    let stLabel = item.status == "granted" ? hub.t("deskPermGranted")
                        : item.status == "denied" ? hub.t("deskPermDenied")
                        : item.status == "unsupported" ? hub.t("deskPermUnsupported")
                        : hub.t("deskPermNotDetermined")
                    let bg = item.status == "granted" ? p.sage : item.status == "denied" ? p.pink : p.s1
                    let fg: Color = item.status == "granted" ? p.sageInk : item.status == "denied" ? p.danger : .secondary
                    VStack(alignment: .leading, spacing: 6) {
                        HStack(spacing: 8) {
                            Text(permText("deskPermName_\(item.id)", item.name))
                                .font(.system(size: AppFont.body, weight: .semibold))
                            Spacer()
                            Tag(text: stLabel, bg: bg, fg: fg)
                        }
                        Text(permText("deskPermDesc_\(item.id)", item.description))
                            .font(.system(size: AppFont.secondary))
                            .foregroundStyle(.secondary)
                        if (item.canPrompt == true && item.status != "granted") || item.canOpenSettings == true {
                            HStack(spacing: 8) {
                                if item.canPrompt == true && item.status != "granted" {
                                    Button(hub.t("deskPermRequest")) { reqPerm(item.id, openSettings: false) }
                                        .buttonStyle(HubPrimaryButtonStyle())
                                        .disabled(permBusyId == item.id)
                                }
                                if item.canOpenSettings == true {
                                    Button(hub.t("deskPermOpenSettings")) { reqPerm(item.id, openSettings: true) }
                                        .buttonStyle(.bordered)
                                        .controlSize(.small)
                                        .disabled(permBusyId == item.id)
                                }
                            }
                        }
                    }
                    .padding(.vertical, 2)
                }
            }

            Button { router.openDesktop(permTarget) } label: {
                Label(hub.t("openDesktop"), systemImage: "display")
            }
        } header: {
            HStack {
                Text(hub.t("deskPermsTitle"))
                Spacer()
                Button { Task { await loadPerms(for: permTarget) } } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .buttonStyle(.borderless)
                .disabled(deskPermsLoading)
            }
        } footer: {
            Text(hub.t("deskPermsDesc"))
        }
    }

    private var desktopWorkerSection: some View {
        let harnesses = installedHarnessList(from: hub)
        let defHarness = hub.settings?.defaultHarness ?? harnesses.first?.id ?? ""
        return Section {
            LabeledContent(hub.t("localDesktopEngine")) {
                Text(localEngineText).foregroundStyle(.secondary)
            }
            HubMenuPicker(
                title: hub.t("deskWorkerHarnessLabel"),
                selection: Binding(
                    get: { deskHarness },
                    set: { next in
                        guard next != deskHarness else { return }
                        deskHarness = next
                        deskTouched = true
                        saved = ""
                    }
                ),
                options: [("", hub.t("deskWorkerFollowBot"))] + harnesses.map { ($0.id, $0.title) },
                fallbackLabel: deskHarness.isEmpty ? hub.t("deskWorkerFollowBot") : harnessLabelForSelection(deskHarness, from: hub)
            )
            HubFormField(label: hub.t("deskWorkerModelLabel")) {
                if deskHarness.isEmpty {
                    GlobalModelFieldRow(
                        title: hub.t("deskWorkerModelLabel"),
                        defaultHarness: defHarness,
                        harnesses: harnesses,
                        text: Binding(
                            get: { deskModel },
                            set: { next in
                                guard next != deskModel else { return }
                                deskModel = next
                                deskTouched = true
                                saved = ""
                            }
                        ),
                        placeholder: hub.t("deskWorkerModelPh"),
                        onError: { error = $0 }
                    )
                } else {
                    ModelFieldRow(
                        title: hub.t("deskWorkerModelLabel"),
                        harness: deskHarness,
                        text: Binding(
                            get: { deskModel },
                            set: { next in
                                guard next != deskModel else { return }
                                deskModel = next
                                deskTouched = true
                                saved = ""
                            }
                        ),
                        placeholder: hub.t("deskWorkerModelPh"),
                        onError: { error = $0 }
                    )
                }
            }
            HStack {
                Spacer()
                if deskSaving {
                    ProgressView().controlSize(.small)
                } else {
                    Button(hub.t("save")) {
                        Task { _ = await saveWorker() }
                    }
                    .buttonStyle(HubPrimaryButtonStyle())
                    .disabled(!deskDirty)
                }
            }
        } header: {
            Text(hub.t("deskWorkerTitle"))
        }
    }

    private func permText(_ key: String, _ fallback: String) -> String {
        let val = hub.t(key)
        return val == key ? fallback : val
    }

    private func syncWorker() {
        guard !deskTouched, let s = hub.settings else { return }
        deskHarness = s.desktopHarness ?? ""
        deskModel = s.desktopModel ?? ""
    }

    private func registerHandlers() {
        router.settingsDirty = deskDirty
        router.settingsSaveAction = { await saveWorker() }
        router.settingsDiscardAction = {
            deskTouched = false
            deskHarness = hub.settings?.desktopHarness ?? ""
            deskModel = hub.settings?.desktopModel ?? ""
        }
    }

    private func saveWorker() async -> Bool {
        error = ""
        saved = ""
        deskSaving = true
        defer { deskSaving = false }
        do {
            try await hub.setDesktopWorker(harness: deskHarness, model: deskModel)
            deskTouched = false
            deskHarness = hub.settings?.desktopHarness ?? ""
            deskModel = hub.settings?.desktopModel ?? ""
            router.settingsDirty = false
            saved = hub.t("deskWorkerSaved")
            return true
        } catch {
            self.error = errText(error, hub.t("saveFailed"))
            return false
        }
    }

    private func loadPerms(for target: String) async {
        deskPermsLoading = true
        defer { deskPermsLoading = false }
        if let resp = try? await hub.fetchDesktopPermissions(target) {
            deskPerms = resp.items ?? []
        }
    }

    private func reqPerm(_ id: String, openSettings: Bool) {
        error = ""
        saved = ""
        permBusyId = id
        Task {
            defer { permBusyId = "" }
            do {
                let res = try await hub.requestDesktopPermission(target: permTarget, permission: id, openSettings: openSettings)
                if let items = res.items { deskPerms = items }
                if res.openedSettings == true {
                    saved = hub.t("deskPermOpened")
                } else if res.prompted == true {
                    saved = hub.t("deskPermPrompted")
                }
            } catch {
                self.error = errText(error, hub.t("opFailed"))
            }
        }
    }
}
