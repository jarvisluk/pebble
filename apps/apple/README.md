# Bot Hub Apple 客户端（iOS / iPadOS / macOS）

原生 SwiftUI 客户端，同一个 Xcode 工程 `apps/apple/BotHub.xcodeproj` 同时提供 iOS / iPadOS（Target：`BotHub`）与 macOS 原生桌面端（Target：`PebbleMac`）：

- **iPhone / iPad（`BotHub`）**：支持多主机连接（HTTP / Tailcat / Tailscale）、扫码配对、Bot 列表与对话（SSE 实时流）、Bot 详情与记忆、定时任务、设置、设备与远程桌面接管；iPhone 为单栏导航，iPad 为侧栏 + 会话 + 详情 inspector 三栏。
- **macOS 原生端（`PebbleMac`）**：直接复用同一套 SwiftUI 界面与模型层，并通过内置 `bot-hub host-daemon` sidecar 子进程承接本机运行、远程连接（Tailcat / Tailscale / HTTP）、本机节点授权、容灾接管与增量回传、菜单栏常驻与远程桌面独立原生窗口（支持鼠标、滚轮与原生键盘输入透传）。
- 视觉规范沿用 `DESIGN.md`（macOS 桌面基准正文 `14px`，iOS/iPadOS 移动端 `16px`）。

## 构建

### macOS 原生应用（`Pebble.app`）

在仓库根目录一键构建原生 macOS App 并打包 `bot-hub`、`agent-desktop`、`agent-browser`：

```bash
make app
```

或仅在 Xcode 中选择 Scheme **PebbleMac**（Destination 选 **My Mac**）编译运行；如使用命令行单独构建：

```bash
cd apps/apple
xcodebuild -project BotHub.xcodeproj -scheme PebbleMac -destination 'platform=macOS' build
```

### iOS / iPadOS 客户端（`BotHub.app`）

先在仓库根目录构建 Tailcat 隧道 XCFramework（含 iOS 真机与模拟器切片）：

```bash
make ios-tunnel
```

产物在 `build/mobile/ios/BotHubTunnel.xcframework`，工程通过相对路径链接。然后用 Xcode 打开 `apps/apple/BotHub.xcodeproj`，Scheme 选 **BotHub**，目标选 iPhone / iPad 模拟器或真机，`⌘R` 运行。

命令行构建：

```bash
cd apps/apple
xcodebuild -project BotHub.xcodeproj -scheme BotHub -destination 'generic/platform=iOS Simulator' build
```

## 个人账号打包与推送配置

仓库内的 `Config/Signing.xcconfig` 仅包含开源占位签名（无个人 Team ID 与正式 Bundle ID）。

1. 复制 `apps/apple/Config/Signing.local.xcconfig.example` 为 `apps/apple/Config/Signing.local.xcconfig`（已在 `.gitignore` 中忽略）。
2. 在其中填入你自己的 `DEVELOPMENT_TEAM` 和正式 `APP_BUNDLE_ID`。
3. 真机或 App Store 打包时，在 Xcode Signing & Capabilities 中确保主 App 已开启 **Push Notifications** 与 **App Groups**（`group.$(APP_BUNDLE_ID)`），并在 `BotHub` 服务端 `.env` 配置自己的 `.p8` APNs 密钥或中继地址。

## 联调

按 `AGENTS.md` 在临时目录用独立配置与非默认端口启动服务（例如 `3801`），再在 App 里填 `http://127.0.0.1:3801` 与令牌，或在模拟器里打开：

```bash
xcrun simctl openurl booted "bothub://connect?link=http%3A%2F%2F127.0.0.1%3A3801%23token%3D<令牌>"
```
