import SwiftUI

struct StageTabBar: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var tabs: [StageTab]
    var activeTabId: String
    var onSelect: (String) -> Void
    var onClose: (String) -> Void
    var onNewTab: () -> Void

    var body: some View {
        HStack(spacing: 6) {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 4) {
                    ForEach(tabs) { tab in
                        let active = tab.id == activeTabId
                        HStack(spacing: 5) {
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
                                .padding(.trailing, tab.closable ? 4 : 10)
                                .padding(.vertical, 6)
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)

                            if tab.closable {
                                Button {
                                    onClose(tab.id)
                                } label: {
                                    Image(systemName: "xmark")
                                        .font(.system(size: AppFont.caption - 2, weight: .semibold))
                                        .foregroundStyle(p.mute)
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
                            RoundedRectangle(cornerRadius: 10, style: .continuous)
                                .fill(active ? p.card : Color.clear)
                        )
                        .overlay(
                            RoundedRectangle(cornerRadius: 10, style: .continuous)
                                .strokeBorder(active ? p.s2 : Color.clear, lineWidth: 0.5)
                        )
                    }

                    Button {
                        onNewTab()
                    } label: {
                        Image(systemName: "plus")
                            .font(.system(size: AppFont.secondary, weight: .semibold))
                            .foregroundStyle(p.text2)
                            .frame(width: 28, height: 28)
                            .background(p.s1.opacity(0.6), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                    }
                    .buttonStyle(.plain)
                    .help(hub.t("stageOpenNewTab"))
                    .accessibilityLabel(hub.t("stageOpenNewTab"))
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
            }
        }
        .background(p.page)
        .overlay(alignment: .bottom) {
            Rectangle().fill(p.s2).frame(height: 0.5)
        }
    }
}
