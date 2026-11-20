# bot-hub (Stage 1)

基于 **Go** 编写的极简 Personal Bot 编排底座（零第三方依赖），严格落实 **`1 Bot = 1 Workspace = 1 Session`** 模型，直接复用本机 **OpenCode v2 + Magpie** 的 Agent Loop、模型路由、配额 Fallback、MCP 与 `billion-context` 能力。

## 核心能力

- **多 Bot 网页聊天界面**：`bot-hub serve` 启动后访问 `http://127.0.0.1:3789`，支持多 Bot 左侧切换、实时 SSE 推送、持久化聊天记录、工具调用动态提示、查看 `AGENTS.md`、一键新建 Bot 与重置 Session。
- **原生 Bot 管理与协作 MCP 工具**：每个 Bot 的 Workspace 自动生成 `opencode.json`，挂载本地 `bot-hub mcp`（stdio JSON-RPC 2.0），直接向 Bot 暴露 4 个工具：
  - `list_bots`：查看所有 Bot 及其状态
  - `get_bot`：查看指定 Bot 的配置与 `AGENTS.md`
  - `create_bot`：热创建并立即上线一个新 Bot（自动初始化独立 Workspace 与串行队列）
  - `message_bot`：向另一个 Bot 发消息并获取回复（带 `hop <= 4` 防死循环保护，双方聊天栏实时可见协作记录）
- **远程访问（Tailcat）**：内置 [tailcat](https://github.com/tailscale/tailcat)，经 DERP 中继打洞直连，两端都不需要 Tailscale 账号、登录或系统 VPN。Web UI 左上角「远程访问」显示链接与二维码，另一台机器运行 `bot-hub connect <链接>` 即可在本地打开。
- **多通道接入**：Web UI + 本地 HTTP/Webhook API + Telegram 长轮询 + 终端交互 CLI + 轻量定时唤醒器。

## 快速开始

### 1. 编译
```bash
make build
```

### 2. 启动网页界面与常驻服务
```bash
./bin/bot-hub serve
```
打开浏览器访问：`http://127.0.0.1:3789`

### 3. 终端直接试聊（可选）
```bash
./bin/bot-hub chat assistant
```

## HTTP / SSE API（默认仅监听 `127.0.0.1:3789`）

- `GET  /`：内嵌多 Bot 网页聊天界面
- `GET  /api/events`：SSE 实时事件流（`message` / `tool_use` / `bot_status` / `bot_created`）
- `GET  /api/bots`：查看所有 Bot 状态
- `POST /api/bots`：热创建新 Bot
- `GET  /api/bots/{id}`：查看 Bot 详情与 `AGENTS.md`
- `GET  /api/bots/{id}/messages`：获取持久化聊天记录
- `POST /api/bots/{id}/message`：发送消息（支持 `fromBotId` / `hop` 跨 Bot 调用）
- `POST /api/bots/{id}/reset`：重置 Session（保留 Workspace 和 `memory/`）
- `POST /hooks/{id}`：外部 Webhook 唤醒（可选 `X-Webhook-Secret` 校验）
- `POST /api/login`：用 `http.apiToken` 换取 HttpOnly Cookie（Web UI 使用）
- `GET  /api/tailcat`、`GET /api/tailcat/qr.png`：Tailcat 状态、链接与二维码

## 安全

- 所有请求校验 `Origin`，拒绝跨站请求（防 CSRF）；仅监听回环地址时还校验 `Host`（防 DNS rebinding）。
- `http.apiToken`（如 `${BOT_HUB_API_TOKEN}`）启用后，`/api/*` 需要 `Authorization: Bearer <token>` 或登录 Cookie；`host` 不是回环地址时必须设置，否则拒绝启动。Bot 内的 `bot-hub mcp` 会自动通过环境变量带上令牌。
- Telegram 必须配置 `allowedUserIds` 或 `allowedChatIds`，为空时不启动该 Bot 的轮询。
- `http.tailcat.enabled` 必须配合 `http.apiToken`；经隧道进来的请求一律校验令牌。链接含服务端密钥，只发给自己的设备；删除 `state/tailcat-identity.json` 并重启即可作废旧链接。默认使用 Tailscale 公共 DERP，可用 `derpMapURL` 指向自建 DERP。
- 新建 Bot 写回 `config/bots.json` 时保留 `${VAR}` 引用，密钥不会以明文落盘。
