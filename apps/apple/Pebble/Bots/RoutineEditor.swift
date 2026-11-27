import SwiftUI

struct RoutineEditor: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.dismiss) private var dismiss
    @Environment(\.palette) private var p
    var botId: String
    var routine: RoutineItem?

    enum Mode: String, CaseIterable { case interval, daily, cron, webhook }

    @State private var name = ""
    @State private var mode: Mode = .interval
    @State private var every = "60"
    @State private var daily = "09:00"
    @State private var cron = "0 9 * * 1-5"
    @State private var webhookTrigger = false
    @State private var secret = ""
    @State private var secretSet = false
    @State private var clearSecret = false
    @State private var webhookUrl = ""
    @State private var delivery = "receipt"
    @State private var permMode = ""
    @State private var prompt = ""
    @State private var enabled = true
    @State private var saving = false
    @State private var err = ""

    private func modeLabel(_ m: Mode) -> String {
        switch m {
        case .interval: return hub.t("rtModeInterval")
        case .daily: return hub.t("rtModeDaily")
        case .cron: return hub.t("rtModeCron")
        case .webhook: return hub.t("rtModeWebhook")
        }
    }

    var body: some View {
        HubSheetScaffold(
            title: routine == nil ? hub.t("routines") : (routine?.name ?? hub.t("routines")),
            closeTitle: hub.t("cancel"),
            onClose: { dismiss() },
            actionTitle: hub.t("rtSaveBtn"),
            loading: saving,
            onAction: { save() }
        ) {
            Form {
                if !err.isEmpty { Section { StatusText(text: err) } }
                Section {
                    TextField(hub.t("rtNamePh"), text: $name)
                    HubSegmentedPicker(
                        selection: Binding(get: { mode }, set: { mode = $0; if $0 == .webhook { webhookTrigger = true } }),
                        options: Mode.allCases.map { ($0, modeLabel($0)) }
                    )
                    switch mode {
                    case .interval: TextField(hub.t("rtEveryPh"), text: $every).monoInput()
                        .numberPadKeyboard()
                    case .daily: TextField(hub.t("rtDailyPh"), text: $daily).monoInput()
                    case .cron: TextField(hub.t("rtCronPh"), text: $cron).monoInput()
                    case .webhook: EmptyView()
                    }
                    if mode != .webhook { Toggle(hub.t("rtAllowWebhook"), isOn: $webhookTrigger) }
                }
                if mode == .webhook || webhookTrigger {
                    Section("Webhook") {
                        SecureField(secretSet && !clearSecret ? hub.t("rtWebhookSecretSetPh") : hub.t("rtWebhookSecretPh"), text: $secret)
                            .disabled(clearSecret)
                        if secretSet {
                            Toggle(hub.t("rtWebhookClearSecret"), isOn: Binding(get: { clearSecret }, set: { clearSecret = $0; if $0 { secret = "" } }))
                        }
                    }
                }
                Section {
                    TextField(hub.t("rtWebhookUrlPh"), text: $webhookUrl).monoInput()
                    HubSegmentedPicker(
                        selection: $delivery,
                        options: [
                            ("receipt", hub.t("rtDeliveryReceipt")),
                            ("notify", hub.t("rtDeliveryNotify")),
                            ("none", hub.t("rtDeliveryNone")),
                        ]
                    )
                    VStack(alignment: .leading, spacing: 6) {
                        Text(hub.t("rtPermMode"))
                            .font(.system(size: AppFont.secondary))
                            .foregroundStyle(p.text2)
                        HubSegmentedPicker(
                            selection: $permMode,
                            options: [
                                ("", hub.t("rtPermFollow")),
                                ("auto", hub.t("permMode_auto")),
                                ("plan", hub.t("permMode_plan")),
                            ]
                        )
                    }
                    TextField(hub.t("rtPromptPh"), text: $prompt, axis: .vertical).lineLimit(3...12)
                    Toggle(hub.t("rtEnabled"), isOn: $enabled)
                }
            }
            .hubForm()
        }
        .onAppear(perform: load)
    }

    private func load() {
        let rt = routine
        name = rt?.name ?? ""
        let hasEvery = (rt?.everyMinutes ?? 0) > 0
        let hasDaily = !(rt?.dailyAt ?? "").isEmpty
        let hasCron = !(rt?.cron ?? "").isEmpty
        mode = hasCron ? .cron : (hasDaily && !hasEvery) ? .daily : (!hasEvery && !hasDaily && rt?.webhookTrigger == true) ? .webhook : .interval
        every = hasEvery ? "\(rt!.everyMinutes!)" : "60"
        daily = hasDaily ? rt!.dailyAt! : "09:00"
        cron = hasCron ? rt!.cron! : "0 9 * * 1-5"
        webhookTrigger = rt?.webhookTrigger == true || mode == .webhook
        secretSet = rt?.webhookSecretSet ?? false
        webhookUrl = rt?.webhookUrl ?? ""
        delivery = rt?.mainDelivery ?? "receipt"
        permMode = (rt?.permissionMode == "auto" || rt?.permissionMode == "plan") ? (rt?.permissionMode ?? "") : ""
        prompt = rt?.prompt ?? ""
        enabled = rt?.enabled ?? true
    }

    private func save() {
        let n = name.trimmingCharacters(in: .whitespaces)
        let pr = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        if n.isEmpty { err = hub.t("rtNameRequired"); return }
        if pr.isEmpty { err = hub.t("rtPromptRequired"); return }
        let everyMinutes = mode == .interval ? (Int(every.trimmingCharacters(in: .whitespaces)) ?? 0) : 0
        let dailyAt = mode == .daily ? daily.trimmingCharacters(in: .whitespaces) : ""
        let cronExpr = mode == .cron ? cron.trimmingCharacters(in: .whitespaces) : ""
        if mode == .interval && everyMinutes <= 0 { err = hub.t("rtEveryPositive"); return }
        if mode == .daily && dailyAt.range(of: "^\\d{2}:\\d{2}$", options: .regularExpression) == nil { err = hub.t("rtTimeFormatErr"); return }
        if mode == .cron && cronExpr.isEmpty { err = hub.t("rtCronRequired"); return }
        err = ""
        var body: [String: Any] = [
            "name": n, "everyMinutes": everyMinutes, "dailyAt": dailyAt, "cron": cronExpr,
            "webhookTrigger": mode == .webhook ? true : webhookTrigger,
            "webhookUrl": webhookUrl.trimmingCharacters(in: .whitespaces),
            "prompt": pr, "mainDelivery": delivery, "permissionMode": permMode, "enabled": enabled,
        ]
        let sec = secret.trimmingCharacters(in: .whitespaces)
        if clearSecret { body["clearWebhookSecret"] = true } else if !sec.isEmpty { body["webhookSecret"] = sec }
        saving = true
        Task {
            do {
                if let id = routine?.id { try await hub.updateRoutine(botId, id, body) } else { try await hub.createRoutine(botId, body) }
                dismiss()
            } catch { err = errText(error, hub.t("rtSaveFailed")) }
            saving = false
        }
    }
}
