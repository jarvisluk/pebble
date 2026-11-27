import SwiftUI

extension SettingsMemoryTab {
    // MARK: - Section 3: Pebble Memory Settings & Maintenance

    var settingsMaintenanceSection: some View {
        let semActive = effectiveSemantic?.active == true
        return Section {
            VStack(alignment: .leading, spacing: 6) {
                FlowLayout(spacing: 6) {
                    Tag(text: hub.t("memBackendPebble"), bg: p.sage, fg: p.sageInk)
                    Tag(
                        text: hub.t(semActive ? "memSemanticOn" : "memSemanticOff"),
                        bg: semActive ? p.sage : p.s1,
                        fg: semActive ? p.sageInk : .secondary
                    )
                    if let docs = memStatus?.docCount, let chunks = memStatus?.chunkCount {
                        Tag(
                            text: hub.t("memStatsSummary", ["docs": docs, "chunks": chunks]),
                            bg: p.s1,
                            fg: p.text2
                        )
                    }
                }
                if !semActive {
                    Text(hub.t("memSemanticHintOff"))
                        .font(.system(size: AppFont.caption))
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if let latest = memStatus?.recentDreams?.first, let date = latest.date, !date.isEmpty {
                    Text(hub.t("memDreamRecent", ["date": date]))
                        .font(.system(size: AppFont.caption))
                        .foregroundStyle(.secondary)
                }
            }

            VStack(alignment: .leading, spacing: 6) {
                HubFormField(label: hub.t("memCoexistence")) {
                    HubSegmentedPicker(
                        selection: cfgBinding($cfgCoexistence),
                        options: [
                            ("auto", hub.t("memCoexistAuto")),
                            ("isolated", hub.t("memCoexistIsolated")),
                        ]
                    )
                }
                Text(hub.t("memCoexistHint"))
                    .font(.system(size: AppFont.caption))
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }

            Toggle(hub.t("memDisableClaudeAuto"), isOn: cfgBinding($cfgDisableClaudeAuto))

            Toggle(hub.t("memDreamEnabled"), isOn: cfgBinding($cfgDreamEnabled))

            if cfgDreamEnabled {
                HubFormField(label: hub.t("memDreamDailyAt")) {
                    TextField(hub.t("memDreamDailyAt"), text: cfgBinding($cfgDreamDailyAt), prompt: Text(hub.t("memDreamDailyAtPh")))
                        .monoInput()
                }
                HubFormField(label: hub.t("memDreamModel")) {
                    TextField(hub.t("memDreamModel"), text: cfgBinding($cfgDreamModel), prompt: Text(hub.t("memDreamModelPh")))
                        .monoInput()
                }
                HubFormField(label: hub.t("memDreamMinMsgs")) {
                    TextField(hub.t("memDreamMinMsgs"), text: cfgBinding($cfgDreamMinMsgs), prompt: Text(hub.t("memDreamMinMsgsPh")))
                        .monoInput()
                        .numberPadKeyboard()
                }
            }

            DisclosureGroup(hub.t("memEmbeddingTitle"), isExpanded: $embedExpanded) {
                let existingEmbed = (hub.settings?.memory ?? memStatus?.config)?.embedding
                let hasSavedEmbedKey = (existingEmbed?.hasApiKey == true)
                    || !(existingEmbed?.apiKey ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                VStack(alignment: .leading, spacing: 10) {
                    Toggle(hub.t("memEmbeddingEnabled"), isOn: cfgBinding($cfgEmbedEnabled))
                    HubFormField(label: hub.t("memEmbeddingBaseUrl")) {
                        TextField(hub.t("memEmbeddingBaseUrl"), text: cfgBinding($cfgEmbedBaseUrl), prompt: Text(hub.t("memEmbeddingBaseUrlPh")))
                            .monoInput()
                            .padding(.horizontal, 10)
                            .padding(.vertical, 7)
                            .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                    }
                    HubFormField(label: hub.t("memEmbeddingApiKey")) {
                        SecureField(
                            hub.t("memEmbeddingApiKey"),
                            text: Binding(
                                get: { cfgEmbedApiKey },
                                set: {
                                    guard $0 != cfgEmbedApiKey else { return }
                                    cfgEmbedApiKey = $0
                                    cfgEmbedApiKeyTouched = true
                                    cfgTouched = true
                                    saved = ""
                                }
                            ),
                            prompt: Text(hub.t(hasSavedEmbedKey ? "memEmbeddingApiKeySetPh" : "memEmbeddingApiKeyPh"))
                        )
                        .monoInput()
                        .padding(.horizontal, 10)
                        .padding(.vertical, 7)
                        .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                    }
                    HubFormField(label: hub.t("memEmbeddingModel")) {
                        TextField(hub.t("memEmbeddingModel"), text: cfgBinding($cfgEmbedModel), prompt: Text(hub.t("memEmbeddingModelPh")))
                            .monoInput()
                            .padding(.horizontal, 10)
                            .padding(.vertical, 7)
                            .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                    }
                }
                .padding(.top, 8)
            }

            HStack {
                FlowLayout(spacing: 8) {
                    MemoryChipButton(
                        title: busy == "mem:dream" ? hub.t("memDreamRunning") : hub.t("memDreamRun"),
                        systemImage: "sparkles",
                        disabled: !busy.isEmpty,
                        action: {
                            run("mem:dream", "opFailed") {
                                _ = try await hub.triggerMemoryDream()
                                await reloadAll()
                                return hub.t("memDreamDone")
                            }
                        }
                    )
                    MemoryChipButton(
                        title: busy == "mem:reindex" ? hub.t("memReindexing") : hub.t("memReindex"),
                        systemImage: "arrow.triangle.2.circlepath",
                        disabled: !busy.isEmpty,
                        action: {
                            run("mem:reindex", "opFailed") {
                                _ = try await hub.triggerMemoryReindex()
                                await reloadAll()
                                return hub.t("memReindexDone")
                            }
                        }
                    )
                }
                Spacer(minLength: 8)
                Button {
                    Task { _ = await saveMemorySettings() }
                } label: {
                    HubLoadingLabel(loading: cfgSaving) { Text(hub.t("save")) }
                }
                .buttonStyle(HubPrimaryButtonStyle())
                .disabled(!cfgTouched || !busy.isEmpty || cfgSaving)
            }

            if effectiveMagpie?.installed == true {
                VStack(alignment: .leading, spacing: 8) {
                    Text(hub.t("memImportTitle"))
                        .font(.system(size: AppFont.secondary, weight: .semibold))
                        .foregroundStyle(p.text)
                    let legacyCount = effectiveMagpie?.legacyCount ?? 0
                    Text(legacyCount > 0
                        ? hub.t("memImportHint", ["count": legacyCount])
                        : hub.t("memImportHintGeneric"))
                        .font(.system(size: AppFont.caption))
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                    FlowLayout(spacing: 8) {
                        MemoryChipButton(
                            title: busy == "mem:import:bots" ? hub.t("memImportImporting") : hub.t("memImportBotsOnly"),
                            systemImage: "square.and.arrow.down",
                            disabled: !busy.isEmpty,
                            action: { importMagpie(mode: "bots_only", key: "mem:import:bots") }
                        )
                        MemoryChipButton(
                            title: busy == "mem:import:all" ? hub.t("memImportImporting") : hub.t("memImportAll"),
                            systemImage: "tray.and.arrow.down",
                            disabled: !busy.isEmpty,
                            action: { importMagpie(mode: "all", key: "mem:import:all") }
                        )
                    }
                }
            }
        } header: {
            Text(hub.t("memSettingsTitle"))
        }
    }

    func cfgBinding<T: Equatable>(_ binding: Binding<T>) -> Binding<T> {
        Binding(
            get: { binding.wrappedValue },
            set: {
                guard $0 != binding.wrappedValue else { return }
                binding.wrappedValue = $0
                cfgTouched = true
                saved = ""
            }
        )
    }

    func syncMemoryConfig() {
        guard !cfgTouched else { return }
        let cfg = hub.settings?.memory ?? memStatus?.config
        cfgCoexistence = (cfg?.coexistence == "isolated") ? "isolated" : "auto"
        cfgDisableClaudeAuto = cfg?.disableClaudeAutoMemory ?? false
        cfgDreamEnabled = cfg?.dream?.enabled ?? true
        cfgDreamDailyAt = (cfg?.dream?.dailyAt ?? "").isEmpty ? "03:00" : (cfg?.dream?.dailyAt ?? "03:00")
        cfgDreamModel = cfg?.dream?.model ?? ""
        if let mins = cfg?.dream?.minNewMessages, mins > 0 {
            cfgDreamMinMsgs = "\(mins)"
        } else {
            cfgDreamMinMsgs = ""
        }
        cfgEmbedEnabled = cfg?.embedding?.enabled ?? false
        cfgEmbedBaseUrl = cfg?.embedding?.baseUrl ?? ""
        cfgEmbedApiKey = ""
        cfgEmbedApiKeyTouched = false
        cfgEmbedModel = cfg?.embedding?.model ?? ""
        if cfgEmbedEnabled {
            embedExpanded = true
        }
    }

    func saveMemorySettings() async -> Bool {
        error = ""
        saved = ""
        cfgSaving = true
        defer { cfgSaving = false }
        var dreamObj: [String: Any] = [
            "enabled": cfgDreamEnabled,
            "dailyAt": cfgDreamDailyAt.trimmingCharacters(in: .whitespacesAndNewlines),
            "model": cfgDreamModel.trimmingCharacters(in: .whitespacesAndNewlines),
        ]
        if let n = Int(cfgDreamMinMsgs.trimmingCharacters(in: .whitespacesAndNewlines)), n > 0 {
            dreamObj["minNewMessages"] = n
        }
        var embedObj: [String: Any] = [
            "enabled": cfgEmbedEnabled,
            "baseUrl": cfgEmbedBaseUrl.trimmingCharacters(in: .whitespacesAndNewlines),
            "model": cfgEmbedModel.trimmingCharacters(in: .whitespacesAndNewlines),
        ]
        let keyTrimmed = cfgEmbedApiKey.trimmingCharacters(in: .whitespacesAndNewlines)
        if cfgEmbedApiKeyTouched && !keyTrimmed.isEmpty {
            embedObj["apiKey"] = keyTrimmed
        }
        let existingCfg = hub.settings?.memory ?? memStatus?.config
        var body: [String: Any] = [
            "enabled": existingCfg?.enabled ?? true,
            "coexistence": cfgCoexistence,
            "disableClaudeAutoMemory": cfgDisableClaudeAuto,
            "dream": dreamObj,
            "embedding": embedObj,
        ]
        if let maxChars = existingCfg?.snapshotMaxChars, maxChars > 0 {
            body["snapshotMaxChars"] = maxChars
        }
        do {
            try await hub.saveMemoryConfig(body)
            cfgTouched = false
            cfgEmbedApiKey = ""
            cfgEmbedApiKeyTouched = false
            syncMemoryConfig()
            await loadStatus()
            saved = hub.t("memConfigSaved")
            return true
        } catch {
            self.error = errText(error, hub.t("saveFailed"))
            return false
        }
    }

    func importMagpie(mode: String, key: String) {
        run(key, "opFailed") {
            let resp = try await hub.importMemoryFromMagpie(mode: mode)
            await reloadAll()
            return hub.t("memImportDone", ["imported": resp.imported ?? 0, "skipped": resp.skipped ?? 0])
        }
    }
}
