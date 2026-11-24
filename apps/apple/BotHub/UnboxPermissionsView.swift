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

    private func locText(_ key: String, fallback: String) -> String {
        let v = hub.t(key)
        return v == key ? fallback : v
    }

    private func statusBadge(_ status: UnboxPermStatus) -> (text: String, bg: Color, fg: Color) {
        switch status {
        case .granted:
            return (hub.t("deskPermGranted"), p.sage, p.sageInk)
        case .requested:
            return (hub.t("ubPermRequested"), p.sage, p.sageInk)
        case .denied:
            return (hub.t("deskPermDenied"), p.pink, p.danger)
        case .unsupported:
            return (hub.t("deskPermUnsupported"), p.s1, p.mute)
        case .notDetermined:
            return (hub.t("deskPermNotDetermined"), p.s1, p.text2)
        }
    }

    var body: some View {
        let maxW: CGFloat = compact ? 420 : 620
        VStack(spacing: 0) {
            if onBack != nil || !perms.autoAdvanceOnGrantAll {
                HStack {
                    if let onBack {
                        Button(hub.t("backBtn"), action: onBack)
                            .font(.system(size: AppFont.body, weight: .medium))
                            .foregroundStyle(p.text2)
                            .buttonStyle(.plain)
                    }
                    Spacer()
                    if !perms.autoAdvanceOnGrantAll {
                        Button(hub.t("ubSkip")) {
                            perms.markUnboxDone()
                            onDone()
                        }
                        .font(.system(size: AppFont.body, weight: .medium))
                        .foregroundStyle(p.text2)
                        .buttonStyle(.plain)
                    }
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
                    .padding(.top, (onBack == nil && perms.autoAdvanceOnGrantAll) ? 24 : 8)

                    HStack(spacing: 8) {
                        let summary = perms.allCoreGranted
                            ? hub.t("ubPermAllReady")
                            : hub.t("ubPermProgress", [
                                "granted": perms.grantedCoreCount,
                                "total": perms.coreItems.count,
                            ])
                        Text(summary)
                            .font(.system(size: AppFont.secondary, weight: .semibold))
                            .foregroundStyle(perms.allCoreGranted ? p.sageInk : p.text2)
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

                    if perms.items.isEmpty && perms.loading {
                        ProgressView()
                            .padding(.vertical, 32)
                    } else {
                        VStack(spacing: 0) {
                            ForEach(Array(perms.items.enumerated()), id: \.element.id) { idx, item in
                                if idx > 0 {
                                    Rectangle()
                                        .fill(p.s2)
                                        .frame(height: 1)
                                        .padding(.horizontal, 16)
                                }
                                permissionRow(item)
                            }
                        }
                        .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
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

    @ViewBuilder
    private func permissionRow(_ item: UnboxPermissionItem) -> some View {
        let badge = statusBadge(item.status)
        let rowBusy = perms.requesting || perms.busyItemId == item.id
        let resolved = item.status == .granted || item.status == .requested
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Text(locText(item.titleKey, fallback: item.fallbackTitle))
                    .font(.system(size: AppFont.body, weight: .semibold))
                    .foregroundStyle(p.text)
                if item.optional {
                    Tag(text: hub.t("ubOptional"), bg: p.s1, fg: p.text2)
                }
                Spacer(minLength: 8)
                Tag(text: badge.text, bg: badge.bg, fg: badge.fg)
            }
            Text(locText(item.descKey, fallback: item.fallbackDesc))
                .font(.system(size: AppFont.secondary))
                .foregroundStyle(p.text2)
                .fixedSize(horizontal: false, vertical: true)
            if !resolved && (item.canPrompt || item.canOpenSettings) {
                HStack(spacing: 8) {
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
                }
                .padding(.top, 2)
            }
        }
        .padding(16)
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
                Spacer()
                if perms.hasPromptableUngranted {
                    Button {
                        Task { await perms.requestAll() }
                    } label: {
                        HStack(spacing: 6) {
                            if perms.requesting {
                                ProgressView()
                                    .controlSize(.small)
                                    .tint(p.card)
                            }
                            Text(hub.t("ubPermRequestAll"))
                        }
                        .font(.system(size: AppFont.body, weight: .semibold))
                        .foregroundStyle(p.card)
                        .padding(.horizontal, 18)
                        .frame(height: 36)
                        .background(p.accent, in: Capsule())
                    }
                    .buttonStyle(.plain)
                    .disabled(perms.requesting)

                    Button {
                        perms.markUnboxDone()
                        onDone()
                    } label: {
                        Text(hub.t("ubContinue"))
                            .font(.system(size: AppFont.body, weight: .medium))
                            .foregroundStyle(p.text)
                            .padding(.horizontal, 18)
                            .frame(height: 36)
                            .background(p.card, in: Capsule())
                            .overlay(Capsule().stroke(p.s2, lineWidth: 1))
                    }
                    .buttonStyle(.plain)
                    .disabled(perms.requesting)
                } else {
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
            }
            .frame(maxWidth: maxW)
        }
    }
}
