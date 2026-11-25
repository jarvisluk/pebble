import SwiftUI

struct SettingsGeneralTab: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    @Environment(\.openURL) private var openURL

    var body: some View {
        appearanceSection
        notificationsSection
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
                    ("zh-CN", "简体中文"),
                    ("en", "English"),
                ]
            )
        }
    }

    private var notificationsSection: some View {
        let pm = PushManager.shared
        return Section(hub.t(platformNotificationSectionTitleKey)) {
            HStack {
                Text(hub.t("pushSystemPerm"))
                Spacer()
                switch pm.authStatus {
                case .authorized, .provisional, .ephemeral:
                    Text(hub.t("pushGranted")).foregroundStyle(p.sageInk)
                case .denied:
                    Button(hub.t("pushOpenSettings")) {
                        if let u = platformNotificationSettingsURL { openURL(u) }
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                default:
                    Button(hub.t("pushEnable")) {
                        Task { await pm.requestPermissionAndRegister() }
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }
            }
            PlatformNotificationExtrasRows()
        }
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
