# Bot Hub Apple 客户端（iOS / iPadOS）

原生 SwiftUI 客户端，一个 Xcode 工程同时出 iPhone 与 iPad 两端（macOS 使用桌面端 `Bot Hub.app`），功能与 `apps/mobile` 对齐：多主机连接（HTTP / Tailcat 隧道）、扫码配对、Bot 列表与对话（SSE 实时流）、Bot 详情与记忆、定时任务、设置（Magpie 接管、模型、USER.md、全局记忆）、设备与远程桌面接管。

- 结构与控件用系统原生组件（`NavigationSplitView` / `NavigationStack`、`Form`、`List`、`Picker`、`Toggle`、系统工具栏与 sheet），视觉沿用 `DESIGN.md` Token，详见 `DESIGN.md` 第 9 节。
- iPhone 为单栏导航；iPad 为侧栏 + 会话 + 详情 inspector 三栏。
- 连接凭据存 Keychain。
- 支持 `bothub://connect?link=<地址>` 链接一键连接，地址可带 `#token=...`。

## 构建

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

## 联调

按 `AGENTS.md` 在临时目录用独立配置与非默认端口启动服务（例如 `3801`），再在 App 里填 `http://127.0.0.1:3801` 与令牌，或在模拟器里打开：

```bash
xcrun simctl openurl booted "bothub://connect?link=http%3A%2F%2F127.0.0.1%3A3801%23token%3D<令牌>"
```
