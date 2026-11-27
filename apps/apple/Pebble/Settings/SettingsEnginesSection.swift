import SwiftUI

extension SettingsModelsTab {
    // MARK: Card 2 - Harnesses

    @ViewBuilder
    private var recheckHeaderButton: some View {
        if busy == "recheck" {
            ProgressView().controlSize(.small)
        } else {
            Button(hub.t("enginesRecheck")) {
                recheckEnvironment()
            }
            .buttonStyle(.borderless)
            .font(.system(size: AppFont.secondary))
            .textCase(nil)
        }
    }

    @ViewBuilder
    var enginesSection: some View {
        let installed = installedHarnesses
        let available = uninstalledHarnesses

        if installed.isEmpty {
            Section {
                Text(hub.t("engineNoInstalled")).foregroundStyle(.secondary)
                ForEach(available) { h in
                    uninstalledEngineRow(h)
                }
            } header: {
                HStack {
                    Text(hub.t("enginesTitle"))
                    Spacer()
                    recheckHeaderButton
                }
            } footer: {
                Text(hub.t("enginesIntro"))
            }
        } else {
            Section {
                ForEach(installed) { h in
                    installedEngineRow(h)
                }
                if !available.isEmpty {
                    DisclosureGroup(hub.t("enginesMoreAvailable", ["n": available.count]), isExpanded: $moreEnginesOpen) {
                        ForEach(available) { h in
                            uninstalledEngineRow(h)
                        }
                    }
                }
            } header: {
                HStack {
                    Text("\(hub.t("enginesInstalled")) (\(installed.count))")
                    Spacer()
                    recheckHeaderButton
                }
            } footer: {
                Text(hub.t("enginesIntro"))
            }
        }
    }

    @ViewBuilder
    private func installedEngineRow(_ h: HarnessInfo) -> some View {
        let loginTask = loginTasks[h.id]
        let loggingIn = loginTask?.state == "running"
        let needsLogin = h.requiresLogin == true && h.authStatus != "ready"
        let authReady = h.authStatus == "ready"

        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        Circle()
                            .fill(authReady ? p.sageInk : needsLogin ? p.danger : p.mute.opacity(0.45))
                            .frame(width: 7, height: 7)
                        Text(h.title)
                            .font(.system(size: AppFont.body, weight: .semibold))
                            .lineLimit(1)
                    }
                    if let sum = h.summary, !sum.isEmpty {
                        Text(sum)
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    } else {
                        Text(authReady ? hub.t("engineReady") : h.id)
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }
                Spacer(minLength: 8)
                if needsLogin {
                    Button {
                        startHarnessLogin(h.id)
                    } label: {
                        if loggingIn {
                            HStack(spacing: 6) {
                                ProgressView().controlSize(.small)
                                Text(hub.t("engineLoggingIn"))
                            }
                        } else {
                            Text(hub.t("engineLoginBtn"))
                        }
                    }
                    .buttonStyle(HubPrimaryButtonStyle())
                    .disabled(loggingIn)
                }
            }

            if loggingIn {
                HStack(spacing: 8) {
                    if let urlStr = loginTask?.loginUrl, let u = URL(string: urlStr) {
                        Button(hub.t("engineOpenLogin")) { openExternalURL(u) }
                            .buttonStyle(.bordered)
                            .controlSize(.small)
                    }
                    Button(hub.t("engineCheckLogin")) { pollHarnessLoginNow(h.id) }
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                }
            }

            if needsLogin || (loginTask?.state == "error" && !(loginTask?.authDetail ?? h.authDetail ?? "").isEmpty) {
                HStack(spacing: 6) {
                    if needsLogin {
                        Tag(text: hub.t("engineNeedsLogin"), bg: p.pink, fg: p.pinkInk)
                    }
                    if let detail = loginTask?.authDetail ?? h.authDetail, !detail.isEmpty {
                        Text(detail)
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(loginTask?.state == "error" ? p.danger : .secondary)
                            .lineLimit(2)
                    }
                }
            }

            if let loginErr = loginTask?.error, !loginErr.isEmpty, loginTask?.state == "error" {
                Text(loginErr)
                    .font(.system(size: AppFont.caption))
                    .foregroundStyle(p.danger)
            }
        }
        .padding(.vertical, 2)
    }

    @ViewBuilder
    private func uninstalledEngineRow(_ h: HarnessInfo) -> some View {
        let task = installTasks[h.id]
        let installing = task?.state == "running"
        let cmd = (h.install?.command ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        let urlStr = (h.install?.url ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        let isExpanded = expandedInstall.contains(h.id)

        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(h.title)
                        .font(.system(size: AppFont.body, weight: .semibold))
                    if let sum = h.summary, !sum.isEmpty {
                        Text(sum)
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer()
                if h.canAutoInstall == true {
                    Button {
                        startHarnessInstall(h.id)
                    } label: {
                        if installing {
                            HStack(spacing: 6) {
                                ProgressView().controlSize(.small)
                                Text(hub.t("engineInstalling"))
                            }
                        } else {
                            Text(hub.t("engineInstallBtn"))
                        }
                    }
                    .buttonStyle(HubPrimaryButtonStyle())
                    .disabled(installing)
                } else {
                    Button(isExpanded ? hub.t("collapse") : hub.t("engineManualInstall")) {
                        if isExpanded { expandedInstall.remove(h.id) } else { expandedInstall.insert(h.id) }
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }
            }

            if isExpanded || task?.state == "error" {
                if let instErr = task?.error, !instErr.isEmpty {
                    Text(instErr)
                        .font(.system(size: AppFont.caption))
                        .foregroundStyle(p.danger)
                }
                let shownCmd = cmd.isEmpty ? hub.t("engineManualHint", ["name": h.title]) : cmd
                HStack(spacing: 8) {
                    Text(shownCmd)
                        .font(.system(size: AppFont.caption, design: .monospaced))
                        .foregroundStyle(p.text2)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    if !cmd.isEmpty {
                        Button(copiedKey == "eng-inst-\(h.id)" ? hub.t("copied") : hub.t("copy")) {
                            copyToClipboard(cmd)
                            copiedKey = "eng-inst-\(h.id)"
                            Task {
                                try? await Task.sleep(for: .milliseconds(1200))
                                if copiedKey == "eng-inst-\(h.id)" { copiedKey = "" }
                            }
                        }
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                    }
                }
                if !urlStr.isEmpty, let u = URL(string: urlStr) {
                    Button(hub.t("engineViewDocs")) { openExternalURL(u) }
                        .buttonStyle(.borderless)
                        .font(.system(size: AppFont.secondary))
                }
            }
        }
        .padding(.vertical, 4)
    }

    private func recheckEnvironment() {
        run("recheck", "opFailed") {
            _ = try await hub.loadSettings(fresh: true)
            hub.resetModelsCache()
            await preloadAllModels(force: true)
            return hub.t("ubRecheckDone")
        }
    }

    private func startHarnessInstall(_ id: String) {
        error = ""
        saved = ""
        pollTasks["install:\(id)"]?.cancel()
        pollTasks["install:\(id)"] = Task {
            do {
                var st = try await hub.startHarnessInstall(id)
                installTasks[id] = st
                while !Task.isCancelled && st.state == "running" {
                    try await Task.sleep(for: .milliseconds(1200))
                    if Task.isCancelled { return }
                    st = try await hub.harnessInstallStatus(id)
                    installTasks[id] = st
                }
                if Task.isCancelled { return }
                if st.state == "done" {
                    hub.resetModelsCache()
                    let fresh = try await hub.loadSettings(fresh: true)
                    await preloadAllModels(force: true)
                    let h = fresh.harnesses?.first(where: { $0.id == id })
                    saved = hub.t("engineInstallDone", ["label": h?.title ?? id])
                    if h?.requiresLogin == true && h?.authStatus == "needs_login" {
                        startHarnessLogin(id)
                    }
                } else if st.state == "error" {
                    expandedInstall.insert(id)
                }
            } catch {
                expandedInstall.insert(id)
                if !(error is CancellationError) {
                    self.error = errText(error, hub.t("opFailed"))
                }
            }
        }
    }

    private func startHarnessLogin(_ id: String) {
        error = ""
        saved = ""
        pollTasks["login:\(id)"]?.cancel()
        pollTasks["login:\(id)"] = Task {
            do {
                var st = try await hub.startHarnessLogin(id)
                loginTasks[id] = st
                if let urlStr = st.loginUrl, !urlStr.isEmpty, let u = URL(string: urlStr) {
                    openExternalURL(u)
                }
                while !Task.isCancelled && st.state == "running" && st.authStatus != "ready" {
                    try await Task.sleep(for: .milliseconds(1400))
                    if Task.isCancelled { return }
                    st = try await hub.harnessLoginStatus(id)
                    loginTasks[id] = st
                }
                if Task.isCancelled { return }
                if st.state == "done" || st.authStatus == "ready" {
                    hub.resetModelsCache()
                    _ = try? await hub.loadSettings(fresh: true)
                    await preloadAllModels(force: true)
                    saved = hub.t("engineLoginConfirmed")
                } else if st.state == "error", let msg = st.error, !msg.isEmpty {
                    error = msg
                }
            } catch {
                if !(error is CancellationError) {
                    self.error = errText(error, hub.t("opFailed"))
                }
            }
        }
    }

    private func pollHarnessLoginNow(_ id: String) {
        Task {
            if let st = try? await hub.harnessLoginStatus(id) {
                loginTasks[id] = st
                if st.state == "done" || st.authStatus == "ready" {
                    hub.resetModelsCache()
                    _ = try? await hub.loadSettings(fresh: true)
                    await preloadAllModels(force: true)
                    saved = hub.t("engineLoginConfirmed")
                }
            }
        }
    }
}
