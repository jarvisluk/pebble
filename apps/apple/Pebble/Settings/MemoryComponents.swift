import SwiftUI

enum MemoryKindConstants {
    static let all = ["fact", "preference", "rule", "decision", "reference", "profile", "procedure", "episode"]

    static func parseTags(_ raw: String) -> [String] {
        raw.components(separatedBy: CharacterSet(charactersIn: ",，")) // i18n:allow-cjk
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
    }

    static func rtrim(_ s: String) -> String {
        s.replacingOccurrences(of: "\\s+$", with: "", options: .regularExpression)
    }
}

/// Compact pill button safe inside `HubFormStyle` (overrides `HubRowButtonStyle`).
struct MemoryChipButton: View {
    @Environment(\.palette) private var p
    var title: String
    var systemImage: String? = nil
    var bg: Color? = nil
    var fg: Color? = nil
    var disabled = false
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 4) {
                if let systemImage {
                    Image(systemName: systemImage)
                        .font(.system(size: AppFont.caption, weight: .medium))
                }
                Text(title)
                    .font(.system(size: AppFont.caption, weight: .medium))
                    .lineLimit(1)
            }
            .foregroundStyle(disabled ? p.mute : (fg ?? p.text2))
            .padding(.horizontal, 9)
            .padding(.vertical, 5)
            .background(bg ?? p.s1, in: Capsule())
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .disabled(disabled)
    }
}

/// Wrapping status filter pills (320pt narrow safe, never truncates).
struct MemoryStatusFilterBar: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var selection: String
    var statuses: [String]
    var candidateCount: Int
    var onSelect: (String) -> Void

    var body: some View {
        FlowLayout(spacing: 6) {
            ForEach(statuses, id: \.self) { st in
                let active = selection == st
                let isCandidate = st == "candidate"
                let label = statusLabel(st)
                let bg: Color = active ? p.accent : (isCandidate && candidateCount > 0 ? p.lilac : p.s1)
                let fg: Color = active ? p.card : (isCandidate && candidateCount > 0 ? p.lilacInk : p.text2)
                Button {
                    onSelect(st)
                } label: {
                    Text(label)
                        .font(.system(size: AppFont.caption, weight: active ? .semibold : .medium))
                        .foregroundStyle(fg)
                        .lineLimit(1)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 5)
                        .background(bg, in: Capsule())
                        .contentShape(Capsule())
                }
                .buttonStyle(.plain)
            }
        }
    }

    private func statusLabel(_ st: String) -> String {
        switch st {
        case "active": return hub.t("memStatusActive")
        case "candidate":
            return candidateCount > 0
                ? hub.t("memStatusCandidateCount", ["n": candidateCount])
                : hub.t("memStatusCandidate")
        case "superseded": return hub.t("memStatusSuperseded")
        case "archived": return hub.t("memStatusArchived")
        case "all": return hub.t("memStatusAll")
        default: return st
        }
    }
}

/// Collapsible `MEMORY.md` viewer and editor with line count indicator.
struct MemoryIndexEditorBlock: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var title: String
    var doc: MemoryIndexDoc?
    @Binding var draft: String
    @Binding var touched: Bool
    @Binding var expanded: Bool
    var loading: Bool
    var saving: Bool
    var defaultMaxLines: Int = 100
    var onReload: () -> Void
    var onSave: () -> Void

    private var maxLines: Int {
        let m = doc?.maxLines ?? defaultMaxLines
        return m > 0 ? m : defaultMaxLines
    }

    private var currentLines: Int {
        if !touched, let serverLines = doc?.lines {
            return serverLines
        }
        let trimmed = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty { return 0 }
        return draft.components(separatedBy: .newlines).count
    }

    private var isDirty: Bool {
        touched && MemoryKindConstants.rtrim(draft) != MemoryKindConstants.rtrim(doc?.content ?? "")
    }

    private var nearOrOverCap: Bool {
        currentLines >= maxLines
    }

    var body: some View {
        DisclosureGroup(isExpanded: $expanded) {
            VStack(alignment: .leading, spacing: 10) {
                if loading && doc == nil {
                    ProgressView().controlSize(.small)
                } else {
                    TextField(
                        title,
                        text: Binding(
                            get: { draft },
                            set: {
                                guard $0 != draft else { return }
                                draft = $0
                                touched = true
                            }
                        ),
                        prompt: Text(hub.t("memIndexPh")),
                        axis: .vertical
                    )
                    .lineLimit(5...18)
                    .monoInput()
                    .labelsHidden()
                    .padding(.horizontal, 10)
                    .padding(.vertical, 8)
                    .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))

                    if nearOrOverCap {
                        Text(hub.t("memIndexLimitHint"))
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(p.busy)
                            .fixedSize(horizontal: false, vertical: true)
                    }

                    if let path = doc?.path, !path.isEmpty {
                        Text(path)
                            .font(.system(size: AppFont.caption, design: .monospaced))
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .truncationMode(.middle)
                    }

                    FlowLayout(spacing: 8) {
                        MemoryChipButton(
                            title: hub.t("refresh"),
                            systemImage: "arrow.clockwise",
                            disabled: loading || saving,
                            action: onReload
                        )
                        if saving {
                            ProgressView().controlSize(.small)
                        } else {
                            Button(hub.t("save"), action: onSave)
                                .buttonStyle(HubPrimaryButtonStyle())
                                .disabled(!isDirty)
                        }
                    }
                }
            }
            .padding(.top, 8)
        } label: {
            HStack(spacing: 8) {
                Text(title)
                    .font(.system(size: AppFont.body, weight: .medium))
                    .foregroundStyle(p.text)
                    .lineLimit(1)
                Spacer(minLength: 6)
                Tag(
                    text: hub.t("memIndexLines", ["lines": currentLines, "max": maxLines]),
                    bg: nearOrOverCap ? p.pink : p.s1,
                    fg: nearOrOverCap ? p.pinkInk : p.text2
                )
                .fixedSize()
            }
        }
    }
}

/// Reusable vertical fields for creating or editing a memory item (320pt safe).
struct MemoryEditorFormFields: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    @Binding var title: String
    @Binding var kind: String
    @Binding var text: String
    var tags: Binding<String>? = nil
    var why: Binding<String>? = nil
    @Binding var pin: Bool
    var daily: Binding<Bool>? = nil
    var contentPlaceholder: String
    var toggleTint: Color? = nil

    private var isDaily: Bool { daily?.wrappedValue == true }

    var body: some View {
        let kindOptions = MemoryKindConstants.all.map { ($0, localizedMemoryKind($0, hub: hub)) }
        VStack(alignment: .leading, spacing: 10) {
            if let daily {
                Toggle(hub.t("memDailyLabel"), isOn: daily)
                    .tint(toggleTint ?? p.accent)
            }

            if !isDaily {
                HubMenuPicker(title: hub.t("memType"), selection: $kind, options: kindOptions)

                HubFormField(label: hub.t("memTitleLabel")) {
                    TextField(hub.t("memTitleLabel"), text: $title, prompt: Text(hub.t("memTitlePh")))
                        .padding(.horizontal, 10)
                        .padding(.vertical, 7)
                        .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                }
            }

            HubFormField(label: hub.t("memContentLabel")) {
                TextField(hub.t("memContentLabel"), text: $text, prompt: Text(contentPlaceholder), axis: .vertical)
                    .lineLimit(2...8)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 7)
                    .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
            }

            if !isDaily {
                if let why {
                    HubFormField(label: hub.t("memWhyLabel")) {
                        TextField(hub.t("memWhyLabel"), text: why, prompt: Text(hub.t("memWhyPh")), axis: .vertical)
                            .lineLimit(1...4)
                            .padding(.horizontal, 10)
                            .padding(.vertical, 7)
                            .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                    }
                }

                if let tags {
                    HubFormField(label: hub.t("tagsPh")) {
                        TextField(hub.t("tagsPh"), text: tags, prompt: Text(hub.t("tagsPh")))
                            .padding(.horizontal, 10)
                            .padding(.vertical, 7)
                            .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                    }
                }

                Toggle(hub.t("memPinLabel"), isOn: $pin)
                    .tint(toggleTint ?? p.accent)
            }
        }
    }
}
