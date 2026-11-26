import SwiftUI

extension SettingsModelsTab {
    // MARK: Card 1 - Magpie Gateway

    var magpieGatewaySection: some View {
        let mp = hub.settings?.magpie
        let installed = mp?.installed == true
        let providers = mp?.providers ?? []
        let presets = mp?.presets ?? []

        return Section {
            if !installed {
                Text(hub.t("hostNoMagpieGlobal")).foregroundStyle(.secondary)
                Button {
                    run("magpie:install", "opFailed") {
                        try await hub.installMagpie()
                        await preloadAllModels(force: true)
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
            } else {
                HubFormField(label: hub.t("mpWebUrl")) {
                    TextField(
                        hub.t("mpWebUrl"),
                        text: Binding(
                            get: { mpWebUrl },
                            set: {
                                guard $0 != mpWebUrl else { return }
                                mpWebUrl = $0
                                mpWebUrlTouched = true
                                saved = ""
                            }
                        ),
                        prompt: Text("https://magpie.example.com")
                    )
                    .monoInput()
                    Text(hub.t("mpWebUrlHint"))
                        .font(.system(size: AppFont.caption))
                        .foregroundStyle(.secondary)
                }

                let configuredIds = Set(providers.map(\.id))
                let extraPresets = presets.filter { !configuredIds.contains($0.id) }
                if !providers.isEmpty {
                    VStack(alignment: .leading, spacing: 6) {
                        Text(hub.t("providersTitle", ["n": providers.count]))
                            .font(.system(size: AppFont.secondary, weight: .medium))
                            .foregroundStyle(.secondary)
                        FlowLayout(spacing: 6) {
                            ForEach(providers) { prov in
                                Tag(
                                    text: (prov.name ?? "").isEmpty ? prov.id : prov.name!,
                                    bg: prov.enabled == false ? p.s1 : p.sage,
                                    fg: prov.enabled == false ? .secondary : p.sageInk
                                )
                            }
                        }
                    }
                } else if !extraPresets.isEmpty {
                    VStack(alignment: .leading, spacing: 6) {
                        Text(hub.t("mpPresetsTitle"))
                            .font(.system(size: AppFont.secondary, weight: .medium))
                            .foregroundStyle(.secondary)
                        FlowLayout(spacing: 6) {
                            ForEach(extraPresets) { pre in
                                Tag(text: (pre.name ?? "").isEmpty ? pre.id : pre.name!, bg: p.s1, fg: p.text2)
                            }
                        }
                    }
                }

                let addr = hub.activeServer?.address ?? hub.baseURL
                let local = ["localhost", "127.0.0.1", "[::1]"].contains { addr.contains($0) }
                if let web = mp?.webUrl, !web.isEmpty, !local, let url = URL(string: web) {
                    Button(hub.t("mpOpenWeb")) { openURL(url) }
                } else {
                    Button(hub.t("mpOpenApp")) {
                        run("magpie:open", "opFailed") { try await hub.openMagpie(); return nil }
                    }
                }
            }
        } header: {
            HStack {
                Text(hub.t("magpieGatewayTitle"))
                Spacer()
                Tag(
                    text: !installed ? hub.t("notInstalled") : mp?.healthy == true ? hub.t("gatewayOk") : hub.t("gatewayDown"),
                    bg: !installed ? p.s1 : mp?.healthy == true ? p.sage : p.pink,
                    fg: !installed ? .secondary : mp?.healthy == true ? p.sageInk : p.danger
                )
                .textCase(nil)
            }
        } footer: {
            if installed {
                Text(hub.t("mpManageInApp"))
            }
        }
    }
}
