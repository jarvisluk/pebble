import SwiftUI

struct DevicesView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p

    private var tailcatText: String {
        guard let t = hub.tailcat else { return hub.t("tailcatUnknown") }
        switch t.state {
        case "ready": return hub.t("tailcatReady")
        case "connecting": return hub.t("tailcatConnecting")
        case "disabled": return hub.t("tailcatDisabled")
        default: return (t.error ?? "").isEmpty ? hub.t("tailcatErr") : t.error!
        }
    }

    private var hostTailscaleText: String {
        guard let t = hub.hostTailscale else { return hub.t("tailcatUnknown") }
        switch t.state {
        case "running": return t.tailnet ?? hub.t("tailcatReady")
        case "starting": return hub.t("tsBinding")
        case "needs_login": return hub.t("tsNeedsLogin")
        case "disabled": return hub.t("tailcatDisabled")
        default: return (t.error ?? "").isEmpty ? hub.t("tailcatErr") : t.error!
        }
    }

    private var mobileTailscaleText: String {
        guard let t = hub.mobileTailscale else { return hub.t("tsNotBound") }
        switch t.state {
        case "running": return t.tailnet ?? hub.t("tailcatReady")
        case "starting": return hub.t("tsBinding")
        case "needs_login": return hub.t("tsNeedsLogin")
        case "disabled": return hub.t("tsNotBound")
        default: return (t.error ?? "").isEmpty ? hub.t("tailcatErr") : t.error!
        }
    }

    var body: some View {
        Form {
            Section(hub.t("currentMobileConn")) {
                if let s = hub.activeServer {
                    LabeledContent(s.label) {
                        Text(s.address).font(.system(size: AppFont.secondary, design: .monospaced)).lineLimit(1).truncationMode(.middle)
                    }
                    HStack(spacing: 6) {
                        Tag(text: hub.isTailscale ? hub.t("tailscaleE2E") : hub.isTailcat ? hub.t("tailcatE2E") : hub.t("httpLan"),
                            bg: p.lilac, fg: p.lilacInk)
                        Tag(text: hub.online ? hub.t("streamConnected") : hub.t("reconnecting"),
                            bg: hub.online ? p.sage : p.s2, fg: hub.online ? p.sageInk : .secondary)
                    }
                } else {
                    Text(hub.t("notConnectedHost")).foregroundStyle(.secondary)
                }
                LabeledContent(hub.t("tsMobileStatus")) {
                    Text(mobileTailscaleText)
                        .foregroundStyle(hub.mobileTailscale?.state == "running" ? p.sageInk : .secondary)
                }
                Button(hub.t("manageConn")) { router.sheet = .connect(scan: false) }
                if hub.activeServer != nil {
                    Button(hub.t("openHostDesktop")) { router.openDesktop("local") }
                }
            }
            Section(hub.t("hostRemoteAccess")) {
                LabeledContent(hub.t("tunnelState")) {
                    Text(tailcatText).foregroundStyle(hub.tailcat?.state == "ready" ? p.sageInk : hub.tailcat?.state == "error" ? p.danger : .secondary)
                }
                LabeledContent(hub.t("tsHostStatus")) {
                    Text(hostTailscaleText).foregroundStyle(hub.hostTailscale?.state == "running" ? p.sageInk : hub.hostTailscale?.state == "error" ? p.danger : .secondary)
                }
                LabeledContent(hub.t("tokenProtect"), value: hub.tailcat?.hasToken == true ? hub.t("tokenEnabled") : hub.t("tokenUnset"))
            }
            Section(hub.t("connectedNodes")) {
                if hub.nodes.isEmpty {
                    Text(hub.t("noRemoteNodes")).foregroundStyle(.secondary)
                } else {
                    ForEach(hub.nodes) { node in nodeRow(node) }
                }
            }
        }
        .hubForm()
        .navigationTitle(hub.t("devicesTunnel"))
        .inlineNavTitle()
        .toolbar { SheetToolbar() }
        .refreshable { await load() }
        .task { await load() }
    }

    private func load() async {
        await hub.refreshMobileTailscale(includePeers: false)
        guard hub.isConnected else { return }
        async let a: Void? = try? hub.refreshNodes()
        async let b: Void? = try? hub.refreshTailcat()
        async let c: Void? = try? hub.refreshHostTailscale()
        _ = await (a, b, c)
    }

    private func nodeRow(_ node: NodeInfo) -> some View {
        let on = node.connected ?? false
        let perms = node.permissions ?? []
        let labels = ["files": hub.t("permFiles"), "exec": hub.t("permExec"), "desktop": hub.t("permDesktop")]
        return VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(node.name ?? node.id).font(.system(size: AppFont.body, weight: .semibold))
                Spacer()
                Tag(text: on ? hub.t("online") : hub.t("offline"), bg: on ? p.sage : p.s2, fg: on ? p.sageInk : .secondary)
            }
            Text("\(node.os ?? "")/\(node.arch ?? "")\((node.hostname ?? "").isEmpty ? "" : " · \(node.hostname!)")")
                .font(.system(size: AppFont.secondary, design: .monospaced)).foregroundStyle(.secondary)
            if !perms.isEmpty {
                HStack(spacing: 6) {
                    ForEach(perms, id: \.self) { Tag(text: labels[$0] ?? $0, bg: p.s1, fg: .secondary) }
                    Spacer()
                    if on && perms.contains("desktop") {
                        Button(hub.t("openDesktop")) { router.openDesktop(node.id) }
                            .buttonStyle(.bordered).controlSize(.small)
                    }
                }
            }
        }
        .padding(.vertical, 4)
    }
}
