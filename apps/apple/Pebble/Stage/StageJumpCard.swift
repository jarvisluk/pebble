import SwiftUI

struct StageJumpCard: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var ref: StageRef
    var inSidebar: Bool = false
    var onOpen: () -> Void

    private var kind: StageKind { ref.resolvedKind }

    private var titleText: String {
        let raw = (ref.title ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        if !raw.isEmpty { return raw }
        switch kind {
        case .files: return hub.t("stageFilesTab")
        case .desktop: return hub.t("stageDesktopTab")
        default:
            let base = StageStateOps.basename(of: ref.path)
            return base.isEmpty ? ref.kind : base
        }
    }

    private var subtitleText: String {
        let raw = (ref.subtitle ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        if !raw.isEmpty { return raw }
        if let path = ref.path, !path.isEmpty { return path }
        if let url = ref.url, !url.isEmpty { return url }
        if let uri = ref.resourceUri, !uri.isEmpty { return uri }
        if let target = ref.target, !target.isEmpty, target != "local" { return target }
        return hub.t("stageCardSubmitted")
    }

    var body: some View {
        Button {
            onOpen()
        } label: {
            HStack(spacing: 10) {
                Image(systemName: StageStateOps.symbolName(for: kind, rawKind: ref.kind))
                    .font(.system(size: AppFont.body, weight: .medium))
                    .foregroundStyle(p.text)
                    .frame(width: 32, height: 32)
                    .background(inSidebar ? p.card : p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))

                VStack(alignment: .leading, spacing: 1) {
                    Text(titleText)
                        .font(.system(size: AppFont.body, weight: .semibold))
                        .foregroundStyle(p.text)
                        .lineLimit(1)
                    Text(subtitleText)
                        .font(.system(size: AppFont.caption))
                        .foregroundStyle(p.mute)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }

                Spacer(minLength: 8)

                HStack(spacing: 3) {
                    Text(hub.t("stageOpenAction"))
                        .font(.system(size: AppFont.caption, weight: .semibold))
                    Image(systemName: "chevron.right")
                        .font(.system(size: AppFont.caption - 1, weight: .semibold))
                }
                .foregroundStyle(p.text2)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .frame(maxWidth: 360, alignment: .leading)
            .background(inSidebar ? p.s1 : p.card, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
            .contentShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        }
        .buttonStyle(PressStyle())
    }
}
