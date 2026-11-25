import SwiftUI

struct SettingsSidebarView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p

    var compact = true

    var body: some View {
        if compact {
            List {
                ForEach(SettingsGroup.allCases) { group in
                    Section {
                        ForEach(group.tabs) { tab in
                            NavigationLink(value: tab) {
                                tabLabel(tab)
                            }
                            .listRowBackground(p.card)
                        }
                    } header: {
                        Text(hub.t(group.titleKey))
                            .font(.system(size: AppFont.caption, weight: .medium))
                            .foregroundStyle(p.mute)
                    }
                }
            }
            .navigationDestination(for: SettingsTab.self) { tab in
                SettingsView(tab: tab)
            }
            .hubColumnList(compact: true, page: p.page)
            .hubListSectionSpacing(12)
            .contentMargins(.top, 8, for: .scrollContent)
            .navigationTitle(hub.t("settings"))
            .inlineNavTitle()
        } else {
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 14) {
                    ForEach(SettingsGroup.allCases) { group in
                        VStack(alignment: .leading, spacing: 4) {
                            Text(hub.t(group.titleKey))
                                .font(.system(size: AppFont.caption, weight: .semibold))
                                .foregroundStyle(p.mute)
                                .padding(.horizontal, 12)
                                .padding(.top, 2)
                            ForEach(group.tabs) { tab in
                                let active = (router.settingsTab ?? .general) == tab
                                Button {
                                    router.selectSettingsTab(tab)
                                } label: {
                                    tabLabel(tab)
                                        .font(.system(size: AppFont.body, weight: active ? .semibold : .regular))
                                        .foregroundStyle(active ? p.text : p.text2)
                                        .padding(.vertical, 9)
                                        .padding(.horizontal, 12)
                                        .frame(maxWidth: .infinity, alignment: .leading)
                                        .background(
                                            RoundedRectangle(cornerRadius: 12, style: .continuous)
                                                .fill(active ? Color.primary.opacity(p.dark ? 0.14 : 0.08) : Color.clear)
                                        )
                                        .contentShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                                }
                                .buttonStyle(.plain)
                            }
                        }
                    }
                }
                .padding(.horizontal, 12)
                .padding(.top, 4)
                .padding(.bottom, 12)
            }
            .safeAreaInset(edge: .top, spacing: 0) {
                HStack {
                    Button {
                        router.requestExitSettings()
                    } label: {
                        Image(systemName: "chevron.left")
                            .font(.system(size: AppFont.cardTitle, weight: .medium))
                            .foregroundStyle(p.text)
                            .frame(width: 28, height: 28)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .help(hub.t("backToChat"))
                    Spacer()
                    Text(hub.t("settings"))
                        .font(.system(size: AppFont.cardTitle, weight: .semibold))
                        .foregroundStyle(p.text)
                    Spacer()
                    Color.clear.frame(width: 28, height: 28)
                }
                .padding(.horizontal, 12)
                .padding(.top, 2)
                .padding(.bottom, 8)
            }
            .navigationTitle("")
            .inlineNavTitle()
        }
    }

    private func tabLabel(_ tab: SettingsTab) -> some View {
        Label(hub.t(tab.titleKey), systemImage: tab.icon)
    }
}
