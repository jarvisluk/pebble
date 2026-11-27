import SwiftUI

struct RoutinesSection: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    var botId: String

    @State private var editing: RoutineItem?
    @State private var creating = false
    @State private var err = ""
    @State private var confirm: ConfirmItem?

    private var list: [RoutineItem] { hub.routines[botId] ?? [] }

    var body: some View {
        let cardFill = p.dark ? Color(hex: "#1c1c1e") : p.card
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(hub.t("routines"))
                Spacer()
                Button { creating = true } label: { Label(hub.t("addShort"), systemImage: "plus") }
                    .labelStyle(.iconOnly)
                    .buttonStyle(.borderless)
            }
            .font(.system(size: AppFont.secondary))
            .foregroundStyle(p.mute)
            .padding(.horizontal, 16)

            VStack(alignment: .leading, spacing: 0) {
                if !err.isEmpty {
                    StatusText(text: err)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 12)
                }
                if list.isEmpty {
                    Text(hub.t("rtEmpty"))
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 12)
                } else {
                    ForEach(Array(list.enumerated()), id: \.element.id) { idx, rt in
                        if idx > 0 {
                            Rectangle()
                                .fill(Color.primary.opacity(p.dark ? 0.10 : 0.08))
                                .frame(height: 0.5)
                                .padding(.leading, 16)
                        }
                        row(rt)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal, 16)
                            .padding(.vertical, 12)
                    }
                }
            }
            .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
            .background(cardFill, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        }
        .confirmAlert($confirm)
        .sheet(isPresented: $creating) {
            RoutineEditor(botId: botId, routine: nil)
                .hubSheetFrame(width: 480, height: 560)
        }
        .sheet(item: $editing) { rt in
            RoutineEditor(botId: botId, routine: rt)
                .hubSheetFrame(width: 480, height: 560)
        }
    }

    private func statusText(_ s: String?) -> String {
        switch s {
        case "ok": return hub.t("rtStatusOk")
        case "silent": return hub.t("rtStatusSilent")
        case "error": return hub.t("rtStatusError")
        default: return ""
        }
    }

    private func row(_ rt: RoutineItem) -> some View {
        let trig = [
            (rt.everyMinutes ?? 0) > 0 ? hub.t("rtEveryShort", ["n": rt.everyMinutes!]) : "",
            (rt.dailyAt ?? "").isEmpty ? "" : hub.t("rtDailyShort", ["time": rt.dailyAt!]),
            (rt.cron ?? "").isEmpty ? "" : "Cron \(rt.cron!)",
            rt.webhookTrigger == true ? hub.t("rtWebhookTag") : "",
        ].filter { !$0.isEmpty }.joined(separator: " · ")
        let deliv = rt.mainDelivery == "notify" ? hub.t("rtDeliveryNotify") : rt.mainDelivery == "none" ? hub.t("rtDeliveryNone") : hub.t("rtDeliveryReceipt")
        let running = rt.running ?? false
        let status = running ? hub.t("rtStatusRunning") : statusText(rt.lastRun?.status)
        let rtPermLabel = rt.permissionMode == "auto" ? hub.t("permMode_auto") : rt.permissionMode == "plan" ? hub.t("permMode_plan") : ""
        let meta = [trig, deliv, rtPermLabel, (rt.webhookUrl ?? "").isEmpty ? "" : hub.t("rtWebhookPushTag"), rt.model ?? ""].filter { !$0.isEmpty }.joined(separator: " · ")
        return VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Text(rt.name ?? rt.id).font(.system(size: AppFont.body, weight: .semibold)).lineLimit(1)
                if !status.isEmpty {
                    Text(status).font(.system(size: AppFont.caption, weight: .semibold))
                        .foregroundStyle(rt.lastRun?.status == "error" && !running ? p.danger : p.sageInk)
                }
                Spacer()
                Toggle("", isOn: Binding(get: { rt.enabled ?? false }, set: { v in
                    Task { do { try await hub.updateRoutine(botId, rt.id, ["enabled": v]) } catch { err = errText(error, hub.t("rtStatusUpdateFailed")) } }
                }))
                .labelsHidden()
                .toggleStyle(.switch)
                .controlSize(.small)
            }
            Text(meta).font(.system(size: AppFont.caption)).foregroundStyle(.secondary)
            Text(rt.prompt ?? "").font(.system(size: AppFont.secondary)).lineLimit(4)
            HStack(spacing: 8) {
                Button(running ? hub.t("rtRunningBtn") : hub.t("rtRunNow")) {
                    err = ""
                    Task { do { try await hub.runRoutine(botId, rt.id) } catch { err = errText(error, hub.t("rtRunFailed")) } }
                }
                .disabled(running)
                Menu {
                    Button(hub.t("edit"), systemImage: "pencil") { editing = rt }
                    if rt.webhookTrigger == true, let path = rt.webhookPath, !path.isEmpty {
                        Button(hub.t("rtCopyWebhook"), systemImage: "doc.on.doc") {
                            var base = hub.baseURL
                            if base.hasSuffix("/") { base.removeLast() }
                            copyToClipboard(base + path)
                        }
                    }
                    Divider()
                    Button(hub.t("delete"), systemImage: "trash", role: .destructive) {
                        confirm = ConfirmItem(title: hub.t("rtDeleteTitle", ["name": rt.name ?? rt.id]), message: hub.t("rtDeleteDesc")) {
                            try? await hub.deleteRoutine(botId, rt.id)
                        }
                    }
                } label: {
                    Label(hub.t("edit"), systemImage: "ellipsis.circle").labelStyle(.iconOnly)
                }
                .menuIndicator(.hidden)
                .fixedSize()
            }
            .buttonStyle(.bordered)
            .controlSize(.small)
        }
        .padding(.vertical, 4)
    }
}
