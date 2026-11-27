import SwiftUI

/// Memory entry row with metadata badges, inline actions, context menu, and swipe actions.
struct MemoryRow: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var item: MemoryItem
    var showTags = false
    var showScope = false
    var busy = false
    var onTogglePin: (() -> Void)? = nil
    var onApprove: (() -> Void)? = nil
    var onDismiss: (() -> Void)? = nil
    var onEdit: (() -> Void)? = nil
    var onArchive: (() -> Void)? = nil
    var onRestore: (() -> Void)? = nil
    var onDelete: () -> Void

    private var isSessionHit: Bool { item.origin == "session" }
    private var status: String { item.effectiveStatus }
    private var isMutedStatus: Bool { status == "archived" || status == "superseded" }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            metadataBar

            if let title = item.title?.trimmingCharacters(in: .whitespacesAndNewlines), !title.isEmpty {
                Text(title)
                    .font(.system(size: AppFont.body, weight: .semibold))
                    .foregroundStyle(isMutedStatus ? p.text2 : p.text)
                    .fixedSize(horizontal: false, vertical: true)
            }

            Text(item.displayBody)
                .font(.system(size: AppFont.body))
                .foregroundStyle(isMutedStatus ? p.text2 : p.text)
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)

            if let why = item.why?.trimmingCharacters(in: .whitespacesAndNewlines), !why.isEmpty {
                Text(hub.t("memWhyPrefix") + why)
                    .font(.system(size: AppFont.secondary))
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }

            if showTags && !item.visibleTags.isEmpty {
                FlowLayout(spacing: 6) {
                    ForEach(item.visibleTags, id: \.self) { tag in
                        Text("#\(tag)")
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(.secondary)
                    }
                }
            }

            if !isSessionHit {
                actionBar
            }
        }
        .swipeActions {
            if !isSessionHit {
                Button(hub.t("delete"), role: .destructive, action: onDelete)
                if status == "candidate", let onApprove {
                    Button(hub.t("memApprove"), action: onApprove)
                        .tint(p.sageInk)
                }
            }
        }
        .contextMenu {
            if !isSessionHit {
                if status == "candidate" {
                    if let onApprove {
                        Button(hub.t("memApprove"), systemImage: "checkmark", action: onApprove)
                    }
                    if let onDismiss {
                        Button(hub.t("memDismiss"), systemImage: "xmark", role: .destructive, action: onDismiss)
                    }
                }
                if let onTogglePin {
                    Button(
                        hub.t(item.isPinned ? "memUnpin" : "memPin"),
                        systemImage: item.isPinned ? "pin.slash" : "pin",
                        action: onTogglePin
                    )
                }
                if let onEdit {
                    Button(hub.t("edit"), systemImage: "pencil", action: onEdit)
                }
                if status == "active", let onArchive {
                    Button(hub.t("memArchive"), systemImage: "archivebox", action: onArchive)
                }
                if isMutedStatus, let onRestore {
                    Button(hub.t("memRestore"), systemImage: "arrow.uturn.backward", action: onRestore)
                }
                Button(hub.t("delete"), systemImage: "trash", role: .destructive, action: onDelete)
            }
        }
    }

    private var metadataBar: some View {
        FlowLayout(spacing: 6) {
            if isSessionHit {
                badge(hub.t("memOriginSession"), bg: p.s1, fg: p.text2)
            } else {
                Text(localizedMemoryKind(item.kind, hub: hub))
                    .font(.system(size: AppFont.caption, weight: .semibold))
                    .foregroundStyle(isMutedStatus ? p.mute : p.sageInk)
            }

            if item.isPinned {
                HStack(spacing: 3) {
                    Image(systemName: "pin.fill")
                    Text(hub.t("memPinnedTag"))
                }
                .font(.system(size: AppFont.caption, weight: .semibold))
                .foregroundStyle(p.text)
            }

            if status != "active" {
                let isCand = status == "candidate"
                badge(
                    statusBadgeText(status),
                    bg: isCand ? p.lilac : p.s1,
                    fg: isCand ? p.lilacInk : p.mute
                )
            }

            if showScope, let sc = item.scope, !sc.isEmpty {
                let scopeText: String = {
                    if sc == "everywhere" || sc == "global" {
                        return hub.t("memScopeGlobal")
                    }
                    if let bid = item.botId, !bid.isEmpty {
                        return "@\(bid)"
                    }
                    return hub.t("memScopeBot")
                }()
                badge(scopeText, bg: p.s1, fg: p.text2)
            }
        }
    }

    private var actionBar: some View {
        FlowLayout(spacing: 6) {
            if status == "candidate" {
                if let onApprove {
                    MemoryChipButton(
                        title: hub.t("memApprove"),
                        systemImage: "checkmark",
                        bg: p.sage,
                        fg: p.sageInk,
                        disabled: busy,
                        action: onApprove
                    )
                }
                if let onDismiss {
                    MemoryChipButton(
                        title: hub.t("memDismiss"),
                        systemImage: "xmark",
                        bg: p.pink,
                        fg: p.pinkInk,
                        disabled: busy,
                        action: onDismiss
                    )
                }
            }

            if let onTogglePin {
                MemoryChipButton(
                    title: hub.t(item.isPinned ? "memUnpin" : "memPin"),
                    systemImage: item.isPinned ? "pin.fill" : "pin",
                    bg: item.isPinned ? p.accentSoft : p.s1,
                    fg: item.isPinned ? p.text : p.text2,
                    disabled: busy,
                    action: onTogglePin
                )
            }

            if let onEdit {
                MemoryChipButton(
                    title: hub.t("edit"),
                    systemImage: "pencil",
                    disabled: busy,
                    action: onEdit
                )
            }

            if status == "active", let onArchive {
                MemoryChipButton(
                    title: hub.t("memArchive"),
                    systemImage: "archivebox",
                    disabled: busy,
                    action: onArchive
                )
            }

            if isMutedStatus, let onRestore {
                MemoryChipButton(
                    title: hub.t("memRestore"),
                    systemImage: "arrow.uturn.backward",
                    bg: p.sage,
                    fg: p.sageInk,
                    disabled: busy,
                    action: onRestore
                )
            }

            MemoryChipButton(
                title: hub.t("delete"),
                systemImage: "trash",
                fg: p.danger,
                disabled: busy,
                action: onDelete
            )
        }
        .padding(.top, 2)
    }

    private func badge(_ text: String, bg: Color, fg: Color) -> some View {
        Text(text)
            .font(.system(size: AppFont.caption, weight: .medium))
            .foregroundStyle(fg)
            .padding(.horizontal, 7)
            .padding(.vertical, 2)
            .background(bg, in: Capsule())
    }

    private func statusBadgeText(_ s: String) -> String {
        switch s {
        case "candidate": return hub.t("memStatusCandidate")
        case "superseded": return hub.t("memStatusSuperseded")
        case "archived": return hub.t("memStatusArchived")
        default: return hub.t("memStatusActive")
        }
    }
}
