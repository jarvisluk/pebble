import SwiftUI

struct StageNewTabView: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var botId: String
    var canUseFiles: Bool
    var inWideStage: Bool = false
    var onOpenFiles: () -> Void
    var onOpenDesktop: () -> Void
    var onOpenTask: (BotTask) -> Void

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

    private var launcherTasks: [BotTask] {
        let all = hub.tasks[botId] ?? []
        let active = all.filter(\.isActive)
        let done = all.filter(\.isTerminal)
        return Array((active + done).prefix(8))
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
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

                if !launcherTasks.isEmpty {
                    VStack(alignment: .leading, spacing: 8) {
                        HStack {
                            Text(hub.t("stageTasksSection"))
                                .font(.system(size: AppFont.caption, weight: .semibold))
                                .foregroundStyle(p.mute)
                            Spacer()
                            Text("\(launcherTasks.count)")
                                .font(.system(size: AppFont.caption, weight: .semibold))
                                .foregroundStyle(p.text2)
                                .padding(.horizontal, 7)
                                .padding(.vertical, 2)
                                .background(inWideStage ? p.s1 : p.card, in: Capsule())
                        }
                        .padding(.horizontal, 2)

                        VStack(spacing: 8) {
                            ForEach(launcherTasks) { task in
                                let vis = taskStatusVisual(task.status, palette: p)
                                let dev = taskDeviceLabel(task.deviceId, nodes: hub.nodes) ?? hub.t("taskDeviceLocal")
                                let meta = [dev, task.harness].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")

                                Button {
                                    onOpenTask(task)
                                } label: {
                                    HStack(spacing: 12) {
                                        Image(systemName: "terminal")
                                            .font(.system(size: AppFont.cardTitle, weight: .medium))
                                            .foregroundStyle(p.text)
                                            .frame(width: 34, height: 34)
                                            .background(inWideStage ? p.card : p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))

                                        VStack(alignment: .leading, spacing: 2) {
                                            Text(task.displayTitle)
                                                .font(.system(size: AppFont.body, weight: .semibold))
                                                .foregroundStyle(p.text)
                                                .lineLimit(1)
                                            if !meta.isEmpty {
                                                Text(meta)
                                                    .font(.system(size: AppFont.caption))
                                                    .foregroundStyle(p.mute)
                                                    .lineLimit(1)
                                            }
                                        }

                                        Spacer(minLength: 8)

                                        Tag(text: taskStatusText(task.status, hub: hub), bg: vis.bg, fg: vis.fg)
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
                }
            }
            .frame(maxWidth: 520, alignment: .leading)
            .padding(24)
            .frame(maxWidth: .infinity, minHeight: 360, alignment: .center)
        }
        .background(inWideStage ? p.card : p.page)
    }
}
