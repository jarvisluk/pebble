# 工作区规则

项目架构与接口说明见 `README.md`，视觉与交互规范见 `DESIGN.md`，专项细则见 `docs/rules/`。**仅在与当前改动面匹配时才读取并执行对应的 reference 文档，禁止对纯后端、性能优化等无关任务套用额外门禁。**

## 行事规则

- **最简实现**：只解决当前明确提出的问题，不为假想需求增加抽象、配置或额外依赖；外部工具已具备的通用能力直接复用其原生 CLI，不在项目内重复自建。
- **规范与样例先行**：调整视觉方向前先更新 `DESIGN.md` 再改代码；当需求是「样例 / 方案 / 看看效果」时，仅在临时目录制作独立预览，确认后再修改仓库文件。
- **按改动面多端一致**：仅当改动影响对外能力、接口协议或客户端 UI/布局/视觉结构时，同步检查并更新仓库内受影响的活跃客户端（`web/`、`apps/apple`、`apps/android`），避免单端遗漏；`apps/mobile` 为历史归档目录，不再纳入同步。纯后端、构建脚本或零布局/样式变动的单端底层改动（网络、隧道、存储、性能或编译优化等）不触发跨端同步。
- **文案克制与双语同步**：界面与提示文案简短直接，不加问候语和装饰性 emoji；保持简体中文与英文双语一致，新增或修改用户可见文案时同步维护双语资源。Go 后端（`cmd/`、`internal/`）禁止硬编码面向用户的中文文案，统一使用英文规范文案并通过结构化错误 `code`（如 `screen_recording_denied`）交给各端映射本地化文案；客户端（`web/`、`apps/apple/`、`apps/android/`）所有中文文案必须集中维护在各自双语词典中，业务代码与英文资源文件禁止硬编码中文（极少数语言自称如 `"简体中文"` 或历史迁移解析器需显式标注 `i18n:allow-cjk`）。`make test` 内置 `internal/i18ncheck` 门禁（可单独运行 `make check-i18n`），自动拦截后端/客户端代码中文泄露、英文资源混入中文以及三端双语 key 漂移。
- **安全与数据边界**：不削弱 `README.md`「安全」章节声明的鉴权、来源校验与白名单约束；严禁在代码、日志或接口返回中暴露明文密钥；除非任务明确要求，不读写或改动 `config/`、`state/`、`workspaces/` 下的真实运行数据。
- **并发协作纪律**：可能存在多会话并行开发。只修改与当前任务直接相关的文件；遇到非本任务引入的构建或测试失败先报告，不顺手修改；重启本项目的常驻服务或应用包时只操作本项目自身实例，不误伤宿主机其他进程。

## 改动范围与验证矩阵

| 改动范围 | 必做验证 | 明确免除项 | 需阅读的 Reference |
|---|---|---|---|
| **纯文档 / 规则**（`*.md`） | 核对引用路径、链接与命令准确性 | 不编包、不跑单测、不做视觉回归 | — |
| **Go 后端 / 协议 / 性能优化 / 构建脚本**（`cmd/`、`internal/`、`Makefile`、`scripts/`） | 受影响模块的 `make build && make test`、benchmark 或隔离性能对比（避开 `3789`） | **不做视觉回归、不截屏、不启模拟器** | `docs/rules/local-dev-and-release.md` |
| **Web 前端 UI / 交互**（`web/`） | `make web && make build`；仅对改动涉及的页面/状态做浅/深色 + 宽/窄屏截图并输出画廊报告（仅改 legacy Wails 壳层时加测 Wails 窗口） | 未改动页面不截屏；不强制启动 Android / iOS 模拟器 | `docs/rules/visual-regression.md`、`docs/rules/local-dev-and-release.md` |
| **原生客户端非 UI 底层改动**（`apps/apple`、`apps/android` 纯网络/隧道/存储/性能/编译优化，零布局与样式变动） | 仅跑受影响端的编译、单测与对应能力验证 | **不做视觉回归、不截屏、不强绑另一端同步** | `docs/rules/local-dev-and-release.md` |
| **原生客户端 UI / 布局 / 视觉 / 导航**（`apps/apple`、`apps/android`） | Apple & Android 双端同步更新 + 提交前静态门禁 + 改动部分实机截图自审并输出画廊报告 | 未改动页面不截屏 | `docs/rules/native-clients.md`、`docs/rules/visual-regression.md`、`docs/rules/local-dev-and-release.md` |
| **合并入 `main` 分支**（交付阶段） | 在主仓库 `main` 分支按**本次实际受影响的端**重编安装包并覆盖安装/平滑重启 `Pebble.app`（`open -g`） | 未受影响端不重编；非 `main` 合并提交不重装宿主机应用 | `docs/rules/local-dev-and-release.md` |

## 按需参考文档（Reference）

- [原生客户端开发规范与静态门禁](docs/rules/native-clients.md)：**仅修改 `apps/apple` 或 `apps/android` 时阅读**（双端同步范围、6 档字号、Apple 三端单源与 `Theme.swift` 原语白名单、提交前 grep 门禁）。
- [UI 改动截图报告与多端视觉回归手册](docs/rules/visual-regression.md)：**仅涉及 `web/` 或原生客户端 UI/布局/视觉/交互改动时阅读**（按改动范围截屏原则、常见页面核对参考、macOS/iOS/iPadOS/Android/Web 驱动命令、Artifact 画廊报告格式与先逐张 `read_file` 自审要求）。
- [构建入口、隔离联调与合入 main 换包规范](docs/rules/local-dev-and-release.md)：**构建特定端、启动隔离服务或合并 `main` 换包重装时阅读**。
- [版本号管理与发版流程](docs/rules/release-and-versioning.md)：**调整版本号、校验多端版本一致性或执行 Release 打包发布时阅读**。
