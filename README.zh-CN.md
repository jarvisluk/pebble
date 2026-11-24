<h1 align="center">Pebble</h1>

<p align="center"><b>运行在你自己电脑上的极简个人 Bot 编排底座。</b></p>

<p align="center">
  <b>1 Bot = 1 Workspace = 1 Session</b>&nbsp;&nbsp;·&nbsp;&nbsp;<b>自带运行时</b>&nbsp;&nbsp;·&nbsp;&nbsp;<b>本机优先</b>
</p>

<p align="center">
  <img height="26" alt="Go Standard Library" src="https://img.shields.io/badge/Go-Stdlib%20First-1d1b19?logo=go&logoColor=white&style=for-the-badge">
  <a href="https://usemagpie.ai"><img height="26" alt="Powered by Magpie" src="https://img.shields.io/badge/Gateway-Magpie-d9774b?style=for-the-badge"></a>
  <a href="https://github.com/tailscale/tailcat"><img height="26" alt="Remote via Tailcat" src="https://img.shields.io/badge/Tunnel-Tailcat-1d1b19?style=for-the-badge"></a>
  <a href="https://wails.io"><img height="26" alt="Desktop via Wails" src="https://img.shields.io/badge/Desktop-Wails%20v3-1d1b19?style=for-the-badge"></a>
</p>

<p align="center">
  <a href="README.md">English</a>&nbsp;&nbsp;·&nbsp;&nbsp;<b>简体中文</b>
</p>

- **一支常驻本机的个人 Bot 团队。** 严格落实 `1 Bot = 1 Workspace = 1 Session`：每个 Bot 拥有独立目录、`AGENTS.md` 职责规范、串行执行队列与持久会话，互不串味。
- **直接复用已有的 Agent 运行时与订阅。** 原生驱动本机 **OpenCode**、**Claude Code**、**Pi** 与 **DeepSeek Harness (`dsh`)**；既可通过 **Magpie** 统一模型路由与配额回退，也可保留各 CLI 自有配置。
- **数据留在自己机器上，随处安全可达。** 单个 Go 二进制内嵌 Web UI、定时唤醒与 **Tailcat** 点对点加密隧道——无需公网 IP、无需 Tailscale 账号即可跨设备连接。

## 核心特性

### 隔离工作区与共享记忆：`1 Bot = 1 Workspace = 1 Session`

把日常杂事、深度调研、运维巡检塞进同一个聊天窗口，上下文很快就会腐化。`pebble` 为每个 Bot 分配独立的磁盘工作区、唯一的长期会话与专属串行队列（`botLane`）：同一时刻来自网页、定时任务或其他 Bot 的请求按序执行，支持随时中断当前轮次。

与此同时，每个工作区自动软链 `_shared/` 公共目录并维护各自的 `memory/`：在 `_shared/USER.md` 写一次全局偏好，所有 Bot 都能读到；把关键结论沉淀进 `memory/`，即使重置会话（`/reset`）也不会丢失长期记忆。

<details>
<summary><b>工作区布局：</b>每个 Bot 在磁盘上的目录结构与生命周期</summary>

```text
pebble/
├── config/bots.json              # Bot 列表、模型接入与定时规则（保留 ${VAR} 引用）
├── state/
│   ├── hub.db                    # 单文件 SQLite 状态库（会话归档与 FTS5 检索）
│   ├── sessions.json             # 历史兼容会话快照（首次启动自动导入 hub.db）
│   └── messages/                 # 历史兼容 JSONL 记录（导入后保持只读）
└── workspaces/
    ├── _shared/                  # 共享全局上下文（USER.md 与托管版 PEBBLE.md）
    └── assistant/                # 单个 Bot 的专属工作区
        ├── AGENTS.md             # 该 Bot 的角色定义与行为边界
        ├── memory/               # 本地笔记（配合 Magpie Memory 使用）
        ├── _shared -> ../_shared # 自动创建的共享目录软链
        ├── .agents/skills/pebble-setup/SKILL.md # 自动下发的 Pebble 接口与配置 Skill
        └── opencode.json         # 自动注入的 pebble MCP 与 PEBBLE.md 指令
```

调用 `POST /api/bots/{id}/reset` 或发送 `/reset` 时，会将当前会话归档进 `state/hub.db` 并开启干净的新会话；工作区文件、`AGENTS.md`、`memory/` 与历史会话切片召回均保留。

</details>

### 多 Bot 自治与协作：一句话拉起新成员，跨 Bot 委派任务

每个 Bot 启动时都会自动挂载内置的 `pebble mcp`（stdio JSON-RPC 2.0）工具集。当一项任务超出单个 Bot 的职责范围时，它可以查看现有团队成员、热创建一个具备专属指令与模型的新 Bot，或直接向另一个 Bot 发消息请求协助。

跨 Bot 调用自带 `hop <= 4` 防死循环保护，且双方的聊天时间线都会实时展示协作记录；你也可以在对话框里直接用 `/ask <bot> <消息>` 点名协作。

<details>
<summary><b>MCP 核心工具与全量 CLI：</b>运行时 MCP 原语 + 全量 <code>pebble</code> CLI</summary>

<table>
  <tr>
    <th align="left" width="160">MCP 工具</th>
    <th align="left" width="540">用途</th>
  </tr>
  <tr>
    <td><code>message_bot</code></td>
    <td>向目标 Bot 发送任务并同步获取回复，双方聊天记录同步留痕（最大跳数 <code>hop &lt;= 4</code>）。</td>
  </tr>
  <tr>
    <td><code>recall_session</code></td>
    <td>按切片和单条长度保护读取历史会话（支持 <code>previous</code>、分页与单条全文提取）。</td>
  </tr>
  <tr>
    <td><code>desktop_task</code></td>
    <td>将多步桌面 GUI 或可见浏览器任务委派给运行在可见桌面的隔离 computer-use worker。</td>
  </tr>
  <tr>
    <td><code>request_takeover</code></td>
    <td>遇到扫码登录、2FA、验证码或支付确认时暂停自动化，通知用户到远程桌面页面处理并恢复。</td>
  </tr>
  <tr>
    <td><code>node_exec</code></td>
    <td>在远端节点执行 Shell 命令（包括调用远端 <code>agent-desktop</code> / <code>agent-browser</code>）。</td>
  </tr>
  <tr>
    <td><code>node_file</code></td>
    <td>在远端节点读取、写入或列举文件。</td>
  </tr>
</table>

<p><b>全量 <code>pebble</code> CLI</b>（自动从环境变量或工作区 MCP 配置识别当前 Bot 上下文，输出 JSON）：<code>pebble bot list|get|create|update|delete|message|send</code>、<code>pebble settings get|models|set</code>、<code>pebble session list|recall|rotate</code>、<code>pebble routine list|upsert|delete|run</code>、<code>pebble node list|exec|file</code>、<code>pebble desktop targets|task|takeover|setup|power</code>。</p>

<p>桌面与网页自动化直接复用 <code>agent-desktop</code> 与 <code>agent-browser</code> CLI（远端设备经 <code>node_exec</code> 调用）：浏览器优先用 <code>agent-browser --auto-connect --pin-tab</code> 接管正在运行的 Chromium 标签页，连不上时按需回退到无头模式或 <code>--headed --profile ~/.pebble/browsers/&lt;bot-id&gt;</code>。</p>

</details>

### 自带运行时（BYO Harness）与模型网关

`pebble` 不重复造 Agent Loop，而是直接编排你机器上最成熟的 Agent CLI。同一个 Hub 内的不同 Bot 可以混用不同运行时，并支持在 Web UI 设置面板中切换两种模型接入模式（`modelAccess`）：

- **Magpie 网关模式（`magpie`，默认）**：复用本机 [Magpie](https://usemagpie.ai) 的模型目录、路由策略与配额 Fallback，支持在界面内一键将各 Harness 绑定到网关。
- **自有配置模式（`self`）**：不改动任何全局配置，直接沿用各 CLI 自身的订阅账号或 API Key。

<details>
<summary><b>运行时一览：</b>内置支持的执行后端与通信机制</summary>

<table>
  <tr>
    <th align="left" width="120">Harness</th>
    <th align="left" width="180">运行时</th>
    <th align="left" width="400">长连接与会话机制</th>
  </tr>
  <tr>
    <td><code>opencode</code></td>
    <td>OpenCode（默认）</td>
    <td>托管单个常驻 <code>opencode serve</code>（或连接外部实例），通过 HTTP + 共享 SSE 事件流驱动多会话，工作区自动写入 <code>opencode.json</code>。</td>
  </tr>
  <tr>
    <td><code>claude</code></td>
    <td>Claude Code</td>
    <td>每个 Bot 维持常驻 <code>claude -p --input-format stream-json</code> 进程，自动桥接 <code>CLAUDE.md</code> → <code>@AGENTS.md</code> 并按进程注入 MCP。</td>
  </tr>
  <tr>
    <td><code>pi</code></td>
    <td>Pi</td>
    <td>每个 Bot 维持常驻 <code>pi --mode rpc</code> 进程，由 Hub 托管 Session ID，进程重启后无缝续接原会话。</td>
  </tr>
  <tr>
    <td><code>dsh</code></td>
    <td>DeepSeek Harness</td>
    <td>复用单个常驻 <code>dsh --profile acp</code> 进程（Agent Client Protocol over stdio），每个 Bot 对应一个独立 ACP Session。</td>
  </tr>
</table>

</details>

### 主动唤醒与多通道触达：定时任务与 Webhook

Bot 不必只在浏览器标签页里被动等待输入。通过内置调度器（`schedules`），Bot 可以按固定间隔（`everyMinutes`）或每日指定时刻（`dailyAt`）自动醒来整理待办、巡检状态，并将回执或通知送达主会话；外部脚本、CI 或监控告警也可随时通过 `/hooks/{id}` 唤醒指定 Bot。

<details>
<summary><b>配置示例：</b><code>config/bots.json</code> 中的多 Bot 与定时简报</summary>

```json
{
  "defaultModel": "magpie/mac-mini/auto/hy4flash-free",
  "http": {
    "host": "127.0.0.1",
    "port": 3789,
    "apiToken": "${PEBBLE_API_TOKEN}",
    "webhookSecret": "${PEBBLE_WEBHOOK_SECRET}",
    "tailcat": { "enabled": false }
  },
  "bots": [
    {
      "id": "assistant",
      "name": "Personal Assistant",
      "description": "日常助理：待办、日程、信息整理与日常问答",
      "harness": "opencode",
      "schedules": [
        {
          "name": "morning-brief",
          "dailyAt": "09:00",
          "prompt": "检查今天的重要事项与 memory/ 中的待办，给出一份简洁的晨间简报。",
          "enabled": true
        }
      ]
    }
  ]
}
```

支持读取项目根目录 `.env` 文件，所有 `${VAR}` 环境变量引用在界面改配置或热创建新 Bot 后依然原样保留，密钥不会以明文写盘。

</details>

### 更多特性

- **零账号远程访问（Tailcat）**：内置 [tailcat](https://github.com/tailscale/tailcat)，经 WireGuard 与 DERP 中继打洞直连，服务端与客户端均无需注册 Tailscale 账号或开启系统 VPN。点开 Web UI 左上角「远程访问」扫码或复制 `tailcat://` 链接即可在另一台电脑连接。
- **原生桌面应用**：提供系统菜单栏常驻应用，支持**在这台电脑上运行**与**连接远端 Pebble** 两种模式随时切换。
- **单文件零依赖 Web UI**：整个前端内嵌于单个 `internal/web/index.html`，无框架、无 CDN、无外部 Web 字体，离线与隧道环境秒开；自适应浅色与纯黑深色模式，支持实时流式输出、工具调用提示与快捷指令（`/status`、`/reset`、`/bots`、`/ask`）。
- **终端交互与用量统计**：支持 `pebble chat` 终端直连对话与 `pebble status` 状态巡检，持久化记录每个会话的轮次数、耗时与累计成本。

## 快速开始

开始前请确保本机已安装 **Go** 以及至少一种受支持的 Agent CLI（默认使用 `opencode`，也可使用 `claude`、`pi` 或 `dsh`）。

### 方式一：命令行与常驻服务

适合在本机或常驻主机（如 Mac mini、家庭服务器）上运行完整 Hub：

```bash
# 1. 编译二进制到 bin/pebble（并自动创建 bin/bot-hub 兼容软链）
make build

# 2. （可选）从模板准备自己的配置与环境变量
cp config/bots.example.json config/bots.json

# 3. 启动 Web UI、HTTP/SSE API 与定时调度器
./bin/pebble serve
```

打开浏览器访问 <http://127.0.0.1:3789>。如果只想在终端快速试聊或查看状态：

```bash
./bin/pebble chat assistant
./bin/pebble status
```

### 方式二：桌面应用

适合日常在单机使用，或作为轻量客户端连接另一台主机上的 `pebble`：

```bash
# macOS：构建签名后的 bin/Pebble.app（自动将 pebble、agent-desktop、agent-browser 与可选 bot-host 打入 Contents/Resources/bin）
make app

# 其他平台：构建原生桌面可执行文件 bin/bot-hub-desktop
make desktop
```

macOS 桌面端采用「系统 `PATH` 优先、App 内置 `Contents/Resources/bin` 兜底」的混合策略，并在未指定 `AGENT_BROWSER_EXECUTABLE_PATH` 时自动探测系统默认 Chromium 浏览器（Edge/Chrome/Brave/Arc）。首次打开桌面端时可选择工作模式（之后可随时从系统菜单栏图标「切换模式…」更改）：

1. **在这台电脑上运行**：在系统用户配置目录自动初始化 `config/bots.json`、`state/` 与 `workspaces/` 并启动本机服务。
2. **连接远端 Pebble**：粘贴远端主机生成的 `tailcat://` 链接直接打开聊天界面，无需安装 Tailscale 或配置端口转发。

### 方式三：跨设备远程连接（Tailcat）

当 `pebble` 运行在家中或办公室主机上，需要在外出笔记本上访问时：

1. 在服务端配置 `PEBBLE_API_TOKEN` 并将 `http.tailcat.enabled` 设为 `true`，启动 `./bin/pebble serve`。
2. 在服务端 Web UI 左上角点开「远程访问」，复制 `tailcat://connect/...` 链接。
3. 在另一台电脑使用桌面应用的「连接远端 Pebble」，或直接运行 CLI 在本地端口打开：

```bash
./bin/pebble connect '<tailcat-link>'
# 连接成功后在浏览器打开 http://127.0.0.1:3790
```

## 参考手册

### CLI 命令

| 命令 | 说明 |
| --- | --- |
| `pebble serve [-config path]` | 启动内嵌 Web UI、HTTP/SSE API 与定时调度器 |
| `pebble chat [-config path] [bot]` | 在终端与指定 Bot 交互对话（支持 `/status`、`/reset`、`/bots`、`/ask`、`/exit`） |
| `pebble status [-config path]` | 查看所有已配置 Bot、工作区路径与持久化 Session 统计 |
| `pebble connect [-listen addr] <link>` | 通过 Tailcat 链接将远端 `pebble` 映射到本地地址（默认 `127.0.0.1:3790`） |
| `pebble mcp [--hub-url url] [--bot-id id]` | 启动 stdio MCP 服务（由 Hub 自动为各 Bot 挂载，一般无需手动运行） |

### HTTP / SSE API（默认仅监听 `127.0.0.1:3789`）

- `GET  /`：内嵌多 Bot 网页聊天界面
- `GET  /healthz`：存活探针
- `GET  /api/events`：SSE 实时事件流（`message` / `tool_use` / `bot_status` / `bot_created`）
- `GET  /api/bots`（或 `/api/status`）：查看所有 Bot 状态与会话统计
- `POST /api/bots`：热创建新 Bot
- `GET  /api/bots/{id}`：查看 Bot 详情与 `AGENTS.md` 全文
- `PATCH /api/bots/{id}`：在线修改 Bot 名称、简介、Harness 或模型
- `GET  /api/bots/{id}/messages`：获取持久化聊天记录（支持 `?limit=`）
- `POST /api/bots/{id}/message`：发送消息（支持 `fromBotId` / `hop` 跨 Bot 调用）
- `POST /api/bots/{id}/interrupt`：中断该 Bot 正在执行的轮次
- `POST /api/bots/{id}/reset`：归档当前 Session 并开启新会话（保留 Workspace 和 `memory/`）
- `POST /api/bots/{id}/rotate`：立即或预约本轮结束后归档当前会话（支持 `handoffNote` / `defer`）
- `GET  /api/sessions`、`GET /api/bots/{id}/sessions`：检索或列举历史与活跃会话（支持 `?query=&limit=`）
- `GET  /api/sessions/{sessionId}`、`GET /api/bots/{id}/sessions/{sessionId}`：切片召回指定或 `previous` 会话内容
- `GET  /api/memory`、`POST /api/memory`、`DELETE /api/memory/{id}`：Magpie Memory 记忆透传管理（全局与单 Bot）
- `GET  /api/settings`、`PUT /api/settings`：读取或更新全局模型接入方式（`magpie` / `self`）与默认模型
- `GET  /api/models`：列出指定 `?harness=` 可选的模型列表
- `POST /api/harnesses/{id}/magpie`：通过 Magpie 将指定 Harness CLI 接入网关模型
- `POST /api/login`：用 `http.apiToken` 换取 `HttpOnly` 登录 Cookie（Web UI 使用）
- `GET  /api/tailcat`、`GET /api/tailcat/qr.png`：Tailcat 状态、连接链接与二维码
- `POST /hooks/{id}`：外部 Webhook 唤醒指定 Bot（可选 `X-Webhook-Secret` 或 `Bearer` 校验）

### 项目文档

<table>
  <tr>
    <th align="left" width="180">文档</th>
    <th align="left" width="520">内容</th>
  </tr>
  <tr>
    <td><a href="AGENTS.md"><code>AGENTS.md</code></a></td>
    <td>代码目录结构、开发测试命令与工程约束规范。</td>
  </tr>
  <tr>
    <td><a href="DESIGN.md"><code>DESIGN.md</code></a></td>
    <td>Web UI 与桌面端共用的视觉系统、颜色 Token、圆角间距与动效规范。</td>
  </tr>
  <tr>
    <td><a href="config/bots.example.json"><code>config/bots.example.json</code></a></td>
    <td>完整配置模板，包含多 Bot 与定时任务示例。</td>
  </tr>
</table>

## 安全

- **防跨站与防重绑定**：所有请求校验 `Origin`，拒绝跨站请求（防 CSRF）；仅监听回环地址时同时校验 `Host`（防 DNS rebinding）。
- **令牌强校验**：`http.apiToken`（如 `${PEBBLE_API_TOKEN}`）启用后，`/api/*` 需要 `Authorization: Bearer <token>` 或 `SameSite=Strict` 的 `HttpOnly` 登录 Cookie；当 `host` 不是回环地址时必须设置 `apiToken`，否则拒绝启动。Bot 内的 `pebble mcp` 会自动携带令牌。
- **Tailcat 隧道鉴权**：启用 `http.tailcat.enabled` 必须配合 `http.apiToken`，经隧道进入的所有请求一律强制校验令牌。连接链接内含服务端身份密钥，只应发给自己的设备；删除 `state/tailcat-identity.json` 并重启服务即可立即作废旧链接。默认使用 Tailscale 公共 DERP，也可通过 `derpMapURL` 指向自建 DERP。
- **Webhook 签名与密钥保护**：配置 `http.webhookSecret` 后，`/hooks/{id}` 采用常量时间比对校验密钥；新建或修改 Bot 写回 `config/bots.json` 时始终保留原始 `${VAR}` 引用，密钥绝不以明文落盘。

## 致谢

`Pebble` 站在以下优秀开源项目与工具之上构建：

- **[OpenCode](https://opencode.ai)**、**Claude Code**、**Pi** 与 **DeepSeek Harness**：提供强大的本地 Agent Loop 与工具执行能力。
- **[Magpie](https://usemagpie.ai)**：统一本地模型网关、多订阅路由与配额自动回退。
- **[Tailcat](https://github.com/tailscale/tailcat)**：提供无需账号体系的 WireGuard + DERP 点对点加密隧道。
- **[Wails](https://wails.io)**：驱动轻量的跨平台原生桌面窗口与系统菜单栏集成。
