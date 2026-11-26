import Foundation

extension HubStore {
    // MARK: 设置

    @discardableResult
    func loadSettings(fresh: Bool = false) async throws -> SettingsData {
        let s: SettingsData = try await callAPI(fresh ? "/api/settings?fresh=1" : "/api/settings")
        settings = s
        return s
    }

    @discardableResult
    func loadModels(_ harness: String, force: Bool = false) async throws -> [String] {
        guard !harness.isEmpty else { return [] }
        if !force, let cached = models[harness], !cached.isEmpty {
            return cached
        }
        if force {
            modelLoadTasks[harness]?.1.cancel()
            modelLoadTasks.removeValue(forKey: harness)
        } else if let existing = modelLoadTasks[harness] {
            return try await existing.1.value
        }
        let token = UUID()
        let task = Task<[String], Error> {
            let d: ModelsResp = try await self.callAPI("/api/models?harness=\(enc(harness))")
            return d.models ?? []
        }
        modelLoadTasks[harness] = (token, task)
        defer {
            if modelLoadTasks[harness]?.0 == token {
                modelLoadTasks.removeValue(forKey: harness)
            }
        }
        let list = try await task.value
        if !list.isEmpty || force || models[harness] == nil {
            models[harness] = list
        }
        return models[harness] ?? list
    }

    func loadMagpieModels(_ harness: String) async throws -> [String] {
        guard !harness.isEmpty else { return [] }
        let d: ModelsResp = try await callAPI("/api/models?harness=\(enc(harness))&source=magpie")
        return d.models ?? []
    }

    func resetModelsCache() {
        for (_, entry) in modelLoadTasks { entry.1.cancel() }
        modelLoadTasks.removeAll()
        models = [:]
        modelsGeneration += 1
    }

    func saveModelsSettings(
        changed: Bool,
        defaultHarness: String,
        defaultPermissionMode: String? = nil,
        defaultModel: String,
        defaultModels: [String: String],
        magpieWebUrl: String?,
        botUpdates: [(id: String, harness: String, model: String)]
    ) async throws {
        if changed {
            var body: [String: Any] = [
                "defaultHarness": defaultHarness,
                "defaultModel": defaultModel,
                "defaultModels": defaultModels,
            ]
            if let defaultPermissionMode, !defaultPermissionMode.isEmpty {
                body["defaultPermissionMode"] = defaultPermissionMode
            }
            if let magpieWebUrl {
                body["magpieWebUrl"] = magpieWebUrl
            }
            settings = try await callAPI("/api/settings", method: "PUT", body: body)
        }
        for u in botUpdates { try await updateBot(u.id, ["harness": u.harness, "model": u.model]) }
        try await loadSettings()
        try await refreshBots()
    }

    func saveSharedUserMd(_ content: String) async throws {
        settings = try await callAPI("/api/settings", method: "PUT", body: ["sharedUserMd": content])
    }

    func startHarnessInstall(_ harness: String) async throws -> SetupTaskStatus {
        try await callAPI("/api/harnesses/\(enc(harness))/install", method: "POST", body: [:])
    }

    func harnessInstallStatus(_ harness: String) async throws -> SetupTaskStatus {
        try await callAPI("/api/harnesses/\(enc(harness))/install")
    }

    func startHarnessLogin(_ harness: String) async throws -> SetupTaskStatus {
        try await callAPI("/api/harnesses/\(enc(harness))/login", method: "POST", body: [:])
    }

    func harnessLoginStatus(_ harness: String) async throws -> SetupTaskStatus {
        try await callAPI("/api/harnesses/\(enc(harness))/login")
    }

    func connectMagpie(_ harness: String, model: String) async throws {
        settings = try await callAPI("/api/harnesses/\(enc(harness))/magpie", method: "POST", body: ["action": "connect", "model": model])
        resetModelsCache()
    }

    func disconnectMagpie(_ harness: String) async throws {
        settings = try await callAPI("/api/harnesses/\(enc(harness))/magpie", method: "POST", body: ["action": "disconnect"])
        resetModelsCache()
    }

    func openMagpie() async throws {
        let _: OkResp = try await callAPI("/api/magpie/open", method: "POST", body: [:])
    }

    func installMagpie() async throws {
        var st: SetupTaskStatus = try await callAPI("/api/magpie/install", method: "POST", body: [:])
        while st.state == "running" {
            try await Task.sleep(nanoseconds: 1_200_000_000)
            st = try await callAPI("/api/magpie/install")
        }
        if st.state == "error" {
            throw NSError(domain: "Pebble", code: 400, userInfo: [NSLocalizedDescriptionKey: st.error ?? "Magpie install failed"])
        }
        _ = try await loadSettings()
        resetModelsCache()
    }

    func setDesktopWorker(harness: String, model: String) async throws {
        settings = try await callAPI("/api/settings", method: "PUT",
                                      body: ["desktopHarness": harness.trimmingCharacters(in: .whitespaces),
                                             "desktopModel": model.trimmingCharacters(in: .whitespaces)])
    }

    func harnessInfo(_ id: String?) -> HarnessInfo? {
        guard let id, !id.isEmpty else { return nil }
        return settings?.harnesses?.first(where: { $0.id == id })
    }

    func supportedPermissionModes(for harnessId: String?) -> [String] {
        harnessInfo(harnessId)?.supportedModes ?? ["auto"]
    }

    func clampPermissionMode(_ raw: String?, harnessId: String?) -> String {
        let modes = supportedPermissionModes(for: harnessId)
        let cur = (raw ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        if !cur.isEmpty && modes.contains(cur) { return cur }
        return modes.contains("auto") ? "auto" : (modes.first ?? "auto")
    }
}
