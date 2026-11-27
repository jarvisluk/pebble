import SwiftUI

struct SettingsGeneralTab: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    @Environment(\.openURL) private var openURL

    @State private var notesExpanded = false

    var body: some View {
        appearanceSection
        notificationsSection
        if hub.isConnected && hub.supports("update.check") {
            updateSection
        }
        aboutSection
    }

    private var appearanceSection: some View {
        @Bindable var h = hub
        return Section(hub.t("appearance")) {
            HubSegmentedPicker(
                selection: $h.themePref,
                options: [
                    ("system", hub.t("followSystem")),
                    ("light", hub.t("themeLight")),
                    ("dark", hub.t("themeDark")),
                ]
            )
            HubMenuPicker(
                title: hub.t("language"),
                selection: $h.langPref,
                options: [
                    ("system", hub.t("followSystem")),
                    ("zh-CN", "简体中文"), // i18n:allow-cjk
                    ("en", "English"),
                ]
            )
        }
    }

    private var notificationsSection: some View {
        let pm = PushManager.shared
        #if os(iOS)
        let granted = pm.authStatus == .authorized || pm.authStatus == .provisional || pm.authStatus == .ephemeral
        #else
        let granted = pm.authStatus == .authorized || pm.authStatus == .provisional
        #endif
        return Section(hub.t(platformNotificationSectionTitleKey)) {
            HStack {
                Text(hub.t("pushSystemPerm"))
                Spacer()
                Toggle("", isOn: Binding(
                    get: { granted },
                    set: { next in
                        if next {
                            if pm.authStatus == .denied {
                                if let u = platformNotificationSettingsURL { openURL(u) }
                            } else {
                                Task { await pm.requestPermissionAndRegister() }
                            }
                        } else {
                            if let u = platformNotificationSettingsURL { openURL(u) }
                        }
                    }
                ))
                .labelsHidden()
                .toggleStyle(.switch)
                .tint(p.sageInk)
                .controlSize(platformSwitchControlSize)
                .accessibilityLabel(hub.t("pushSystemPerm"))
            }
            .task { await pm.refreshAuthorizationStatus() }
            PlatformNotificationExtrasRows()
        }
    }

    private var updateSection: some View {
        let st = hub.updateStatus
        let checking = hub.updateChecking || st?.state == "checking"
        let hasUpdate = hub.effectiveHasUpdate
        let latest = st?.latest
        let checkedStr = formattedCheckedAt(st?.checkedAt)
        let autoCheck = st?.config.autoCheck ?? true
        let interval = st?.config.interval ?? "6h"
        let channel = st?.config.channel ?? "stable"

        return Section(hub.t("updateSection")) {
            HStack(spacing: 8) {
                if hasUpdate, let latest {
                    let verLabel = latest.tag.isEmpty ? "v\(latest.version)" : latest.tag
                    Tag(text: hub.t("updateAvailableTag", ["version": verLabel]), bg: p.sage, fg: p.sageInk)
                } else if st?.state == "error" {
                    Text(updateErrorText(st))
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(p.danger)
                } else {
                    Text(checkedStr.isEmpty ? hub.t("updateNeverChecked") : hub.t("updateUpToDate"))
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(p.text2)
                }
                Spacer()
                if checking {
                    ProgressView().controlSize(.small)
                } else {
                    Button(hub.t("updateCheckBtn")) {
                        Task { await hub.checkForUpdates() }
                    }
                    .font(.system(size: AppFont.secondary, weight: .semibold))
                }
            }

            if !checkedStr.isEmpty {
                LabeledContent(hub.t("updateLastChecked", ["time": ""]).trimmingCharacters(in: .whitespaces), value: checkedStr)
            }

            if hasUpdate, let latest, let url = URL(string: latest.url) {
                Link(destination: url) {
                    Label(hub.t("updateViewRelease"), systemImage: "arrow.up.right.square")
                        .foregroundStyle(p.text)
                }
            }

            if hasUpdate, let notes = latest?.notes, !notes.isEmpty {
                DisclosureGroup(hub.t("updateReleaseNotes"), isExpanded: $notesExpanded) {
                    Text(notes)
                        .font(.system(size: AppFont.caption, design: .monospaced))
                        .foregroundStyle(p.text2)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .textSelection(.enabled)
                        .padding(.top, 4)
                }
            }

            if st != nil {
                HStack {
                    Text(hub.t("updateAutoCheck"))
                    Spacer()
                    Toggle("", isOn: Binding(
                        get: { autoCheck },
                        set: { next in Task { await hub.saveUpdateConfig(autoCheck: next) } }
                    ))
                    .labelsHidden()
                    .toggleStyle(.switch)
                    .tint(p.sageInk)
                    .controlSize(platformSwitchControlSize)
                    .accessibilityLabel(hub.t("updateAutoCheck"))
                }

                if autoCheck {
                    HubMenuPicker(
                        title: hub.t("updateInterval"),
                        selection: Binding(
                            get: { interval },
                            set: { next in Task { await hub.saveUpdateConfig(interval: next) } }
                        ),
                        options: [
                            ("30m", hub.t("updateInterval_30m")),
                            ("1h", hub.t("updateInterval_1h")),
                            ("6h", hub.t("updateInterval_6h")),
                            ("24h", hub.t("updateInterval_24h")),
                        ]
                    )
                }

                HubMenuPicker(
                    title: hub.t("updateChannel"),
                    selection: Binding(
                        get: { channel },
                        set: { next in Task { await hub.saveUpdateConfig(channel: next) } }
                    ),
                    options: [
                        ("stable", hub.t("updateChannel_stable")),
                        ("prerelease", hub.t("updateChannel_prerelease")),
                    ]
                )
            }
        }
        .task {
            if hub.updateStatus == nil {
                await hub.refreshUpdateStatus()
            }
        }
    }

    private func updateErrorText(_ st: UpdateStatus?) -> String {
        switch st?.errorCode {
        case "rate_limited": return hub.t("updateErrRateLimited")
        case "network_error": return hub.t("updateErrNetwork")
        default:
            let msg = st?.error ?? ""
            return msg.isEmpty ? hub.t("updateErrGeneric") : msg
        }
    }

    private func formattedCheckedAt(_ iso: String?) -> String {
        guard let iso, !iso.isEmpty else { return "" }
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let date = f.date(from: iso) ?? ISO8601DateFormatter().date(from: iso)
        guard let date else { return iso }
        let df = DateFormatter()
        df.dateStyle = .short
        df.timeStyle = .short
        return df.string(from: date)
    }

    private var aboutSection: some View {
        Section(hub.t("aboutSection")) {
            LabeledContent(hub.t("appVersionLbl"), value: platformAppVersionString)
            LabeledContent(hub.t("serverVersionLbl"), value: hub.serverInfo?.version ?? "—")
            Text(hub.t("openSourceDesc"))
                .font(.system(size: AppFont.secondary))
                .foregroundStyle(p.text2)
            Link(destination: URL(string: "https://github.com/jarvisluk/pebble")!) {
                Label(hub.t("openSourceLinkBtn"), systemImage: "star")
                    .foregroundStyle(p.text)
            }
            .foregroundStyle(p.text)
        }
    }
}
