# BotHubApp (macOS + iOS 共用 Swift 壳)

多平台 SwiftUI + `WKWebView` 壳，直接打开本地或远端的 Bot Hub 页面：
- macOS：隐藏系统标题栏并保留红绿灯，注入 `BotHubMac` UA 触发 `internal/web/index.html` 的顶部避让。
- iOS：全屏铺满并通过 `viewport-fit=cover` + `env(safe-area-inset-*)` 避让刘海/灵动岛与底部指示条。
- 支持 Safari Web Inspector（Debug 模式已开启 `isInspectable = true`）。

## 快速调试（和 Wails 同时开）

1. 先用 Wails（或 `make serve`）启动本机服务，监听 `http://127.0.0.1:3789`。
2. 直接用 Xcode 打开本目录：
   ```bash
   open apps/apple/Package.swift
   ```
3. 在 Xcode 顶部 Scheme 选择 **BotHubApp**，目标设备选 **My Mac** 或 **iPhone Simulator**，按 `⌘R` 运行即可与 Wails 同屏调试。
4. 按 `⌘,` 可切换服务端地址，按 `⌘R`（应用内）刷新页面。
