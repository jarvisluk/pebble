#if os(macOS)
import SwiftUI

struct MacSetupView: View {
    @Environment(HubStore.self) private var hub
    @Environment(DesktopHostManager.self) private var host
    @Environment(\.palette) private var p
    @Environment(\.openURL) private var openURL

    @State private var remoteExpanded = false
    @State private var remoteLink = ""
    @State private var remoteToken = ""
    @State private var tsAdvOpen = false
    @State private var tsAuthKey = ""
    @State private var tsControlUrl = ""
    @State private var localErr = ""

    private var snap: DesktopHostSnapshot { host.snapshot }

    private var isStarting: Bool {
        snap.statusState == "starting" || (!snap.status.isEmpty && snap.activeUrl.isEmpty)
    }

    private var errorMessage: String {
        if !localErr.isEmpty { return localErr }
        if !host.daemonError.isEmpty { return host.daemonError }
        if snap.statusState == "error", !snap.statusError.isEmpty { return snap.statusError }
        return ""
    }

    var body: some View {
        GeometryReader { geo in
            ScrollView {
                VStack(spacing: 24) {
                    Spacer(minLength: 16)

                    VStack(spacing: 12) {
                        BotAvatar(id: "assistant", size: 96, color: pebbleColor, busy: isStarting)
                        Text("Pebble")
                            .font(.system(size: AppFont.hero, weight: .bold))
                            .foregroundStyle(p.text)
                        Text(hub.t("onboardHeroSub"))
                            .font(.system(size: AppFont.secondary))
                            .foregroundStyle(p.text2)
                            .multilineTextAlignment(.center)
                    }

                    if !host.ready && errorMessage.isEmpty {
                        VStack(spacing: 10) {
                            ProgressView()
                            Text(hub.t("macModeStarting"))
                                .font(.system(size: AppFont.secondary))
                                .foregroundStyle(p.text2)
                        }
                        .padding(.vertical, 24)
                    } else if isStarting {
                        VStack(spacing: 10) {
                            ProgressView()
                            Text(snap.status.isEmpty ? hub.t("macModeStarting") : snap.status)
                                .font(.system(size: AppFont.secondary))
                                .foregroundStyle(p.text2)
                        }
                        .padding(.vertical, 24)
                    } else {
                        VStack(spacing: 12) {
                            localCard
                            remoteCard
                        }
                    }

                    if !errorMessage.isEmpty {
                        VStack(spacing: 10) {
                            StatusText(text: errorMessage)
                            if !host.ready {
                                Button(hub.t("retry")) {
                                    localErr = ""
                                    host.startDaemonIfNeeded()
                                }
                                .buttonStyle(.borderedProminent)
                            }
                        }
                        .padding(14)
                        .frame(maxWidth: .infinity)
                        .background(p.card, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
                    }

                    Spacer(minLength: 16)
                }
                .frame(maxWidth: 460)
                .padding(.horizontal, 24)
                .padding(.vertical, 16)
                .frame(maxWidth: .infinity, minHeight: geo.size.height)
            }
        }
        .background(p.page)
        .navigationTitle("")
        .modifier(MacSetupToolbarHiddenModifier())
        .onAppear {
            if remoteLink.isEmpty && !snap.link.isEmpty {
                remoteLink = snap.link
            }
        }
    }

    private var localCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 10) {
                Image(systemName: "laptopcomputer")
                    .font(.system(size: 18, weight: .semibold))
                    .foregroundStyle(p.text)
                VStack(alignment: .leading, spacing: 2) {
                    Text(hub.t("macModeLocalTitle"))
                        .font(.system(size: AppFont.cardTitle, weight: .semibold))
                        .foregroundStyle(p.text)
                    Text(hub.t("macModeLocalDesc"))
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(p.text2)
                }
                Spacer()
            }
            Button {
                localErr = ""
                Task { await host.startLocal() }
            } label: {
                Text(hub.t("macModeLocalTitle"))
                    .font(.system(size: AppFont.body, weight: .semibold))
                    .frame(maxWidth: .infinity)
                    .frame(height: 36)
                    .background(p.accent, in: Capsule())
                    .foregroundStyle(p.card)
            }
            .buttonStyle(.plain)
            .disabled(!host.ready)
        }
        .padding(16)
        .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
    }

    private var remoteCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            Button {
                withAnimation(.easeOut(duration: 0.2)) {
                    remoteExpanded.toggle()
                }
            } label: {
                HStack(spacing: 10) {
                    Image(systemName: "network")
                        .font(.system(size: 18, weight: .semibold))
                        .foregroundStyle(p.text)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(hub.t("macModeRemoteTitle"))
                            .font(.system(size: AppFont.cardTitle, weight: .semibold))
                            .foregroundStyle(p.text)
                        Text(hub.t("macModeRemoteDesc"))
                            .font(.system(size: AppFont.secondary))
                            .foregroundStyle(p.text2)
                    }
                    Spacer()
                    Image(systemName: remoteExpanded ? "chevron.up" : "chevron.down")
                        .font(.system(size: 12, weight: .semibold))
                        .foregroundStyle(p.mute)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            if remoteExpanded {
                Rectangle().fill(p.s2).frame(height: 1)
                VStack(alignment: .leading, spacing: 10) {
                    HStack(spacing: 8) {
                        TextField(hub.t("macRemoteLinkPh"), text: $remoteLink)
                            .textFieldStyle(HubMacTextFieldStyle())
                            .monoInput()
                        Button(hub.t("pasteLinkBtn")) {
                            let clip = readClipboardString()
                                .trimmingCharacters(in: .whitespacesAndNewlines)
                                .replacingOccurrences(of: "[\\r\\n]+", with: "", options: .regularExpression)
                            if !clip.isEmpty { remoteLink = clip }
                        }
                        .buttonStyle(.bordered)
                    }

                    SecureField(hub.t("macRemoteTokenPh"), text: $remoteToken)
                        .textFieldStyle(HubMacTextFieldStyle())
                        .font(.system(size: AppFont.secondary))

                    let canConnect = host.ready && !remoteLink.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                    Button {
                        let trimmed = remoteLink.trimmingCharacters(in: .whitespacesAndNewlines)
                        guard !trimmed.isEmpty else {
                            localErr = hub.t("enterValidAddr")
                            return
                        }
                        localErr = ""
                        Task { await host.startRemote(link: trimmed, token: remoteToken) }
                    } label: {
                        Text(hub.t("connect"))
                            .font(.system(size: AppFont.body, weight: .semibold))
                            .frame(maxWidth: .infinity)
                            .frame(height: 36)
                            .background(canConnect ? p.accent : p.s2, in: Capsule())
                            .foregroundStyle(canConnect ? p.card : p.text2)
                    }
                    .buttonStyle(.plain)
                    .disabled(!host.ready)

                    tailscaleArea
                }
            }
        }
        .padding(16)
        .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
    }

    @ViewBuilder
    private var tailscaleArea: some View {
        let ts = host.tailscale
        let state = ts?.state ?? "disabled"
        Divider()
        if state == "running" {
            HStack {
                Text(hub.t("tsBoundReady", ["tailnet": ts?.tailnet ?? "Tailscale"]))
                    .font(.system(size: AppFont.secondary, weight: .semibold))
                    .foregroundStyle(p.sageInk)
                Spacer()
                Button(hub.t("tsRefreshPeers")) {
                    Task { await host.refreshTailscale(includePeers: true) }
                }
                .buttonStyle(.bordered).controlSize(.small)
                Button(hub.t("tsUnlink"), role: .destructive) {
                    Task { await host.logoutTailscale() }
                }
                .buttonStyle(.bordered).controlSize(.small)
            }
            let peers = (ts?.peers ?? []).filter { $0.online }
            ForEach(peers) { peer in
                Button {
                    remoteLink = peer.address
                } label: {
                    HStack {
                        Text(peer.hostname).foregroundStyle(p.text)
                        if peer.isBotHub {
                            Tag(text: hub.t("tsBotHubBadge"), bg: p.lilac, fg: p.lilacInk)
                        }
                        Spacer()
                        Text(peer.dnsName ?? peer.ip ?? peer.address)
                            .font(.system(size: 12, design: .monospaced))
                            .foregroundStyle(p.mute)
                            .lineLimit(1)
                    }
                    .padding(.vertical, 4)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        } else if state == "needs_login", let auth = ts?.authUrl, let url = URL(string: auth) {
            HStack {
                Text(hub.t("tsNeedsLogin")).font(.system(size: AppFont.secondary))
                Spacer()
                Button(hub.t("tsOpenLogin")) { openURL(url) }
                    .buttonStyle(.borderedProminent).controlSize(.small)
            }
        } else if state == "starting" || host.tsBinding {
            HStack(spacing: 8) {
                ProgressView().controlSize(.small)
                Text(hub.t("tsBinding")).font(.system(size: AppFont.secondary)).foregroundStyle(p.text2)
            }
        } else {
            DisclosureGroup(hub.t("tsBindAccount"), isExpanded: $tsAdvOpen) {
                VStack(alignment: .leading, spacing: 8) {
                    SecureField(hub.t("tsAuthKeyPh"), text: $tsAuthKey).textFieldStyle(HubMacTextFieldStyle())
                    TextField(hub.t("tsControlUrlPh"), text: $tsControlUrl).textFieldStyle(HubMacTextFieldStyle())
                    Button(hub.t("tsBindAccount")) {
                        Task {
                            await host.bindTailscale(authKey: tsAuthKey, controlUrl: tsControlUrl)
                            tsAuthKey = ""
                        }
                    }
                    .buttonStyle(.bordered)
                }
                .padding(.top, 4)
            }
            .font(.system(size: AppFont.secondary))
        }
    }
}

private struct MacSetupToolbarHiddenModifier: ViewModifier {
    func body(content: Content) -> some View {
        if #available(macOS 15.0, *) {
            content.toolbarBackgroundVisibility(.hidden, for: .windowToolbar)
        } else {
            content
        }
    }
}
#endif
