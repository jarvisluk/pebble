import SwiftUI

/// 右侧工作面板 Inspector：仅保留无外框电脑屏幕实时预览与定时任务（Routines）。
struct BotDetailView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.hubWindowVisible) private var windowVisible
    var botId: String
    @Binding var isPresented: Bool

    @State private var previewStream = H264StreamController()
    @State private var previewUseFrame = false
    @State private var previewFrameImage: PlatformImage?
    @State private var previewFrameError = ""

    private var effectiveTarget: String {
        if let to = hub.takeovers.first(where: { $0.botId == botId }), let t = to.target, !t.isEmpty {
            return t
        }
        if let bound = hub.bots.first(where: { $0.id == botId })?.desktopTarget, !bound.isEmpty {
            return bound
        }
        let active = hub.activeDesktopTarget.target
        return active.isEmpty ? "local" : active
    }

    private var shouldUsePreviewFrame: Bool {
        !HubStore.isLocalDesktopTarget(effectiveTarget) || !hub.supports("desktop.ws_h264") || previewUseFrame
    }

    private var targetBadgeLabel: String {
        if let matched = hub.desktopTargets.first(where: { $0.id == effectiveTarget }), let name = matched.name, !name.isEmpty {
            return name
        }
        return effectiveTarget
    }

    var body: some View {
        HubSheetScaffold(
            title: hub.t("rightPanelTitle"),
            onClose: { isPresented = false },
            enableCancelShortcut: false
        ) {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    desktopPreviewCard
                    RoutinesSection(botId: botId)
                    BotTasksSection(
                        botId: botId,
                        onSelectTask: { task in
                            router.openTask(botId: botId, task: task)
                        }
                    )
                }
                .padding(.horizontal, 16)
                .padding(.top, 6)
                .padding(.bottom, 24)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .scrollDismissesKeyboard(.interactively)
            .background(p.page.ignoresSafeArea())
        }
        .task(id: botId) {
            if hub.isConnected && hub.desktopTargets.isEmpty {
                _ = try? await hub.refreshDesktopTargets()
            }
            try? await hub.fetchRoutines(botId)
        }
        .onChange(of: effectiveTarget) { _, _ in
            previewUseFrame = false
            previewFrameImage = nil
            previewFrameError = ""
        }
        .onChange(of: previewStream.preFirstFrameFailureSeq) { _, _ in
            if windowVisible && hub.isConnected {
                previewUseFrame = true
            }
        }
        .task(id: "\(botId)-\(hub.isConnected)-\(windowVisible)-\(effectiveTarget)-\(shouldUsePreviewFrame)") {
            guard windowVisible, hub.isConnected else {
                previewStream.stop()
                return
            }
            if shouldUsePreviewFrame {
                previewStream.stop()
                await pollPreviewFrames()
                return
            }
            previewFrameImage = nil
            previewFrameError = ""
            let codec = hub.supports("desktop.ws_hevc") ? "hevc" : "h264"
            guard let req = hub.makeDesktopWSRequest(target: effectiveTarget, preset: "preview", codec: codec) else {
                previewStream.stop()
                return
            }
            previewStream.start(request: req, trackFPS: false)
        }
        .onDisappear { previewStream.stop() }
    }

    private func pollPreviewFrames() async {
        while !Task.isCancelled && windowVisible && hub.isConnected && shouldUsePreviewFrame {
            do {
                let fr = try await hub.fetchDesktopFrame(effectiveTarget)
                if Task.isCancelled || !windowVisible { break }
                let img: PlatformImage? = await Task.detached(priority: .utility) {
                    guard !fr.jpegBase64.isEmpty, let data = Data(base64Encoded: fr.jpegBase64) else { return nil }
                    return PlatformImage(data: data)
                }.value
                if Task.isCancelled || !windowVisible { break }
                if let img {
                    previewFrameImage = img
                    if !previewFrameError.isEmpty { previewFrameError = "" }
                }
            } catch {
                if Task.isCancelled { break }
                if let he = error as? HubError, he.code == "screen_recording_denied" {
                    previewFrameError = hub.t("deskScreenRecordingDenied")
                    break
                }
                if previewFrameImage == nil {
                    previewFrameError = errText(error, hub.t("connFailed"))
                }
            }
            try? await Task.sleep(nanoseconds: 2_500_000_000)
        }
    }

    private var desktopPreviewCard: some View {
        let hasFrame = shouldUsePreviewFrame && previewFrameImage != nil
        let isReady = hasFrame || previewStream.connected
        let displayErr = shouldUsePreviewFrame ? previewFrameError : previewStream.error
        return Button {
            router.openDesktop(effectiveTarget)
        } label: {
            ZStack(alignment: .topTrailing) {
                ZStack {
                    Color.black
                    H264DisplayView(controller: previewStream)
                    if let img = previewFrameImage, shouldUsePreviewFrame {
                        Image(platform: img)
                            .resizable()
                            .aspectRatio(contentMode: .fit)
                    }
                    if !isReady {
                        VStack(spacing: 6) {
                            if !displayErr.isEmpty {
                                Image(systemName: "display")
                                    .font(.system(size: AppFont.hero))
                                    .foregroundStyle(.secondary)
                                Text(displayErr)
                                    .font(.system(size: AppFont.caption))
                                    .foregroundStyle(.secondary)
                                    .multilineTextAlignment(.center)
                                    .lineLimit(2)
                                    .padding(.horizontal, 12)
                            } else {
                                ProgressView()
                                    .tint(.white)
                            }
                        }
                    }
                }
                .frame(minWidth: 0, maxWidth: .infinity)
                .aspectRatio(16.0 / 10.0, contentMode: .fit)
                .clipped()

                if effectiveTarget != "local" {
                    Text(targetBadgeLabel)
                        .font(.system(size: AppFont.caption, weight: .medium))
                        .foregroundStyle(.white)
                        .lineLimit(1)
                        .padding(.horizontal, 9)
                        .padding(.vertical, 4)
                        .background(.black.opacity(0.60), in: Capsule())
                        .padding(10)
                        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottomLeading)
                }

                Image(systemName: "arrow.up.left.and.arrow.down.right")
                    .font(.system(size: AppFont.caption, weight: .semibold))
                    .foregroundStyle(.white)
                    .padding(7)
                    .background(.black.opacity(0.55), in: Circle())
                    .padding(10)
            }
            .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        }
        .buttonStyle(.plain)
    }
}
