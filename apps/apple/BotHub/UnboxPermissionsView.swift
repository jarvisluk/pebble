import SwiftUI

struct UnboxPermissionsView: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    @Environment(\.hubCompact) private var compact
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.hubWindowVisible) private var windowVisible

    var onBack: (() -> Void)? = nil
    var onDone: () -> Void

    private let perms = PermissionsManager.shared
    @State private var remoteDetailsOpen = false
    @State private var advancedOpen = false

    private func locText(_ key: String, fallback: String) -> String {
        let v = hub.t(key)
        return v == key ? fallback : v
    }

    private func statusBadge(_ status: UnboxPermStatus) -> (text: String, bg: Color, fg: Color) {
        switch status {
        case .granted:
            return (hub.t("ubPermEnabled"), p.sage, p.sageInk)
        case .requested:
            return (hub.t("ubPermRequested"), p.sage, p.sageInk)
        case .unsupported:
            return (hub.t("deskPermUnsupported"), p.s1, p.mute)
        case .denied, .notDetermined:
            return (hub.t("ubPermNotEnabled"), p.s1, p.text2)
        }
    }

    var body: some View {
        let maxW: CGFloat = compact ? 420 : 620
        VStack(spacing: 0) {
            if let onBack {
                HStack {
                    Button(hub.t("backBtn"), action: onBack)
                        .font(.system(size: AppFont.body, weight: .medium))
                        .foregroundStyle(p.text2)
                        .buttonStyle(.plain)
                    Spacer()
                }
                .padding(.horizontal, 20)
                .padding(.top, 14)
                .padding(.bottom, 6)
            }

            ScrollView {
                VStack(spacing: 20) {
                    VStack(spacing: 10) {
                        BotAvatar(
                            id: "assistant",
                            size: compact ? 72 : 64,
                            color: pebbleColor,
                            busy: perms.requesting
                        )
                        Text(hub.t("ubPermTitle"))
                            .font(.system(size: AppFont.hero, weight: .bold))
                            .foregroundStyle(p.text)
                            .multilineTextAlignment(.center)
                        Text(hub.t(perms.subtitleKey))
                            .font(.system(size: AppFont.secondary))
                            .foregroundStyle(p.text2)
                            .multilineTextAlignment(.center)
                    }
                    .padding(.top, onBack == nil ? 24 : 8)

                    if perms.items.isEmpty && perms.loading {
                        ProgressView()
                            .padding(.vertical, 32)
                    } else {
                        if !perms.essentialItems.isEmpty {
                            VStack(alignment: .leading, spacing: 8) {
                                HStack(spacing: 8) {
                                    Text(hub.t("ubPermGroupEssentials"))
                                        .font(.system(size: AppFont.secondary, weight: .semibold))
                                        .foregroundStyle(p.text2)
                                    Spacer()
                                    Button {
                                        Task { await perms.refresh() }
                                    } label: {
                                        Image(systemName: "arrow.clockwise")
                                            .font(.system(size: AppFont.secondary, weight: .medium))
                                            .foregroundStyle(p.text2)
                                    }
                                    .buttonStyle(.plain)
                                    .disabled(perms.loading || perms.requesting)
                                    .accessibilityLabel(hub.t("refresh"))
                                }
                                .padding(.horizontal, 4)

                                itemCard(perms.essentialItems, multiButton: false)
                            }
                        }

                        if !perms.qrItems.isEmpty {
                            VStack(alignment: .leading, spacing: 8) {
                                Text(hub.t("ubPermGroupQR"))
                                    .font(.system(size: AppFont.secondary, weight: .semibold))
                                    .foregroundStyle(p.text2)
                                    .padding(.horizontal, 4)

                                itemCard(perms.qrItems, multiButton: false)

                                Text(hub.t("ubPermQRHint"))
                                    .font(.system(size: AppFont.caption))
                                    .foregroundStyle(p.mute)
                                    .padding(.horizontal, 4)
                            }
                        }

                        if !perms.remoteDesktopItems.isEmpty {
                            remoteDesktopScenarioCard
                        }

                        if !perms.advancedItems.isEmpty {
                            advancedDisclosureCard
                        }
                    }

                    if !perms.feedback.isEmpty {
                        Text(perms.feedback)
                            .font(.system(size: AppFont.secondary))
                            .foregroundStyle(p.text2)
                            .multilineTextAlignment(.center)
                            .padding(.horizontal, 8)
                    }
                }
                .frame(maxWidth: maxW)
                .padding(.horizontal, 20)
                .padding(.vertical, 12)
                .frame(maxWidth: .infinity)
            }

            Rectangle()
                .fill(p.s2)
                .frame(height: 1)

            footerBar(maxW: maxW)
                .padding(.horizontal, 20)
                .padding(.vertical, 14)
                .frame(maxWidth: .infinity)
                .background(p.page)
        }
        .background(p.page.ignoresSafeArea())
        .task {
            await perms.refresh()
        }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active {
                Task { await perms.refresh() }
            }
        }
        .onChange(of: windowVisible) { _, vis in
            if vis {
                Task { await perms.refresh() }
            }
        }
    }

    private func itemCard(_ list: [UnboxPermissionItem], multiButton: Bool) -> some View {
        VStack(spacing: 0) {
            ForEach(Array(list.enumerated()), id: \.element.id) { idx, item in
                if idx > 0 {
                    Rectangle()
                        .fill(p.s2)
                        .frame(height: 1)
                        .padding(.horizontal, 16)
                }
                permissionRow(item, multiButton: multiButton)
            }
        }
        .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
    }

    private var remoteDesktopScenarioCard: some View {
        let ready = perms.allRemoteDesktopGranted
        let cols = [GridItem(.adaptive(minimum: 165), spacing: 8)]
        return VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 8) {
                Text(hub.t("ubPermRemoteTitle"))
                    .font(.system(size: AppFont.cardTitle, weight: .semibold))
                    .foregroundStyle(p.text)
                Spacer(minLength: 8)
                Tag(
                    text: ready
                        ? hub.t("ubPermRemoteAllReady")
                        : hub.t("ubPermRemoteProgress", [
                            "granted": perms.remoteDesktopGrantedCount,
                            "total": perms.remoteDesktopItems.count,
                        ]),
                    bg: ready ? p.sage : p.s1,
                    fg: ready ? p.sageInk : p.text2
                )
            }

            Text(hub.t("ubPermRemoteDesc"))
                .font(.system(size: AppFont.secondary))
                .foregroundStyle(p.text2)
                .fixedSize(horizontal: false, vertical: true)

            LazyVGrid(columns: cols, alignment: .leading, spacing: 8) {
                ForEach(perms.remoteDesktopItems) { item in
                    let granted = item.status == .granted || item.status == .requested
                    let purpose = item.shortPurposeKey.map { locText($0, fallback: "") } ?? ""
                    HStack(spacing: 6) {
                        Circle()
                            .fill(granted ? p.sageInk : p.mute)
                            .frame(width: 7, height: 7)
                        Text(locText(item.titleKey, fallback: item.fallbackTitle))
                            .font(.system(size: AppFont.caption, weight: .semibold))
                            .foregroundStyle(granted ? p.sageInk : p.text)
                            .lineLimit(1)
                        if !purpose.isEmpty {
                            Text("· \(purpose)")
                                .font(.system(size: AppFont.caption))
                                .foregroundStyle(granted ? p.sageInk : p.text2)
                                .lineLimit(1)
                        }
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 6)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(granted ? p.sage : p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                }
            }

            HStack(spacing: 10) {
                if perms.hasPromptableRemoteDesktop {
                    Button(hub.t("ubPermRemoteEnableBtn")) {
                        remoteDetailsOpen = true
                        Task { await perms.requestRemoteDesktopBundle() }
                    }
                    .buttonStyle(HubPrimaryButtonStyle())
                    .disabled(perms.requesting)
                }
                Button(remoteDetailsOpen ? hub.t("ubPermHideDetails") : hub.t("ubPermShowDetails")) {
                    withAnimation(.easeOut(duration: 0.18)) {
                        remoteDetailsOpen.toggle()
                    }
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
                Spacer()
            }

            if remoteDetailsOpen {
                VStack(spacing: 0) {
                    ForEach(perms.remoteDesktopItems) { item in
                        Rectangle()
                            .fill(p.s2)
                            .frame(height: 1)
                        permissionRow(item, multiButton: true, horizontalPad: 0)
                    }
                }
                .padding(.top, 2)
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
    }

    private var advancedDisclosureCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            Button {
                withAnimation(.easeOut(duration: 0.18)) {
                    advancedOpen.toggle()
                }
            } label: {
                HStack(spacing: 8) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(hub.t("ubPermAdvTitle"))
                            .font(.system(size: AppFont.body, weight: .semibold))
                            .foregroundStyle(p.text)
                        Text(hub.t("ubPermAdvHint"))
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(p.text2)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    Spacer(minLength: 8)
                    Image(systemName: advancedOpen ? "chevron.up" : "chevron.down")
                        .font(.system(size: AppFont.caption, weight: .semibold))
                        .foregroundStyle(p.mute)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            if advancedOpen {
                ForEach(perms.advancedItems) { item in
                    Rectangle()
                        .fill(p.s2)
                        .frame(height: 1)
                    permissionRow(item, multiButton: false, horizontalPad: 0)
                }
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
    }

    @ViewBuilder
    private func permissionRow(_ item: UnboxPermissionItem, multiButton: Bool, horizontalPad: CGFloat = 16) -> some View {
        let badge = statusBadge(item.status)
        let rowBusy = perms.requesting || perms.busyItemId == item.id
        let resolved = item.status == .granted || item.status == .requested
        let showPromptSingle = item.canPrompt && item.status == .notDetermined
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Text(locText(item.titleKey, fallback: item.fallbackTitle))
                    .font(.system(size: AppFont.body, weight: .semibold))
                    .foregroundStyle(p.text)
                Spacer(minLength: 8)
                Tag(text: badge.text, bg: badge.bg, fg: badge.fg)
            }
            Text(locText(item.descKey, fallback: item.fallbackDesc))
                .font(.system(size: AppFont.secondary))
                .foregroundStyle(p.text2)
                .fixedSize(horizontal: false, vertical: true)
            if !resolved && (item.canPrompt || item.canOpenSettings) {
                HStack(spacing: 8) {
                    if multiButton {
                        if item.canPrompt {
                            Button(hub.t("deskPermRequest")) {
                                Task { await perms.requestItem(item, openSettings: false) }
                            }
                            .buttonStyle(HubPrimaryButtonStyle())
                            .disabled(rowBusy)
                        }
                        if item.canOpenSettings {
                            Button(hub.t("deskPermOpenSettings")) {
                                Task { await perms.requestItem(item, openSettings: true) }
                            }
                            .buttonStyle(.bordered)
                            .controlSize(.small)
                            .disabled(rowBusy)
                        }
                    } else if showPromptSingle {
                        Button(hub.t("ubPermEnableBtn")) {
                            Task { await perms.requestItem(item, openSettings: false) }
                        }
                        .buttonStyle(HubPrimaryButtonStyle())
                        .disabled(rowBusy)
                    } else if item.canOpenSettings {
                        Button(hub.t("deskPermOpenSettings")) {
                            Task { await perms.requestItem(item, openSettings: true) }
                        }
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                        .disabled(rowBusy)
                    }
                }
                .padding(.top, 2)
            }
        }
        .padding(.horizontal, horizontalPad)
        .padding(.vertical, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private func footerBar(maxW: CGFloat) -> some View {
        if perms.autoAdvanceOnGrantAll {
            VStack(spacing: 10) {
                Button {
                    Task {
                        if perms.hasPromptableUngranted {
                            await perms.requestAll()
                        }
                        perms.markUnboxDone()
                        onDone()
                    }
                } label: {
                    HStack(spacing: 8) {
                        if perms.requesting {
                            ProgressView()
                                .controlSize(.small)
                                .tint(p.card)
                        }
                        Text(perms.hasPromptableUngranted ? hub.t("ubPermGrantAllContinue") : hub.t("ubContinue"))
                            .font(.system(size: AppFont.body, weight: .semibold))
                    }
                    .frame(maxWidth: .infinity)
                    .frame(height: 48)
                    .background(p.accent, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                    .foregroundStyle(p.card)
                }
                .buttonStyle(.plain)
                .disabled(perms.requesting)

                Button {
                    perms.markUnboxDone()
                    onDone()
                } label: {
                    Text(hub.t("ubPermSkipLater"))
                        .font(.system(size: AppFont.body, weight: .medium))
                        .frame(maxWidth: .infinity)
                        .frame(height: 48)
                        .background(p.card, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                        .foregroundStyle(p.text)
                }
                .buttonStyle(.plain)
                .disabled(perms.requesting)
            }
            .frame(maxWidth: 420)
        } else {
            HStack(spacing: 12) {
                Text(hub.t("ubPermLaterInSettings"))
                    .font(.system(size: AppFont.secondary))
                    .foregroundStyle(p.text2)
                Spacer()
                Button {
                    perms.markUnboxDone()
                    onDone()
                } label: {
                    Text(hub.t("ubContinue"))
                        .font(.system(size: AppFont.body, weight: .semibold))
                        .foregroundStyle(p.card)
                        .padding(.horizontal, 18)
                        .frame(height: 36)
                        .background(p.accent, in: Capsule())
                }
                .buttonStyle(.plain)
                .disabled(perms.requesting)
            }
            .frame(maxWidth: maxW)
        }
    }
}
