import SwiftUI

extension View {
    /// 原生 Form 套 DESIGN 页面底色与卡片圆角。
    func hubForm() -> some View { modifier(HubSurface(form: true)) }

    /// 窄宽度：insetGrouped + page 底。
    @ViewBuilder
    func hubColumnList(compact: Bool, page: Color) -> some View {
        #if os(iOS)
        if compact {
            self.listStyle(.insetGrouped).scrollContentBackground(.hidden).background(page)
        } else {
            self
        }
        #else
        self
        #endif
    }

    func monoInput() -> some View {
        self.font(.system(size: AppFont.secondary, design: .monospaced))
            .autocorrectionDisabled()
            .noAutocap()
    }

    @ViewBuilder
    func hubFlushFormRow() -> some View {
        if #available(iOS 18.0, macOS 15.0, *) {
            self.containerValue(\.hubFlushRow, true)
        } else {
            self.listRowInsets(EdgeInsets())
        }
    }
}

/// 跨平台表单上下结构输入行（标签在输入框上方，避免窄屏折行并给足输入宽度）。
struct HubFormField<Field: View>: View {
    @Environment(\.palette) private var p
    var label: String
    @ViewBuilder var field: () -> Field

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(label)
                .font(.system(size: AppFont.caption, weight: .medium))
                .foregroundStyle(p.mute)
            field()
                .labelsHidden()
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

struct HubLabeledContentStyle: LabeledContentStyle {
    @Environment(\.palette) private var p

    func makeBody(configuration: Configuration) -> some View {
        HStack(alignment: .center, spacing: 12) {
            configuration.label
                .foregroundStyle(p.text)
            Spacer(minLength: 12)
            configuration.content
                .foregroundStyle(p.mute)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct HubSwitchToggleStyle: ToggleStyle {
    @Environment(\.palette) private var p

    func makeBody(configuration: Configuration) -> some View {
        HStack(alignment: .center, spacing: 12) {
            configuration.label
                .foregroundStyle(p.text)
            Spacer(minLength: 12)
            Toggle("", isOn: configuration.$isOn)
                .labelsHidden()
                .toggleStyle(.switch)
                .controlSize(platformSwitchControlSize)
        }
    }
}

struct HubRowButtonStyle: ButtonStyle {
    @Environment(\.palette) private var p
    @Environment(\.isEnabled) private var isEnabled
    @State private var hovering = false

    func makeBody(configuration: Configuration) -> some View {
        let fg: Color = configuration.role == .destructive ? p.danger : (isEnabled ? p.text : p.mute)
        HStack(spacing: 8) {
            configuration.label
            Spacer(minLength: 8)
        }
        .foregroundStyle(fg)
        .padding(.vertical, 2)
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(configuration.isPressed ? Color.primary.opacity(0.08) : (hovering ? Color.primary.opacity(0.04) : .clear))
                .padding(.horizontal, -8)
                .padding(.vertical, -4)
        )
        .contentShape(Rectangle())
        .onHover { hovering = $0 }
    }
}

@available(iOS 18.0, macOS 15.0, *)
private struct HubFlushRowKey: ContainerValueKey {
    static let defaultValue = false
}

@available(iOS 18.0, macOS 15.0, *)
extension ContainerValues {
    var hubFlushRow: Bool {
        get { self[HubFlushRowKey.self] }
        set { self[HubFlushRowKey.self] = newValue }
    }
}

@available(iOS 18.0, macOS 15.0, *)
struct HubFormStyle: FormStyle {
    @Environment(\.palette) private var p

    func makeBody(configuration: Configuration) -> some View {
        let cardFill = p.dark ? Color(hex: "#1c1c1e") : p.card
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                Group(sections: configuration.content) { sections in
                    ForEach(sections) { section in
                        VStack(alignment: .leading, spacing: 6) {
                            if !section.header.isEmpty {
                                section.header
                                    .font(.system(size: AppFont.secondary, weight: .regular))
                                    .foregroundStyle(p.mute)
                                    .padding(.horizontal, 16)
                            }
                            if !section.content.isEmpty {
                                Group(subviews: section.content) { subviews in
                                    VStack(alignment: .leading, spacing: 0) {
                                        ForEach(Array(subviews.enumerated()), id: \.element.id) { idx, subview in
                                            let flush = subview.containerValues.hubFlushRow
                                            if idx > 0 {
                                                Rectangle()
                                                    .fill(Color.primary.opacity(p.dark ? 0.10 : 0.08))
                                                    .frame(height: 0.5)
                                                    .padding(.leading, flush ? 0 : 16)
                                            }
                                            subview
                                                .frame(maxWidth: .infinity, alignment: .leading)
                                                .padding(.horizontal, flush ? 0 : 16)
                                                .padding(.vertical, flush ? 0 : 12)
                                        }
                                    }
                                    .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
                                    .background(cardFill, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
                                    .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
                                }
                            }
                            if !section.footer.isEmpty {
                                section.footer
                                    .font(.system(size: AppFont.caption))
                                    .foregroundStyle(p.mute)
                                    .padding(.horizontal, 16)
                            }
                        }
                    }
                }
            }
            .padding(.horizontal, 16)
            .padding(.top, 8)
            .padding(.bottom, 24)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

struct HubSurface: ViewModifier {
    @Environment(\.palette) private var p
    var form: Bool

    func body(content: Content) -> some View {
        if #available(iOS 18.0, macOS 15.0, *) {
            content
                .buttonStyle(HubRowButtonStyle())
                .labeledContentStyle(HubLabeledContentStyle())
                .toggleStyle(HubSwitchToggleStyle())
                .textFieldStyle(.plain)
                .formStyle(HubFormStyle())
                .scrollDismissesKeyboard(.interactively)
                .background(p.page.ignoresSafeArea())
                .font(.system(size: AppFont.body))
        } else {
            content
                .formStyle(.grouped)
                .scrollContentBackground(.hidden)
                .background(p.page.ignoresSafeArea())
                .font(.system(size: AppFont.body))
        }
    }
}
