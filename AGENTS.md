# 工作区规则

项目架构与接口说明见 `README.md`，视觉与交互规范见 `DESIGN.md`。本文件只声明协作行事准则与测试、运行基建约定。

## 行事规则

- **最简实现**：只解决当前明确提出的问题，不为假想需求增加抽象、配置或额外依赖；外部工具已具备的通用能力直接复用其原生 CLI，不在项目内重复自建。
- **规范与样例先行**：调整视觉方向前先更新 `DESIGN.md` 再改代码；当需求是「样例 / 方案 / 看看效果」时，仅在临时目录制作独立预览，确认后再修改仓库文件。
- **多端一致**：涉及能力、接口或视觉变更时，同步检查并更新仓库内所有受影响的客户端入口，避免单端遗漏。任何对 Apple 原生客户端（`apps/apple`）或 Android 原生客户端（`apps/android`，以及历史 `apps/mobile`）的页面结构、顶栏/按钮样式、导航入口、设置页或字号/间距变更，**严禁单端提交**；必须在同一次任务中对照另一端同步更新，并严格遵循 `DESIGN.md` 第 3 节与第 10 节的 6 档字号常量（桌面 `12/13/14/15/17/20`，移动 `12/13/15/16/17/20`），禁止在视图中硬编码散落字号；原生端同时覆盖 iPhone 窄屏与 iPad 宽屏两种布局，改动后两种都要检查。
  - **Apple 三端单源与原语白名单**：`apps/apple/BotHub/` 业务视图（`BotListView`、`BotDetailView`、`CreateBotView`、`DevicesView`、`SettingsView`、`ConnectView` 等）禁止出现 UI 级 `#if os(macOS)` / `#if os(iOS)`，只允许按 `hubCompact` 分支；操作系统能力差异收口到 `Platform.swift` 与专用能力文件。禁止在业务视图中裸用跨端渲染分裂的系统容器（系统默认 `Form`、宽屏 `!compact` 侧栏 `List(.sidebar)`、承载 `.sheet`/`.inspector` 外壳的 `NavigationStack` + `.toolbar`）；必须统一复用 `Theme.swift` 原语（`HubFormStyle`、`HubSheetScaffold`、`HubSegmentedPicker`、`HubMenuPicker`、`HubFormField`、`HubSearchBar` 与基于 `ScrollView` + `LazyVStack` 的 `12pt` 统一宽屏侧栏）。
  - **提交前静态门禁**：涉及客户端视图改动提交前必须执行静态 grep，确认业务视图 UI 级 `#if os(...)` = 0、`!compact` 侧栏无 `.listStyle(.sidebar)`、`.sheet`/`.inspector` 无 `NavigationStack`/`SheetToolbar` 残留、无视图内散落硬编码字号（统一走 `AppFont` / `PebbleFont`）。
- **文案克制**：界面与提示文案统一使用简体中文，简短直接，不加问候语和装饰性 emoji。
- **安全与数据边界**：不削弱 `README.md`「安全」章节声明的鉴权、来源校验与白名单约束；严禁在代码、日志或接口返回中暴露明文密钥；除非任务明确要求，不读写或改动 `config/`、`state/`、`workspaces/` 下的真实运行数据。
- **并发协作纪律**：可能存在多会话并行开发。只修改与当前任务直接相关的文件；遇到非本任务引入的构建或测试失败先报告，不顺手修改；重启本项目的常驻服务或应用包时只操作本项目自身实例，不误伤宿主机其他进程。

## 测试与运行基建

- **构建与单测入口**：代码改动完成后必须执行 `make build && make test` 验证通过；涉及 `web/` 前端改动时先执行 `make web`（产出 `internal/web/dist`）再执行 `make build` / `make app`；涉及桌面包体时执行 `make app`，涉及移动端时同步通过移动端静态检查与构建校验。
- **隔离运行实例**：本地拉起服务做联调或回归时，必须在临时目录使用独立配置与非默认端口（避开默认常驻端口 `3789`），验证完毕后主动终止后台测试进程并确认端口已释放。
- **浏览器与原生壳层双重回归**：涉及桌面端或 `web/` 界面改动时，需覆盖浅色与深色模式、桌面宽屏（`1440`）与窄屏（`≤820`）；除通过无头浏览器工具（`agent-browser`）配合独立 `AGENT_BROWSER_SESSION` 检查页面外，还必须启动带原生壳层的实际桌面应用包，以后台不抢焦点、不抢鼠标的方式（如 `open -g` 配合 `agent-desktop` 窗口截图与无头语义操作）目视检查窗口标题栏/红绿灯内嵌区等真实壳层表现。
- **三端视觉回归（Android 蓝叠 + iPhone + iPad）**：所有任务完成后，必须在 **本机蓝叠 Android 模拟器**、**iPhone 模拟器** 与 **iPad 模拟器**（均已常驻开启）三端完成安装、拉起与截图目视回归（覆盖浅色与深色模式）；同时检查系统状态栏、导航栏与安全区表现。涉及侧栏、表单、弹窗或顶栏改动时，必须做 **macOS vs iPadOS 同屏并排截图核对**（核对选中气泡圆角、左右边距对齐、顶栏/弹窗壳层一致），并覆盖 **最窄容器边界检查**（`320pt` Inspector 展开 `Appearance`、`Technical Details` 等全部 `DisclosureGroup`、`New Bot` 等表单 Sheet 与 `375pt` 手机窄屏，确认零文字截断、零控件水平溢出）。Android 蓝叠回归前**必须基于当前最新代码重新构建 Release APK**（`./gradlew assembleRelease`，严禁安装无 JS bundle 的 Debug APK，严禁复用过期时间戳的旧 APK），且必须同时覆盖**蓝叠手机竖屏浮窗（`364×640 dp`）**与**蓝叠平板宽屏（`1280×676 dp`）**及浅/深色模式。本机 Android SDK 路径为 `/opt/homebrew/share/android-commandlinetools`（`ANDROID_HOME`），通过 `adb devices` / `adb install -r` / `adb exec-out screencap -p` 驱动蓝叠与调试真机；通过 `xcrun simctl list devices booted` / `xcrun simctl install` / `xcrun simctl launch` / `xcrun simctl io <udid> screenshot` 驱动 iPhone 与 iPad 模拟器（若连有 `xcrun devicectl list devices` 可见的 iOS 真机，同步通过 `xcrun devicectl device install app` 回归）。遇到系统锁屏或授权门禁时停下交由人工处理，测试期间临时调整的设备常亮或端口转发在结束时必须恢复原状。
- **提交、合并 main 与换包重装**：每次代码或界面变动验证通过并提交 `git commit` 后，合并入 `main` 分支时必须在主仓库（`main` 分支）重新构建受影响端的最新安装包（桌面端 `make build && make app`，前端变动先 `make web`，Android 端变动同步打包并通过 ADB 覆盖安装到已连真机，Apple/iOS 端变动同步编译真机包并通过 `xcrun devicectl device install app` 覆盖安装到已配对/连接的 iOS 真机），并完成本机 `Pebble.app` 的覆盖安装与平滑重启（使用 `open -g` 不抢焦点），不能只在临时 worktree 内构建或忘记重启主仓库应用，确保实际运行的始终是最新包。
