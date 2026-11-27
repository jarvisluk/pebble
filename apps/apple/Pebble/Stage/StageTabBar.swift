import SwiftUI

struct StageTabBar: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var tabs: [StageTab]
    var activeTabId: String
    var inCard: Bool = false
    var onSelect: (String) -> Void
    var onClose: (String) -> Void
    var onNewTab: () -> Void

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 2) {
                ForEach(Array(tabs.enumerated()), id: \.element.id) { idx, tab in
                    let active = tab.id == activeTabId
                    let nextActive = idx + 1 < tabs.count && tabs[idx + 1].id == activeTabId

                    HStack(spacing: 4) {
                        Button {
                            onSelect(tab.id)
                        } label: {
                            HStack(spacing: 6) {
                                Image(systemName: StageStateOps.symbolName(for: tab.kind, rawKind: tab.rawKind))
                                    .font(.system(size: AppFont.caption, weight: .medium))
                                    .foregroundStyle(active ? p.text : p.text2)
                                Text(tab.title)
                                    .font(.system(size: AppFont.secondary, weight: active ? .semibold : .medium))
                                    .foregroundStyle(active ? p.text : p.text2)
                                    .lineLimit(1)
                            }
                            .padding(.leading, 10)
                            .padding(.trailing, tab.closable ? 2 : 10)
                            .padding(.vertical, 5)
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)

                        if tab.closable {
                            Button {
                                onClose(tab.id)
                            } label: {
                                Image(systemName: "xmark")
                                    .font(.system(size: AppFont.caption - 2, weight: .semibold))
                                    .foregroundStyle(p.mute.opacity(active ? 0.9 : 0.6))
                                    .frame(width: 20, height: 20)
                                    .contentShape(Circle())
                            }
                            .buttonStyle(.plain)
                            .padding(.trailing, 4)
                            .accessibilityLabel(hub.t("stageCloseTab"))
                        }
                    }
                    .frame(maxWidth: 210)
                    .background(
                        RoundedRectangle(cornerRadius: 8, style: .continuous)
                            .fill(active ? p.s1 : Color.clear)
                    )

                    if idx < tabs.count - 1 && !active && !nextActive {
                        Rectangle()
                            .fill(p.s2)
                            .frame(width: 0.5, height: 14)
                            .padding(.horizontal, 2)
                    }
                }

                Button {
                    onNewTab()
                } label: {
                    Image(systemName: "plus")
                        .font(.system(size: AppFont.secondary, weight: .medium))
                        .foregroundStyle(p.text2)
                        .frame(width: 28, height: 28)
                        .contentShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                }
                .buttonStyle(.plain)
                .padding(.leading, 2)
                .help(hub.t("stageOpenNewTab"))
                .accessibilityLabel(hub.t("stageOpenNewTab"))
            }
            .padding(.horizontal, 12)
        }
        .frame(height: 44)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(inCard ? p.card : p.page)
        .overlay(alignment: .bottom) {
            Rectangle().fill(p.s2).frame(height: 0.5)
        }
    }
}
