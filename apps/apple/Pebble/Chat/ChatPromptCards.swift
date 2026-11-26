import SwiftUI

struct QuestionCardView: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    var question: PendingQuestion
    var pal: (accent: Color, text: Color, soft: Color)

    @State private var singles: [String: String] = [:]
    @State private var multis: [String: [String]] = [:]
    @State private var bools: [String: Bool] = [:]
    @State private var customText: [String: String] = [:]
    @State private var customActive: [String: Bool] = [:]
    @State private var submitting = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text((question.title ?? "").isEmpty ? hub.t("questionTitle") : question.title!)
                    .font(.system(size: AppFont.cardTitle, weight: .bold)).foregroundStyle(p.text)
                Spacer()
                Text(hub.t("questionWaitingBadge"))
                    .font(.system(size: AppFont.caption, weight: .semibold)).foregroundStyle(p.busy)
                    .padding(.horizontal, 8).padding(.vertical, 3)
                    .background(p.s1, in: Capsule())
            }

            ForEach(question.fields, id: \.key) { f in
                fieldView(f)
            }

            HStack {
                Spacer()
                Button(hub.t("questionCancel")) { submit(cancelled: true) }
                    .buttonStyle(.bordered).disabled(submitting)
                Button(hub.t("questionSubmit")) { submit(cancelled: false) }
                    .buttonStyle(.borderedProminent).tint(pal.accent)
                    .disabled(submitting || !canSubmit)
            }
        }
        .padding(16)
        .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .onAppear { seedDefaults() }
        .onChange(of: question.id) { _, _ in seedDefaults() }
    }

    @ViewBuilder
    private func fieldView(_ f: QuestionField) -> some View {
        let isMulti = f.type == "multiselect"
        let opts = f.options ?? []
        VStack(alignment: .leading, spacing: 8) {
            if !(f.title ?? "").isEmpty || isMulti {
                HStack {
                    if let title = f.title, !title.isEmpty {
                        Text(title).font(.system(size: AppFont.body, weight: .semibold)).foregroundStyle(p.text)
                    }
                    Spacer()
                    if isMulti {
                        Text(hub.t("questionMultiHint")).font(.system(size: AppFont.caption)).foregroundStyle(p.mute)
                    }
                }
            }
            if let desc = f.description, !desc.isEmpty {
                Text(desc).font(.system(size: AppFont.secondary)).foregroundStyle(p.text2)
            }

            if f.type == "boolean" {
                HStack(spacing: 8) {
                    boolButton(f.key, value: true, label: hub.t("questionYes"))
                    boolButton(f.key, value: false, label: hub.t("questionNo"))
                }
            } else if !opts.isEmpty {
                VStack(spacing: 8) {
                    ForEach(opts, id: \.value) { opt in
                        let active = isMulti
                            ? (multis[f.key] ?? []).contains(opt.value)
                            : (!(customActive[f.key] ?? false) && singles[f.key] == opt.value)
                        Button {
                            if isMulti {
                                var list = multis[f.key] ?? []
                                if let idx = list.firstIndex(of: opt.value) { list.remove(at: idx) }
                                else { list.append(opt.value) }
                                multis[f.key] = list
                            } else {
                                customActive[f.key] = false
                                singles[f.key] = opt.value
                            }
                        } label: {
                            VStack(alignment: .leading, spacing: 2) {
                                Text((opt.label ?? "").isEmpty ? opt.value : opt.label!)
                                    .font(.system(size: AppFont.body, weight: .semibold)).foregroundStyle(p.text)
                                if let d = opt.description, !d.isEmpty {
                                    Text(d).font(.system(size: AppFont.caption)).foregroundStyle(p.text2)
                                }
                            }
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal, 12).padding(.vertical, 10)
                            .background(active ? pal.soft : p.s1, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                            .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous)
                                .stroke(active ? pal.accent : Color.clear, lineWidth: 1))
                        }
                        .buttonStyle(.plain)
                        .disabled(submitting)
                    }
                    if f.custom == true {
                        TextField(hub.t("questionCustomPlaceholder"), text: Binding(
                            get: { customText[f.key] ?? "" },
                            set: { v in
                                customText[f.key] = v
                                if !isMulti { customActive[f.key] = !v.isEmpty }
                            }
                        ))
                        .textFieldStyle(.plain)
                        .font(.system(size: AppFont.secondary))
                        .padding(.horizontal, 12).padding(.vertical, 9)
                        .background(p.s1, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                        .disabled(submitting)
                    }
                }
            } else {
                TextField(f.placeholder ?? hub.t("questionInputPlaceholder"), text: Binding(
                    get: { singles[f.key] ?? "" },
                    set: { singles[f.key] = $0 }
                ))
                .textFieldStyle(.plain)
                .font(.system(size: AppFont.secondary))
                .padding(.horizontal, 12).padding(.vertical, 9)
                .background(p.s1, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                .disabled(submitting)
            }
        }
    }

    private func boolButton(_ key: String, value: Bool, label: String) -> some View {
        let active = bools[key] == value
        return Button {
            bools[key] = value
        } label: {
            Text(label)
                .font(.system(size: AppFont.body, weight: .semibold)).foregroundStyle(p.text)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 10)
                .background(active ? pal.soft : p.s1, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .stroke(active ? pal.accent : Color.clear, lineWidth: 1))
        }
        .buttonStyle(.plain)
        .disabled(submitting)
    }

    private var canSubmit: Bool {
        question.fields.allSatisfy { f in
            guard f.required == true else { return true }
            if f.type == "multiselect" {
                let c = (customText[f.key] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                return !(multis[f.key] ?? []).isEmpty || (f.custom == true && !c.isEmpty)
            }
            if f.type == "boolean" { return bools[f.key] != nil }
            if !(f.options ?? []).isEmpty, customActive[f.key] == true {
                return !(customText[f.key] ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            }
            return !(singles[f.key] ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        }
    }

    private func seedDefaults() {
        var s: [String: String] = [:]
        var m: [String: [String]] = [:]
        var b: [String: Bool] = [:]
        for f in question.fields {
            if f.type == "multiselect" {
                m[f.key] = f.default?.stringArrayValue ?? []
            } else if f.type == "boolean" {
                if let bv = f.default?.boolValue { b[f.key] = bv }
            } else if let sv = f.default?.stringValue {
                s[f.key] = sv
            }
        }
        singles = s
        multis = m
        bools = b
        customText = [:]
        customActive = [:]
        submitting = false
    }

    private func submit(cancelled: Bool) {
        guard !submitting else { return }
        submitting = true
        var answers: [String: Any] = [:]
        if !cancelled {
            for f in question.fields {
                if f.type == "multiselect" {
                    var list = multis[f.key] ?? []
                    let c = (customText[f.key] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                    if f.custom == true, !c.isEmpty, !list.contains(c) { list.append(c) }
                    answers[f.key] = list
                } else if f.type == "boolean" {
                    answers[f.key] = bools[f.key] ?? false
                } else if f.type == "integer" {
                    let raw = (singles[f.key] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                    if let n = Int(raw) { answers[f.key] = n }
                } else if f.type == "number" {
                    let raw = (singles[f.key] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                    if let n = Double(raw) { answers[f.key] = n }
                } else if !(f.options ?? []).isEmpty, customActive[f.key] == true {
                    answers[f.key] = (customText[f.key] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                } else {
                    answers[f.key] = singles[f.key] ?? ""
                }
            }
        }
        Task {
            try? await hub.replyQuestion(id: question.id, answers: answers, cancelled: cancelled)
            submitting = false
        }
    }
}

struct ApprovalCardView: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    var approval: PendingApproval
    var pal: (accent: Color, text: Color, soft: Color)

    @State private var reason = ""
    @State private var showRaw = false
    @State private var submitting = false
    @State private var err = ""

    private var kindColors: (bg: Color, fg: Color) {
        switch approval.kind ?? "other" {
        case "shell": return (p.pink, p.pinkInk)
        case "write": return (p.lilac, p.lilacInk)
        case "read": return (p.sage, p.sageInk)
        default: return (p.s1, p.text2)
        }
    }

    private var kindLabel: String {
        let k = (approval.kind ?? "").isEmpty ? "other" : approval.kind!
        let key = "permKind_\(k)"
        let t = hub.t(key)
        return t == key ? k : t
    }

    private var opts: [PermissionOption] { approval.options ?? [] }
    private var allowOnceOpt: PermissionOption? { opts.first(where: { $0.kind == "allow_once" }) }
    private var allowAlwaysOpt: PermissionOption? { opts.first(where: { $0.kind == "allow_always" }) }
    private var rejectOpt: PermissionOption? { opts.first(where: { $0.kind == "reject" }) }

    var body: some View {
        let kc = kindColors
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .center, spacing: 8) {
                Text(kindLabel)
                    .font(.system(size: AppFont.caption, weight: .semibold))
                    .foregroundStyle(kc.fg)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 3)
                    .background(kc.bg, in: Capsule())

                Text(approval.toolName)
                    .font(.system(size: AppFont.cardTitle, weight: .semibold, design: .monospaced))
                    .foregroundStyle(p.text)
                    .lineLimit(2)

                Spacer(minLength: 8)

                Text(hub.t("approvalWaitingBadge"))
                    .font(.system(size: AppFont.caption, weight: .semibold))
                    .foregroundStyle(p.busy)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 3)
                    .background(p.busy.opacity(0.16), in: Capsule())
            }

            if let title = approval.title?.trimmingCharacters(in: .whitespacesAndNewlines),
               !title.isEmpty, title != approval.toolName {
                Text(title)
                    .font(.system(size: AppFont.body, weight: .medium))
                    .foregroundStyle(p.text)
            }

            if let summary = approval.summary?.trimmingCharacters(in: .whitespacesAndNewlines),
               !summary.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    Text(summary)
                        .font(.system(size: AppFont.secondary, design: .monospaced))
                        .foregroundStyle(p.text)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .padding(10)
                .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).stroke(p.s2, lineWidth: 0.5))
            }

            if let rawPretty = approval.rawInputPretty, !rawPretty.isEmpty {
                DisclosureGroup(isExpanded: $showRaw) {
                    ScrollView(.horizontal, showsIndicators: false) {
                        Text(rawPretty)
                            .font(.system(size: AppFont.caption, design: .monospaced))
                            .foregroundStyle(p.text2)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .padding(10)
                    .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).stroke(p.s2, lineWidth: 0.5))
                    .padding(.top, 4)
                } label: {
                    Text(hub.t("approvalParams"))
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(p.text2)
                }
            }

            TextField("", text: $reason, prompt: Text(hub.t("approvalReasonPh")))
                .font(.system(size: AppFont.body))
                .disabled(submitting)
                .padding(.horizontal, 10)
                .padding(.vertical, 8)
                .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).stroke(p.s2, lineWidth: 0.5))

            if !err.isEmpty {
                StatusText(text: err)
            }

            ViewThatFits(in: .horizontal) {
                HStack(spacing: 8) {
                    Spacer(minLength: 0)
                    actionButtons
                }
                FlowLayout(spacing: 8) {
                    actionButtons
                }
            }
        }
        .padding(16)
        .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .onChange(of: approval.id) { _, _ in
            reason = ""
            showRaw = false
            submitting = false
            err = ""
        }
    }

    @ViewBuilder
    private var actionButtons: some View {
        Button(hub.t("approvalReject")) {
            let trimmed = reason.trimmingCharacters(in: .whitespacesAndNewlines)
            submit("reject", optionId: rejectOpt?.id, denyReason: trimmed.isEmpty ? nil : trimmed)
        }
        .buttonStyle(.bordered)
        .disabled(submitting)

        if let alwaysOpt = allowAlwaysOpt {
            Button(hub.t("approvalAllowAlways")) {
                submit("allow_always", optionId: alwaysOpt.id, denyReason: nil)
            }
            .buttonStyle(.bordered)
            .disabled(submitting)
        }

        Button(hub.t("approvalAllowOnce")) {
            submit("allow_once", optionId: allowOnceOpt?.id, denyReason: nil)
        }
        .buttonStyle(.borderedProminent)
        .tint(pal.accent)
        .disabled(submitting)
    }

    private func submit(_ decision: String, optionId: String?, denyReason: String?) {
        guard !submitting else { return }
        submitting = true
        err = ""
        Task {
            do {
                try await hub.replyApproval(id: approval.id, decision: decision, optionId: optionId, reason: denyReason)
            } catch {
                err = errText(error, hub.t("updateFailed"))
            }
            submitting = false
        }
    }
}
