# UI 改动截图报告与多端视觉回归手册

> **触发条件**：**仅当改动涉及 `web/` 或原生客户端（`apps/apple` / `apps/android`）的 UI、布局、样式、主题、导航、弹窗或可见交互时，按受影响端与改动范围执行。**
>
> **免除范围**：纯后端、协议、构建脚本、性能优化、纯文档改动，以及零布局/样式/交互变动的单端底层改动，**一律不做视觉回归，也不生成截图报告**。

## 1. 总原则：只截改动部分，不做无关页面的全量截图

发生 UI 改动时，在**隔离测试环境**下基于当前最新构建产物，**仅对本次改动直接涉及的页面、组件状态及受影响的主题/视口做实机截图**，逐张自审修复缺陷后，向用户交付一份图文画廊报告 Artifact（参考 `macos_swift_client_planning` 分支的画廊报告做法）。

- **按改动范围裁剪**：单页、单组件或局部交互改动，只需截取该页面及对应的关键状态（如展开/收起、弹窗、浅/深色或受影响的宽/窄屏边界），**不要顺手截取未改动页面**。
- **何时扩大范围**：仅当改动影响全局主题 Token、通用字号体系、顶层导航壳层或公共表单/列表原语，或用户明确要求全量回归时，才按受影响面扩展到代表性页面或完整清单。
- **隔离运行前提**：在临时目录使用独立配置与非默认端口拉起测试服务（避开默认常驻端口 `3789`），按本次截屏页面所需准备代表性测试数据（如 Bot、对话消息、Markdown/代码块或工作区文件），确保被核对区域有真实内容。

## 2. 按改动匹配的页面核对参考

无需每次全覆盖；按本次改动命中的模块，从下方挑选对应项截屏与核对（浅/深色、宽/窄屏均按改动是否涉及来覆盖）：

1. **引导 / 连接页**：启动门禁、连接配置与配对入口。
2. **主列表与主对话页**：宽屏多栏或手机窄屏列表/对话页，核对气泡、Markdown、选中胶囊与底栏输入框。
3. **Bot 详情 / Inspector 面板**：展开 `Appearance`、`Technical Details` 等相关 `DisclosureGroup`，核对 `320pt` 最窄 Inspector 容器边界。
4. **新建 Bot 表单 Sheet 与主要弹窗**：核对表单原语、选择器与按钮布局，以及 `375pt` 手机窄屏边界。
5. **工作台 Stage（文件浏览器 + 文件预览 + 远程桌面）**：按改动覆盖对应子视图（文件列表、Markdown/代码预览或远程桌面），含展开/收起左侧聊天栏与左上角返回对话态。
6. **设置页各主要分页与退出恢复态**：覆盖改动涉及的设置分页；若改动涉及设置导航、顶栏或侧栏，再补截**退出设置页后的主界面**，核对侧栏宽度保持、工具栏无异常折叠或多余标题残留。
7. **设备管理 / 配对页**：改动涉及设备管理或扫码/手动配对时覆盖。

## 3. 各端驱动与截屏方法

### Web 端（仅 `web/` UI 改动）

- 先执行 `make web && make build`（产出最新 `internal/web/dist` 并编入二进制）。
- 使用无头浏览器 `agent-browser` 配合独立 `AGENT_BROWSER_SESSION` 覆盖改动涉及页面的浅色/深色模式，以及受影响的桌面宽屏（`1440`）与窄屏（`≤820`）。
- 默认 `make app` 构建的是原生 SwiftUI `PebbleMac`，不加载 `web/`；**仅当改动涉及 legacy Wails 桌面壳（`make app-wails`）时**，才需额外启动 Wails 桌面应用包检查窗口标题栏与红绿灯内嵌区。

### macOS 原生端（`apps/apple` UI 改动）

- 以后台不抢焦点、不抢鼠标的方式启动测试包（如 `open -g`），使用无头语义与窗口截图工具驱动：
  - `agent-desktop list-windows` 定位目标窗口 ID；
  - `agent-desktop snapshot --window-id <id> -i --compact` + `agent-desktop click @ref` 语义切换页面与主题；
  - `agent-desktop screenshot --window-id <id> <path>` 保存窗口截图；
  - 改动涉及顶栏或窗口壳层时，目视核对标题栏与红绿灯内嵌区表现。

### iPhone & iPad 模拟器（`apps/apple` UI 改动）

- 使用 `xcrun simctl list devices booted`、`xcrun simctl install`、`xcrun simctl launch` 安装并启动最新编译的 `Pebble.app`。
- 可配合 `xcrun simctl openurl <udid> "pebble://..."` 直达目标改动页面，或通过 `agent-desktop` 语义操作 Simulator 窗口切换状态，使用 `xcrun simctl io <udid> screenshot <path>` 截屏。
- 若连有 `xcrun devicectl list devices` 可见的 iOS 真机，同步通过 `xcrun devicectl device install app` 覆盖验证。
- 涉及侧栏、表单、弹窗或顶栏改动时，执行 **macOS vs iPadOS 改动页同主题并排核对**（核对选中气泡圆角、左右边距对齐、顶栏/弹窗壳层一致）。

### Android 蓝叠与真机（`apps/android` UI 改动）

- 回归前**必须基于当前最新代码重新构建原生 Release APK**（`make android-native` 或 `cd apps/android && ./gradlew :app:assembleRelease`），严禁安装 `.debug` 变体或复用过期时间戳的旧 APK。
- 本机 Android SDK 路径为 `/opt/homebrew/share/android-commandlinetools`（`ANDROID_HOME`），通过 `adb devices`、`adb install -r`、`adb exec-out screencap -p` 驱动蓝叠与已连调试真机。
- 按改动影响范围覆盖**蓝叠手机竖屏浮窗（`364×640 dp`）**与/或**蓝叠平板宽屏（`1280×676 dp`）**的对应浅色/深色模式，并检查系统状态栏、导航栏与安全区表现。

## 4. 截图画廊报告格式与强制自审流程

### 产物存放与结构

1. 所有截图文件统一保存到当前会话的 Artifact 目录（如 `<Artifact Directory Path>/scratch/`），**严禁写入仓库目录**；并在 Artifact 目录创建面向用户的画廊报告（如 `ui_regression_gallery.md`）。
2. 报告顶部写明：隔离测试服务端口、对应端字号档位、本次改动覆盖的页面/状态范围及清理状态。
3. 按端分节（macOS / iPadOS / iOS / Android / Web），每组改动页面使用四反引号 `carousel` 语法嵌入本地绝对路径截图：
   `````markdown
   ````carousel
   ![页面说明（深色）](/abs/path/to/dark.png)
   <!-- slide -->
   ![页面说明（浅色）](/abs/path/to/light.png)
   ````
   `````
4. 跨端对比页面（如 macOS vs iPadOS 同页同主题）在同一个 `carousel` 中紧邻排布，方便直观对比。
5. 若本次改动范围内的某项因客观环境限制未能截取，**必须在报告中显式列出未覆盖项与原因**，不得静默省略。

### 先逐张自审、再交付（强制）

生成画廊报告并交给用户前，Agent 必须先用 `read_file` **逐张打开并审阅本次全部截图**，对照 `DESIGN.md` 检查与本次改动相关的条目，发现问题立即修复并重截，绝不带病交付：

- 浅/深色背景与卡片层次、`--page` / `--card` / 中性灰边界是否正确，无全局误染彩色；
- 6 档字号是否与端别一致，无异常过大/过小或混用；
- 窄屏或窄容器（如 `320pt` Inspector、`375pt` / `364×640 dp` 手机窄屏）下**零文字截断、零控件水平溢出**；
- 系统状态栏、导航栏、底部手势条与 macOS 红绿灯安全区无遮挡或重叠；
- 选中胶囊圆角、左右边距、退出设置页后的工具栏与侧栏宽度无残留异常。

### 收尾与人工门禁

- 遇到系统锁屏、系统隐私授权弹窗等门禁时停下交由人工处理。
- 回归完成后，必须终止隔离测试服务、清理临时数据、确认测试端口已释放，并将测试期间临时调整的设备常亮或端口转发恢复原状。
