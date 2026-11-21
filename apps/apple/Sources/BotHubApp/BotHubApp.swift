import SwiftUI
#if os(macOS)
import AppKit
#endif

@main
struct BotHubApp: App {
    @AppStorage("bothub.serverURL") private var serverURL = "http://127.0.0.1:3789"
    @State private var reloadToken = UUID()
    @State private var showServerSheet = false

    var body: some Scene {
        WindowGroup("Bot Hub") {
            RootView(
                serverURL: $serverURL,
                reloadToken: reloadToken,
                showServerSheet: $showServerSheet
            )
            .background(Color(red: 0.937, green: 0.925, blue: 0.910))
            #if os(macOS)
            .frame(minWidth: 680, minHeight: 520)
            .background(WindowAccessor())
            #endif
        }
        #if os(macOS)
        .windowStyle(.hiddenTitleBar)
        .commands {
            CommandGroup(after: .appSettings) {
                Button("切换服务端地址…") { showServerSheet = true }
                    .keyboardShortcut(",", modifiers: .command)
                Button("刷新页面") { reloadToken = UUID() }
                    .keyboardShortcut("r", modifiers: .command)
            }
        }
        #endif
    }
}

private struct RootView: View {
    @Binding var serverURL: String
    let reloadToken: UUID
    @Binding var showServerSheet: Bool
    @State private var draftURL = ""
    @State private var loadError: String?

    var body: some View {
        ZStack {
            if let url = normalizedURL(serverURL) {
                WebView(url: url, reloadToken: reloadToken, loadError: $loadError)
                    .ignoresSafeArea()
            } else {
                ContentUnavailableView("服务端地址无效", systemImage: "exclamationmark.triangle")
            }

            if let loadError {
                VStack(spacing: 12) {
                    Text("无法连接到 Bot Hub")
                        .font(.headline)
                    Text(loadError)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                    HStack(spacing: 10) {
                        Button("修改地址") {
                            draftURL = serverURL
                            showServerSheet = true
                        }
                        Button("重试") {
                            self.loadError = nil
                            // Trigger WebView reload via sheet or URL toggle
                            let current = serverURL
                            serverURL = ""
                            DispatchQueue.main.async { serverURL = current }
                        }
                        .buttonStyle(.borderedProminent)
                    }
                }
                .padding(24)
                .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 20))
                .padding()
            }
        }
        .sheet(isPresented: $showServerSheet) {
            NavigationStack {
                Form {
                    Section("Bot Hub 地址") {
                        TextField("http://127.0.0.1:3789", text: $draftURL)
                            #if os(iOS)
                            .keyboardType(.URL)
                            .textInputAutocapitalization(.never)
                            #endif
                            .autocorrectionDisabled()
                    }
                    Section {
                        Text("本机调试默认使用 http://127.0.0.1:3789（可与 Wails 桌面端共用同一个后台服务）。")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
                .navigationTitle("连接设置")
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("取消") { showServerSheet = false }
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("保存") {
                            let trimmed = draftURL.trimmingCharacters(in: .whitespacesAndNewlines)
                            if !trimmed.isEmpty {
                                serverURL = trimmed
                                loadError = nil
                            }
                            showServerSheet = false
                        }
                    }
                }
            }
            .frame(minWidth: 340, minHeight: 200)
        }
        .onAppear {
            draftURL = serverURL
        }
    }

    private func normalizedURL(_ raw: String) -> URL? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        if trimmed.hasPrefix("http://") || trimmed.hasPrefix("https://") {
            return URL(string: trimmed)
        }
        return URL(string: "http://" + trimmed)
    }
}

#if os(macOS)
private struct WindowAccessor: NSViewRepresentable {
    func makeNSView(context: Context) -> NSView {
        let view = NSView()
        DispatchQueue.main.async {
            guard let window = view.window else { return }
            window.titleVisibility = .hidden
            window.titlebarAppearsTransparent = true
            window.styleMask.insert(.fullSizeContentView)
            window.isMovableByWindowBackground = true
        }
        return view
    }
    func updateNSView(_ nsView: NSView, context: Context) {}
}
#endif
