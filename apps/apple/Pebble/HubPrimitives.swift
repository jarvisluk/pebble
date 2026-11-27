import SwiftUI

// MARK: 通用小件

struct PressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(.easeOut(duration: 0.15), value: configuration.isPressed)
    }
}

struct Tag: View {
    var text: String
    var bg: Color
    var fg: Color
    var body: some View {
        Text(text)
            .font(.system(size: AppFont.secondary, weight: .semibold))
            .foregroundStyle(fg)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(bg, in: Capsule())
    }
}

/// 表单内的错误 / 成功提示。
struct StatusText: View {
    @Environment(\.palette) private var p
    var text: String
    var error = true
    var body: some View {
        Label(text, systemImage: error ? "exclamationmark.triangle.fill" : "checkmark.circle.fill")
            .font(.system(size: AppFont.body))
            .foregroundStyle(error ? p.danger : p.sageInk)
    }
}

struct ConfirmItem: Identifiable {
    let id = UUID()
    var title: String
    var message: String
    var destructive = true
    var action: () async -> Void
}

extension View {
    func confirmAlert(_ item: Binding<ConfirmItem?>) -> some View {
        alert(item.wrappedValue?.title ?? "", isPresented: Binding(get: { item.wrappedValue != nil }, set: { if !$0 { item.wrappedValue = nil } }),
              presenting: item.wrappedValue) { c in
            Button(L.t("cancel"), role: .cancel) {}
            Button(L.t("confirm"), role: c.destructive ? .destructive : nil) { Task { await c.action() } }
        } message: { c in Text(c.message) }
    }
}

/// 简单的换行流式布局。
struct FlowLayout: Layout {
    var spacing: CGFloat = 8

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let maxW = proposal.width ?? .infinity
        var x: CGFloat = 0, y: CGFloat = 0, rowH: CGFloat = 0, width: CGFloat = 0
        for v in subviews {
            let s = v.sizeThatFits(.unspecified)
            if x > 0, x + s.width > maxW { x = 0; y += rowH + spacing; rowH = 0 }
            x += s.width + spacing
            rowH = max(rowH, s.height)
            width = max(width, x - spacing)
        }
        return CGSize(width: proposal.width ?? width, height: y + rowH)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, rowH: CGFloat = 0
        for v in subviews {
            let s = v.sizeThatFits(.unspecified)
            if x > bounds.minX, x + s.width > bounds.maxX { x = bounds.minX; y += rowH + spacing; rowH = 0 }
            v.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(s))
            x += s.width + spacing
            rowH = max(rowH, s.height)
        }
    }
}

/// 跨平台分段选择器：iOS 与 macOS 共用同一套全宽胶囊分段样式。
struct HubSegmentedPicker<Selection: Hashable>: View {
    @Environment(\.palette) private var p
    @Binding var selection: Selection
    var options: [(Selection, String)]

    var body: some View {
        HStack(spacing: 2) {
            ForEach(Array(options.enumerated()), id: \.offset) { _, opt in
                let active = selection == opt.0
                Button {
                    withAnimation(.easeInOut(duration: 0.16)) {
                        selection = opt.0
                    }
                } label: {
                    Text(opt.1)
                        .font(.system(size: AppFont.secondary, weight: active ? .semibold : .regular))
                        .foregroundStyle(active ? p.text : p.text2)
                        .lineLimit(1)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 6)
                        .background {
                            if active {
                                Capsule()
                                    .fill(p.dark ? Color(hex: "#525256") : Color.white)
                                    .shadow(color: p.dark ? .clear : Color.black.opacity(0.08), radius: 2, y: 1)
                            }
                        }
                        .contentShape(Capsule())
                }
                .buttonStyle(.plain)
            }
        }
        .padding(3)
        .background(Capsule().fill(p.dark ? Color.white.opacity(0.09) : Color.black.opacity(0.07)))
        .frame(maxWidth: .infinity)
    }
}

/// 跨平台表单弹出选择器：两端统一 `标签 + 当前值 ⌄` 无边框行样式。
struct HubMenuPicker<Selection: Hashable>: View {
    @Environment(\.palette) private var p
    var title: String
    @Binding var selection: Selection
    var options: [(Selection, String)]

    private var currentLabel: String {
        options.first(where: { $0.0 == selection })?.1 ?? ""
    }

    var body: some View {
        LabeledContent(title) {
            Menu {
                ForEach(Array(options.enumerated()), id: \.offset) { _, opt in
                    Button {
                        selection = opt.0
                    } label: {
                        if selection == opt.0 {
                            Label(opt.1, systemImage: "checkmark")
                        } else {
                            Text(opt.1)
                        }
                    }
                }
            } label: {
                (Text(currentLabel).foregroundStyle(p.text2)
                    + Text(" ")
                    + Text(Image(systemName: "chevron.up.chevron.down"))
                        .font(.system(size: AppFont.caption - 1, weight: .medium))
                        .foregroundStyle(p.mute))
            }
            .buttonStyle(.plain)
            .hubBorderlessMenu()
            .menuIndicator(.hidden)
            .fixedSize()
        }
    }
}

/// 跨平台无缝弹窗与 Inspector 顶栏外壳（消除 macOS 模态上下灰条分叉）。
struct HubSheetScaffold<Content: View>: View {
    @Environment(\.palette) private var p
    var title: String
    var closeTitle: String = L.t("close")
    var onClose: (() -> Void)? = nil
    var enableCancelShortcut = true
    var actionTitle: String? = nil
    var actionDisabled = false
    var loading = false
    var onAction: (() -> Void)? = nil
    @ViewBuilder var content: () -> Content

    var body: some View {
        VStack(spacing: 0) {
            ZStack {
                Text(title)
                    .font(.system(size: AppFont.cardTitle, weight: .semibold))
                    .foregroundStyle(p.text)
                    .lineLimit(1)
                    .padding(.horizontal, 92)

                HStack {
                    if let onClose {
                        let closeBtn = Button(closeTitle, action: onClose)
                            .font(.system(size: AppFont.body))
                            .foregroundStyle(p.text2)
                            .buttonStyle(.plain)
                            .padding(.horizontal, 12)
                            .padding(.vertical, 6)
                            .background(p.s1, in: Capsule())
                        if enableCancelShortcut {
                            closeBtn.keyboardShortcut(.cancelAction)
                        } else {
                            closeBtn
                        }
                    }
                    Spacer()
                    if let actionTitle, let onAction {
                        Button(action: onAction) {
                            HubLoadingLabel(loading: loading) { Text(actionTitle) }
                        }
                        .font(.system(size: AppFont.body, weight: .semibold))
                        .foregroundStyle(actionDisabled ? p.mute : p.card)
                        .buttonStyle(.plain)
                        .padding(.horizontal, 14)
                        .padding(.vertical, 6)
                        .background(Capsule().fill(actionDisabled ? p.s1 : p.accent))
                        .disabled(actionDisabled || loading)
                    }
                }
            }
            .padding(.horizontal, 16)
            .padding(.top, 14)
            .padding(.bottom, 10)

            content()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .background(p.page.ignoresSafeArea())
    }
}

/// 跨平台胶囊搜索框（支持 macOS `Cmd+K` 聚焦通知）。
struct HubSearchBar: View {
    @Environment(\.palette) private var p
    @Binding var text: String
    var placeholder: String
    @FocusState private var isFocused: Bool

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: AppFont.secondary))
                .foregroundStyle(.secondary)
            TextField(placeholder, text: $text)
                .textFieldStyle(.plain)
                .font(.system(size: AppFont.secondary))
                .autocorrectionDisabled()
                .noAutocap()
                .focused($isFocused)
            if !text.isEmpty {
                Button {
                    text = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.horizontal, 10)
        .frame(height: 32)
        .background(Color.primary.opacity(p.dark ? 0.12 : 0.07), in: Capsule())
        .onReceive(NotificationCenter.default.publisher(for: .hubFocusSearch)) { _ in
            isFocused = true
        }
    }
}

/// 跨平台高对比度主操作胶囊按钮（深色白底黑字 / 浅色黑底白字）。
/// 按钮加载态：保留原按钮与标题占位，仅隐藏标题并原位叠加转圈。
/// 禁止把按钮整体替换为裸 `ProgressView`，否则胶囊 / 工具栏项会塌陷变形、宽度跳动。
struct HubLoadingLabel<Label: View>: View {
    var loading: Bool
    @ViewBuilder var label: () -> Label

    var body: some View {
        label()
            .opacity(loading ? 0 : 1)
            .overlay {
                if loading {
                    ProgressView().controlSize(.mini)
                }
            }
    }
}

struct HubPrimaryButtonStyle: ButtonStyle {
    @Environment(\.palette) private var p
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: AppFont.secondary, weight: .semibold))
            .foregroundStyle(isEnabled ? p.card : p.mute)
            .padding(.horizontal, 12)
            .padding(.vertical, 5)
            .background(Capsule().fill(isEnabled ? p.accent : p.s1))
            .opacity(configuration.isPressed ? 0.82 : 1)
            .contentShape(Capsule())
    }
}
