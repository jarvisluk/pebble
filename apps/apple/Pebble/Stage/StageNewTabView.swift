import SwiftUI

struct StageNewTabView: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var canUseFiles: Bool
    var inWideStage: Bool = false
    var onOpenFiles: () -> Void
    var onOpenDesktop: () -> Void

    private struct LauncherEntry: Identifiable {
        var id: String
        var symbol: String
        var title: String
        var desc: String
        var action: () -> Void
    }

    private var entries: [LauncherEntry] {
        var list: [LauncherEntry] = []
        if canUseFiles {
            list.append(
                LauncherEntry(
                    id: "files",
                    symbol: "folder",
                    title: hub.t("workspaceFiles"),
                    desc: hub.t("stageFilesTabDesc"),
                    action: onOpenFiles
                )
            )
        }
        list.append(
            LauncherEntry(
                id: "desktop",
                symbol: "display",
                title: hub.t("remoteDesktop"),
                desc: hub.t("stageDesktopTabDesc"),
                action: onOpenDesktop
            )
        )
        return list
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(hub.t("stageNewTab"))
                        .font(.system(size: AppFont.title, weight: .bold))
                        .foregroundStyle(p.text)
                    Text(hub.t("stageNewTabSubtitle"))
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(p.mute)
                }

                VStack(spacing: 8) {
                    ForEach(entries) { item in
                        Button {
                            item.action()
                        } label: {
                            HStack(spacing: 12) {
                                Image(systemName: item.symbol)
                                    .font(.system(size: AppFont.cardTitle, weight: .medium))
                                    .foregroundStyle(p.text)
                                    .frame(width: 34, height: 34)
                                    .background(inWideStage ? p.card : p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))

                                VStack(alignment: .leading, spacing: 2) {
                                    Text(item.title)
                                        .font(.system(size: AppFont.body, weight: .semibold))
                                        .foregroundStyle(p.text)
                                    Text(item.desc)
                                        .font(.system(size: AppFont.secondary))
                                        .foregroundStyle(p.mute)
                                        .lineLimit(2)
                                }

                                Spacer(minLength: 8)

                                Image(systemName: "chevron.right")
                                    .font(.system(size: AppFont.caption, weight: .semibold))
                                    .foregroundStyle(p.mute)
                            }
                            .padding(.horizontal, 14)
                            .padding(.vertical, 12)
                            .background(inWideStage ? p.s1 : p.card, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                            .overlay(
                                RoundedRectangle(cornerRadius: 14, style: .continuous)
                                    .strokeBorder(p.s2, lineWidth: 0.5)
                            )
                            .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
                        }
                        .buttonStyle(PressStyle())
                    }
                }
            }
            .frame(maxWidth: 520, alignment: .leading)
            .padding(24)
            .frame(maxWidth: .infinity, minHeight: 360, alignment: .center)
        }
        .background(inWideStage ? p.card : p.page)
    }
}
