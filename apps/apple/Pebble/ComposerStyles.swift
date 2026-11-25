import SwiftUI

#if os(macOS)
let composerControlHeight: CGFloat = 36
#else
let composerControlHeight: CGFloat = 44
#endif
let composerFieldCornerRadius: CGFloat = composerControlHeight / 2

extension View {
    @ViewBuilder
    func composerGlassButton(onCard: Bool = false) -> some View {
        #if os(iOS)
        self.modifier(IOSComposerCircleModifier(onCard: onCard))
        #else
        self.buttonStyle(MacComposerCircleButtonStyle(onCard: onCard))
        #endif
    }

    @ViewBuilder
    func composerGlassField(onCard: Bool = false) -> some View {
        #if os(iOS)
        self.modifier(IOSComposerFieldModifier(onCard: onCard))
        #else
        self.modifier(MacComposerFieldModifier(onCard: onCard))
        #endif
    }

    @ViewBuilder
    func composerPermissionMenu(isPlan: Bool, onCard: Bool = false) -> some View {
        #if os(iOS)
        self.modifier(IOSComposerPillModifier(isPlan: isPlan, onCard: onCard))
        #else
        self.menuStyle(.button)
            .buttonStyle(MacComposerPillButtonStyle(isPlan: isPlan, onCard: onCard))
            .menuIndicator(.hidden)
            .fixedSize()
        #endif
    }

    @ViewBuilder
    func composerSendButton(accent: Color) -> some View {
        #if os(iOS)
        self.buttonStyle(IOSComposerSendButtonStyle(accent: accent))
        #else
        self.buttonStyle(MacComposerSendButtonStyle(accent: accent))
        #endif
    }
}

#if os(macOS)
private struct MacComposerCircleButtonStyle: ButtonStyle {
    @Environment(\.palette) private var p
    @Environment(\.isEnabled) private var isEnabled
    @State private var hovering = false
    var onCard: Bool = false

    func makeBody(configuration: Configuration) -> some View {
        let floatFill = p.dark ? p.card : Color(hex: "#ffffff")
        let idleFill = onCard ? p.s1 : floatFill
        let hoverFill = onCard ? p.s2 : p.s1
        configuration.label
            .frame(width: composerControlHeight, height: composerControlHeight)
            .foregroundStyle(isEnabled ? p.text : p.mute)
            .background(
                Circle().fill(configuration.isPressed ? p.s2 : (hovering ? hoverFill : idleFill))
            )
            .overlay(Circle().stroke(p.s2, lineWidth: 1))
            .contentShape(Circle())
            .onHover { hovering = $0 }
    }
}

private struct MacComposerPillButtonStyle: ButtonStyle {
    @Environment(\.palette) private var p
    @Environment(\.isEnabled) private var isEnabled
    @State private var hovering = false
    var isPlan: Bool = false
    var onCard: Bool = false

    func makeBody(configuration: Configuration) -> some View {
        let floatFill = p.dark ? p.card : Color(hex: "#ffffff")
        let idleFill = isPlan ? p.lilac : (onCard ? p.s1 : floatFill)
        let hoverFill = isPlan ? p.lilac : (onCard ? p.s2 : p.s1)
        let pressFill = isPlan ? p.lilac.opacity(0.85) : p.s2
        let fg = isPlan ? p.lilacInk : (isEnabled ? p.text2 : p.mute)
        let stroke = isPlan ? p.lilacInk.opacity(0.4) : p.s2
        configuration.label
            .foregroundStyle(fg)
            .padding(.horizontal, 12)
            .frame(height: composerControlHeight)
            .background(
                Capsule().fill(configuration.isPressed ? pressFill : (hovering ? hoverFill : idleFill))
            )
            .overlay(Capsule().stroke(stroke, lineWidth: 1))
            .contentShape(Capsule())
            .onHover { hovering = $0 }
    }
}

private struct MacComposerFieldModifier: ViewModifier {
    @Environment(\.palette) private var p
    var onCard: Bool = false

    func body(content: Content) -> some View {
        let shape = RoundedRectangle(cornerRadius: composerFieldCornerRadius, style: .continuous)
        let floatFill = p.dark ? p.card : Color(hex: "#ffffff")
        content
            .frame(minHeight: composerControlHeight, alignment: .center)
            .background(onCard ? p.s1 : floatFill, in: shape)
            .overlay(shape.stroke(p.s2, lineWidth: 1))
    }
}

private struct MacComposerSendButtonStyle: ButtonStyle {
    @Environment(\.palette) private var p
    @Environment(\.isEnabled) private var isEnabled
    var accent: Color

    func makeBody(configuration: Configuration) -> some View {
        let fg: Color = isEnabled ? contrastText(colorToHex(accent) ?? "#171717") : p.mute
        let bg: Color = isEnabled ? (configuration.isPressed ? accent.opacity(0.82) : accent) : p.s2
        configuration.label
            .frame(width: composerControlHeight, height: composerControlHeight)
            .foregroundStyle(fg)
            .background(Circle().fill(bg))
            .contentShape(Circle())
    }
}

struct HubMacTextFieldStyle: TextFieldStyle {
    @Environment(\.palette) private var p

    func _body(configuration: TextField<Self._Label>) -> some View {
        configuration
            .textFieldStyle(.plain)
            .font(.system(size: AppFont.body))
            .foregroundStyle(p.text)
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(p.s1, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).stroke(p.s2, lineWidth: 1))
    }
}
#else
private struct IOSComposerCircleModifier: ViewModifier {
    @Environment(\.palette) private var p
    var onCard: Bool = false

    func body(content: Content) -> some View {
        let styled = content
            .buttonStyle(.plain)
            .foregroundStyle(p.text)
            .frame(width: composerControlHeight, height: composerControlHeight)
            .contentShape(Circle())
        if #available(iOS 26.0, *) {
            styled.glassEffect(.regular.interactive(), in: Circle())
        } else {
            styled
                .background(onCard ? AnyShapeStyle(p.s1) : AnyShapeStyle(.regularMaterial), in: Circle())
                .overlay(Circle().stroke(p.s2, lineWidth: 0.5))
        }
    }
}

private struct IOSComposerFieldModifier: ViewModifier {
    @Environment(\.palette) private var p
    var onCard: Bool = false

    func body(content: Content) -> some View {
        let shape = RoundedRectangle(cornerRadius: composerFieldCornerRadius, style: .continuous)
        let sized = content.frame(minHeight: composerControlHeight, alignment: .center)
        if #available(iOS 26.0, *) {
            sized.glassEffect(.regular.interactive(), in: shape)
        } else {
            sized
                .background(onCard ? AnyShapeStyle(p.s1) : AnyShapeStyle(.regularMaterial), in: shape)
                .overlay(shape.stroke(p.s2, lineWidth: 0.5))
        }
    }
}

private struct IOSComposerPillModifier: ViewModifier {
    @Environment(\.palette) private var p
    var isPlan: Bool = false
    var onCard: Bool = false

    func body(content: Content) -> some View {
        let styled = content
            .buttonStyle(.plain)
            .foregroundStyle(isPlan ? p.lilacInk : p.text2)
            .padding(.horizontal, 12)
            .frame(height: composerControlHeight)
            .contentShape(Capsule())
        Group {
            if #available(iOS 26.0, *) {
                styled
                    .glassEffect(isPlan ? .regular.tint(p.lilac).interactive() : .regular.interactive(), in: Capsule())
                    .overlay(Capsule().stroke(isPlan ? p.lilacInk.opacity(0.4) : .clear, lineWidth: 0.5))
            } else {
                styled
                    .background(isPlan ? AnyShapeStyle(p.lilac) : (onCard ? AnyShapeStyle(p.s1) : AnyShapeStyle(.regularMaterial)), in: Capsule())
                    .overlay(Capsule().stroke(isPlan ? p.lilacInk.opacity(0.4) : p.s2, lineWidth: 0.5))
            }
        }
        .fixedSize()
    }
}

private struct IOSComposerSendButtonStyle: ButtonStyle {
    @Environment(\.palette) private var p
    @Environment(\.isEnabled) private var isEnabled
    var accent: Color

    func makeBody(configuration: Configuration) -> some View {
        let fg: Color = isEnabled ? contrastText(colorToHex(accent) ?? "#171717") : p.mute
        let bg: Color = isEnabled ? (configuration.isPressed ? accent.opacity(0.82) : accent) : p.s2
        let base = configuration.label
            .frame(width: composerControlHeight, height: composerControlHeight)
            .foregroundStyle(fg)
            .contentShape(Circle())
        if #available(iOS 26.0, *) {
            if isEnabled {
                base.glassEffect(.regular.tint(bg).interactive(), in: Circle())
            } else {
                base.background(Circle().fill(p.s2))
            }
        } else {
            base.background(Circle().fill(bg))
        }
    }
}
#endif
