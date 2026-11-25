import SwiftUI

struct SettingsView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.hubCompact) private var compact
    @Environment(\.dismiss) private var dismiss

    var tab: SettingsTab = .general

    @State private var saving = false
    @State private var error = ""
    @State private var saved = ""
    @State private var confirm: ConfirmItem?
    @State private var showUnsavedAlert = false
    @State private var alertTargetTab: SettingsTab? = nil
    @State private var alertLeaveRequested = false

    var body: some View {
        Form {
            if !error.isEmpty {
                Section { StatusText(text: error) }
            }
            if !saved.isEmpty {
                Section { StatusText(text: saved, error: false) }
            }
            switch tab {
            case .general:
                SettingsGeneralTab()
            case .models:
                SettingsModelsTab(error: $error, saved: $saved, confirm: $confirm)
            case .memory:
                SettingsMemoryTab(error: $error, saved: $saved, confirm: $confirm)
            case .desktop:
                SettingsDesktopTab(error: $error, saved: $saved)
            case .remote:
                SettingsNetworkTab(error: $error, saved: $saved, confirm: $confirm)
            }
        }
        .hubForm()
        .navigationTitle(hub.t(tab.titleKey))
        .inlineNavTitle()
        .navigationBarBackButtonHidden(compact && router.settingsDirty)
        .toolbar {
            if compact && router.settingsDirty {
                ToolbarItem(placement: .hubLeading) {
                    Button {
                        router.requestLeaveSettings = true
                    } label: {
                        HStack(spacing: 4) {
                            Image(systemName: "chevron.left")
                            Text(hub.t("back"))
                        }
                    }
                }
            }
            if tab == .models && hub.isConnected {
                ToolbarItem(placement: .confirmationAction) {
                    if saving {
                        ProgressView().controlSize(.small)
                    } else {
                        Button(hub.t("save")) {
                            Task {
                                saving = true
                                _ = await router.settingsSaveAction?()
                                saving = false
                            }
                        }
                        .disabled(!router.settingsDirty)
                    }
                }
            }
        }
        .alert(hub.t("unsavedTitle"), isPresented: $showUnsavedAlert) {
            Button(hub.t(alertTargetTab != nil ? "saveAndSwitch" : "saveAndLeave")) {
                let target = alertTargetTab
                let leaving = alertLeaveRequested
                clearUnsavedPrompt()
                Task {
                    saving = true
                    let ok = await router.settingsSaveAction?() ?? true
                    saving = false
                    guard ok else { return }
                    if let target {
                        router.settingsTab = target
                    } else if leaving {
                        finishLeave()
                    }
                }
            }
            Button(hub.t("discardChanges"), role: .destructive) {
                let target = alertTargetTab
                let leaving = alertLeaveRequested
                clearUnsavedPrompt()
                router.settingsDiscardAction?()
                router.settingsDirty = false
                if let target {
                    router.settingsTab = target
                } else if leaving {
                    finishLeave()
                }
            }
            Button(hub.t("cancel"), role: .cancel) {
                clearUnsavedPrompt()
            }
        } message: {
            Text(hub.t("unsavedDesc"))
        }
        .confirmAlert($confirm)
        .onDisappear {
            router.clearSettingsDirty()
        }
        .onChange(of: tab) { _, _ in
            error = ""
            saved = ""
        }
        .onChange(of: router.requestLeaveSettings) { _, req in
            if req {
                router.requestLeaveSettings = false
                alertLeaveRequested = true
                alertTargetTab = nil
                showUnsavedAlert = true
            }
        }
        .onChange(of: router.pendingSettingsTab) { _, next in
            if let next {
                router.pendingSettingsTab = nil
                alertTargetTab = next
                alertLeaveRequested = false
                showUnsavedAlert = true
            }
        }
    }

    private func clearUnsavedPrompt() {
        showUnsavedAlert = false
        alertTargetTab = nil
        alertLeaveRequested = false
        router.requestLeaveSettings = false
        router.pendingSettingsTab = nil
    }

    private func finishLeave() {
        if compact {
            router.clearSettingsDirty()
            dismiss()
        } else {
            router.leaveSettings()
        }
    }
}
