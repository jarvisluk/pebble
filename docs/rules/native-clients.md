# 原生客户端开发规范与静态门禁

> **触发条件**：**仅在修改 `apps/apple` 或 `apps/android` 时阅读与生效。**
>
> **范围区分**：若改动仅涉及单端非 UI 底层能力（网络、隧道、Keystore/存储、状态管理、日志、性能或编译优化），且**零布局、零样式、零页面结构与交互变动**，只需完成该端编译、单测及对应能力验证，**不触发**双端同步与视觉回归。

## 1. 双端一致与布局覆盖

- 对 Apple 原生客户端（`apps/apple`）或 Android 原生客户端（`apps/android`）的页面结构、顶栏/按钮样式、导航入口、设置页或字号/间距变更，**严禁单端提交**，必须在同一次任务中对照另一端同步更新。
- 原生端必须同时覆盖**手机窄屏**（iPhone / Android 手机竖屏）与**平板宽屏**（iPad / Android 平板横屏）两种布局，只按视口尺寸 / `horizontalSizeClass` 分支，不按设备类型硬编码分支。
- `apps/mobile` 为历史 Expo/React Native 归档目录，**不纳入**多端同步范围。

## 2. 6 档字号规范

严格遵循 `DESIGN.md` 第 3 节与第 10 节的 6 档整数常量，禁止在业务视图中散落硬编码字号：

- **桌面端（macOS 原生 `PebbleMac`）**：`12 / 13 / 14 / 15 / 17 / 20`
- **移动端（iOS / iPadOS / Android 原生）**：`12 / 13 / 15 / 16 / 17 / 20`（正文 `15`、顶栏/卡片标题 `16`，其余档位与桌面一致）
- **统一入口**：
  - Apple 端统一使用 `AppFont`（定义于 `apps/apple/Pebble/Platform.swift`）
  - Android 端统一使用 `PebbleFont`（定义于 `apps/android/app/src/main/java/ai/pebble/android/ui/theme/Theme.kt`）

## 3. Apple 三端单源与原语白名单

- `apps/apple/Pebble/` 业务视图（`BotListView`、`BotDetailView`、`CreateBotView`、`DevicesView`、`SettingsView`、`ConnectView` 等）**禁止出现 UI 级 `#if os(macOS)` / `#if os(iOS)`**，只允许按 `hubCompact` 分支；操作系统能力差异收口到 `Platform.swift` 与专用能力文件。
- 禁止在业务视图中裸用跨端渲染分裂的系统容器：
  - 系统默认 `Form`
  - 宽屏 `!compact` 侧栏 `List(.sidebar)`
  - 承载 `.sheet` / `.inspector` 外壳的 `NavigationStack` + `.toolbar`
- 必须统一复用 `apps/apple/Pebble/Theme.swift`、`HubPrimitives.swift`、`HubForm.swift` 原语：
  - `HubFormStyle`
  - `HubSheetScaffold`
  - `HubSegmentedPicker`
  - `HubMenuPicker`
  - `HubFormField`
  - `HubSearchBar`
  - 基于 `ScrollView` + `LazyVStack` 的 `12pt` 统一宽屏侧栏

## 4. 提交前静态门禁

涉及原生客户端视图改动时，提交前必须执行静态 grep 核查，确认以下各项全部通过：

1. Apple 业务视图（`BotListView.swift`、`BotDetailView.swift`、`CreateBotView.swift`、`DevicesView.swift`、`SettingsView.swift`、`ConnectView.swift` 等）中 UI 级 `#if os(...)` = `0`；
2. `!compact` 宽屏侧栏无 `.listStyle(.sidebar)`；
3. `.sheet` / `.inspector` 外壳无 `NavigationStack` / `SheetToolbar` 残留；
4. 业务视图内无散落硬编码字号（Apple 统一走 `AppFont`，Android 统一走 `PebbleFont`）。
