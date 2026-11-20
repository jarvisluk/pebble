# bot-hub 工作区规则

用 Go 写的个人 Bot 编排底座，模型是 `1 Bot = 1 Workspace = 1 Session`。概览和 API 见 `README.md`。

## 结构

- `cmd/bot-hub`：CLI 与服务入口。`cmd/bot-hub-desktop`：桌面 App（Wails，cgo）。
- `internal/hub`：Bot 调度。`internal/harness`：opencode / claude / pi 等执行后端。
- `internal/channel`：HTTP、SSE、Telegram、Tailcat。`internal/mcp`：给 Bot 用的 MCP 工具。
- `internal/web/index.html`：整个 Web UI，单文件，由 `web.go` 通过 `//go:embed` 嵌入。
- `config/`、`state/`、`workspaces/`：运行时数据。除非任务需要，不要改。

## 命令

```bash
make build                 # 编译到 bin/bot-hub
make test                  # go test ./...
./bin/bot-hub serve        # http://127.0.0.1:3789
```

改完 Go 代码要跑 `make build && make test`。

## 规则

- **零第三方依赖**：Go 只用标准库（桌面 App 的 Wails 除外）。前端不引入框架、CDN、Web 字体或图标库。
- **最简单的方案**：不为假想的需求加抽象或配置。
- **Web UI 必须遵守 `DESIGN.md`**：颜色只用里面定义的 token，形状、动效、布局按规范来。要改视觉方向，先改 `DESIGN.md`，再改代码。
- **UI 保持单文件**：样式和脚本都写在 `index.html` 里。改动后要在浏览器里检查浅色和深色两种模式、桌面宽度（1440）和窄屏（≤820）。
- **先做样例再改**：用户要「样例 / 方案 / 看看效果」时，只在临时目录做独立 HTML，不动项目文件；用户确认后再改 `index.html`。
- **界面文案用简体中文**，简短直接，不加问候语和装饰性 emoji。
- **安全**：不要削弱 README「安全」一节的约束（Origin/Host 校验、apiToken、Telegram 白名单）；密钥不要写成明文。
- **并发编辑**：别人可能同时在改 `internal/harness` 等目录。只改任务相关的文件；遇到不是自己造成的编译错误，先报告，不要顺手修。
