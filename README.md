<h1 align="center">Pebble</h1>

<p align="center"><b>A minimal personal bot orchestrator running locally on your computer.</b></p>

<p align="center">
  <b>1 Bot = 1 Workspace = 1 Session</b>&nbsp;&nbsp;·&nbsp;&nbsp;<b>Bring Your Own Harness</b>&nbsp;&nbsp;·&nbsp;&nbsp;<b>Local-First</b>
</p>

<p align="center">
  <img height="26" alt="Go Standard Library" src="https://img.shields.io/badge/Go-Stdlib%20First-1d1b19?logo=go&logoColor=white&style=for-the-badge">
  <a href="https://usemagpie.ai"><img height="26" alt="Powered by Magpie" src="https://img.shields.io/badge/Gateway-Magpie-d9774b?style=for-the-badge"></a>
  <a href="https://github.com/tailscale/tailcat"><img height="26" alt="Remote via Tailcat" src="https://img.shields.io/badge/Tunnel-Tailcat-1d1b19?style=for-the-badge"></a>
  <a href="https://wails.io"><img height="26" alt="Desktop via Wails" src="https://img.shields.io/badge/Desktop-Wails%20v3-1d1b19?style=for-the-badge"></a>
</p>

<p align="center">
  <b>English</b>&nbsp;&nbsp;·&nbsp;&nbsp;<a href="README.zh-CN.md">简体中文</a>
</p>

- **A persistent bot team on your machine.** Built strictly around `1 Bot = 1 Workspace = 1 Session`: every bot gets its own directory, `AGENTS.md` instructions, serialized execution queue, and long-lived session.
- **Bring your own agent runtime.** Drives **OpenCode**, **Claude Code**, **Pi**, and **DeepSeek Harness (`dsh`)** directly on your computer—routed through **Magpie** with automatic quota fallback, or running on the CLI subscriptions you already pay for.
- **Local data, reachable anywhere.** One Go binary ships the Web UI, scheduled routines, and a **Tailcat** peer-to-peer tunnel—no public IP, no Tailscale account, and no system VPN required.

## Cool features

### Isolated workspaces, shared memory: `1 Bot = 1 Workspace = 1 Session`

Mixing daily tasks, deep research, and ops checks in a single chat thread pollutes context fast. `pebble` gives every bot an isolated workspace on disk, one persistent session, and a dedicated serial lane (`botLane`): messages from the Web UI, schedules, or other bots run in order without stepping on each other, and any running turn can be interrupted on the spot.

Every workspace also symlinks a shared `_shared/` folder while keeping its own `memory/` directory. Write your preferences once in `_shared/USER.md` and every bot reads them; save ongoing decisions in `memory/` and they survive even when a session is reset.

<details>
<summary><b>On disk:</b> how workspaces, memory, and state are laid out</summary>

```text
pebble/
├── config/bots.json              # Bot roster, model access & schedules (keeps ${VAR} refs)
├── state/
│   ├── hub.db                    # SQLite store (active + archived sessions, FTS5 message search)
│   ├── sessions.json             # Legacy session snapshot (auto-imported on first run)
│   └── messages/                 # Legacy JSONL chat logs (kept read-only after import)
└── workspaces/
    ├── _shared/                  # Global context (USER.md + hub-managed PEBBLE.md)
    └── assistant/                # Dedicated workspace for one bot
        ├── AGENTS.md             # Role definition and workspace rules
        ├── memory/               # Durable local notes (complemented by Magpie Memory)
        ├── _shared -> ../_shared # Automatic symlink to shared context
        ├── .agents/skills/pebble-setup/SKILL.md # Managed Pebble setup/CLI skill
        └── opencode.json         # Auto-wired local pebble MCP & PEBBLE.md instructions
```

Resetting a bot (`POST /api/bots/{id}/reset` or `/reset`) archives the current session into `state/hub.db` so a clean session starts while `AGENTS.md`, `memory/`, workspace files, and sliced session recall remain intact.

</details>

### Self-organizing bots: Spin up specialists and delegate work

Every bot automatically receives the built-in `pebble mcp` server (stdio JSON-RPC 2.0). When a task goes beyond one bot's role, it can inspect existing teammates, hot-create a new persistent specialist bot with its own workspace and instructions, or message another bot directly and wait for its answer.

Bot-to-bot calls enforce `hop <= 4` loop protection and appear live in **both** bots' chat timelines. You can also delegate directly from any chat input with `/ask <bot> <message>`.

<details>
<summary><b>MCP core tools &amp; full CLI:</b> runtime MCP primitives + full <code>pebble</code> CLI</summary>

<table>
  <tr>
    <th align="left" width="160">MCP Tool</th>
    <th align="left" width="540">What it does</th>
  </tr>
  <tr>
    <td><code>message_bot</code></td>
    <td>Sends a task to another bot and returns its reply, recording the exchange in both timelines (capped at <code>hop &lt;= 4</code>).</td>
  </tr>
  <tr>
    <td><code>recall_session</code></td>
    <td>Fetches sliced messages from a prior or active session with per-message truncation and single-message full fetch.</td>
  </tr>
  <tr>
    <td><code>desktop_task</code></td>
    <td>Delegates multi-step desktop GUI or visible-browser tasks to an isolated computer-use worker on the visible desktop.</td>
  </tr>
  <tr>
    <td><code>request_takeover</code></td>
    <td>Pauses automation and asks the user to complete QR login, 2FA, CAPTCHA, or payment in the Remote Desktop view until resumed.</td>
  </tr>
  <tr>
    <td><code>node_exec</code></td>
    <td>Runs a shell command on a connected remote node (including remote <code>agent-desktop</code> / <code>agent-browser</code> calls).</td>
  </tr>
  <tr>
    <td><code>node_file</code></td>
    <td>Reads, writes, or lists files on a connected remote node.</td>
  </tr>
</table>

<p><b>Full <code>pebble</code> CLI</b> (auto-detects current bot context from env or workspace MCP config, outputs JSON): <code>pebble bot list|get|create|update|delete|message|send</code>, <code>pebble settings get|models|set</code>, <code>pebble session list|recall|rotate</code>, <code>pebble routine list|upsert|delete|run</code>, <code>pebble node list|exec|file</code>, <code>pebble desktop targets|task|takeover|setup|power</code>.</p>

<p>For GUI and web automation, bots call <code>agent-desktop</code> and <code>agent-browser</code> directly via shell (or via <code>node_exec</code> on remote nodes), preferring <code>agent-browser --auto-connect --pin-tab</code> to attach to the running Chromium browser before falling back to headless or headed isolated profiles.</p>

</details>

### Bring your own harness: OpenCode, Claude Code, Pi or DeepSeek

`pebble` does not lock you into a custom agent loop or provider SDK. Different bots in the same hub can run on different harnesses side by side, with two `modelAccess` modes switchable straight from the Web UI:

- **Magpie gateway (`magpie`, default):** Shares [Magpie](https://usemagpie.ai)'s local model catalog, routing rules, and quota fallback across harnesses, with one-click CLI binding from the settings panel.
- **Native CLI config (`self`):** Leaves global CLI configs untouched so each runtime uses its own subscription or API keys as-is.

<details>
<summary><b>Supported harnesses:</b> process model, protocol, and session persistence</summary>

<table>
  <tr>
    <th align="left" width="120">Harness</th>
    <th align="left" width="180">Runtime</th>
    <th align="left" width="400">How Pebble drives it</th>
  </tr>
  <tr>
    <td><code>opencode</code></td>
    <td>OpenCode (default)</td>
    <td>Manages one shared <code>opencode serve</code> process (or connects to an external server), driving sessions over HTTP + a single SSE stream with workspace <code>opencode.json</code> MCP wiring.</td>
  </tr>
  <tr>
    <td><code>claude</code></td>
    <td>Claude Code</td>
    <td>Keeps one long-lived <code>claude -p --input-format stream-json</code> process per bot, bridging <code>CLAUDE.md</code> → <code>@AGENTS.md</code> and passing MCP config per process.</td>
  </tr>
  <tr>
    <td><code>pi</code></td>
    <td>Pi</td>
    <td>Keeps one long-lived <code>pi --mode rpc</code> process per bot; the hub owns the session ID so restarted processes reopen the exact same session.</td>
  </tr>
  <tr>
    <td><code>dsh</code></td>
    <td>DeepSeek Harness</td>
    <td>Multiplexes bots as independent ACP sessions over a single long-lived <code>dsh --profile acp</code> stdio connection.</td>
  </tr>
</table>

</details>

### Proactive routines: It reaches out when it matters

Bots do not just wait in a browser tab. Built-in `schedules` wake a bot up at a fixed interval (`everyMinutes`) or time of day (`dailyAt`) to review `memory/`, check ongoing work, and deliver receipts or notifications to the main session. External scripts, CI pipelines, and monitors can also wake any bot on demand through `/hooks/{id}`.

<details>
<summary><b>Configuration:</b> multi-bot setup and daily briefs in <code>config/bots.json</code></summary>

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
      "description": "Daily tasks, notes, and personal briefings",
      "harness": "opencode",
      "schedules": [
        {
          "name": "morning-brief",
          "dailyAt": "09:00",
          "prompt": "Check today's priorities and open items in memory/, then write a concise morning brief.",
          "enabled": true
        }
      ]
    }
  ]
}
```

`.env` in the project root is loaded automatically. Whenever bots or settings are updated from the UI or MCP tools, `${VAR}` references stay as references on disk—secrets are never rewritten in plaintext.

</details>

### More features

- **Account-free remote access (Tailcat):** Built-in [tailcat](https://github.com/tailscale/tailcat) punches through NAT over WireGuard + DERP without a Tailscale account, login, or system-wide VPN on either machine. Open **远程访问** in the Web UI to copy the `tailcat://` link or scan the QR code.
- **Native desktop app:** Lives in the system menu bar / tray and switches cleanly between **Run on this computer** and **Connect to remote Pebble**.
- **Single-file embedded Web UI:** Ships inside the binary (`internal/web/index.html`) with zero frontend frameworks, CDNs, or web fonts—instant to load offline or over a tunnel, with light and pure-black dark modes, live SSE streaming, tool-call indicators, and slash commands (`/status`, `/reset`, `/bots`, `/ask`).
- **Terminal CLI & cost tracking:** Chat directly from a shell with `pebble chat` and track per-bot session turns, latency, and cumulative USD cost with `pebble status`.

## Quick start

You need **Go** and at least one supported agent CLI on `PATH` (`opencode` by default, or `claude`, `pi`, `dsh`).

### Option 1: CLI & background server

Best for running the hub on your workstation, Mac mini, or home server:

```bash
# 1. Build bin/pebble (with bin/bot-hub compatibility symlink)
make build

# 2. (Optional) Create your local config from the example
cp config/bots.example.json config/bots.json

# 3. Start Web UI, HTTP/SSE API, and scheduler
./bin/pebble serve
```

Open <http://127.0.0.1:3789> in your browser. To chat or inspect sessions straight from the terminal:

```bash
./bin/pebble chat assistant
./bin/pebble status
```

### Option 2: the desktop app

Best for everyday single-machine use, or as a native window connected to a remote `pebble` hub:

```bash
# macOS: build ad-hoc signed bin/Pebble.app (bundles agent-desktop, agent-browser, and optional bot-host into Contents/Resources/bin)
make app

# Other platforms: build native bin/bot-hub-desktop binary
make desktop
```

On macOS, the desktop app keeps system `PATH` binaries first and appends `Contents/Resources/bin` as a self-contained fallback, while automatically pointing `AGENT_BROWSER_EXECUTABLE_PATH` at your default Chromium browser (Edge/Chrome/Brave/Arc) when unset. On first launch, pick how this computer should run (you can switch anytime from the menu bar icon):

1. **Run locally:** Initializes `config/bots.json`, `state/`, and `workspaces/` under your OS user config directory and starts the hub in-process.
2. **Connect to a remote Pebble:** Paste a `tailcat://` link from another machine—no Tailscale installation required.

### Option 3: remote access over Tailcat

To reach a `pebble` hub running at home or in the office from your laptop:

1. Set `PEBBLE_API_TOKEN` and `"tailcat": { "enabled": true }` on the server, then start `./bin/pebble serve`.
2. Click **远程访问** in the server's Web UI and copy the `tailcat://connect/...` link.
3. Paste the link into the Desktop App on your second machine, or forward it to a local port via CLI:

```bash
./bin/pebble connect '<tailcat-link>'
# Open http://127.0.0.1:3790 in your browser
```

## Reference

### CLI commands

| Command | Description |
| --- | --- |
| `pebble serve [-config path]` | Start Web UI, HTTP/SSE API, and scheduler |
| `pebble chat [-config path] [bot]` | Interactive terminal chat (`/status`, `/reset`, `/bots`, `/ask`, `/exit`) |
| `pebble status [-config path]` | Show configured bots, workspaces, and persisted session stats |
| `pebble connect [-listen addr] <link>` | Expose a remote Pebble hub over Tailcat on a local address (default `127.0.0.1:3790`) |
| `pebble mcp [--hub-url url] [--bot-id id]` | Run stdio MCP server (auto-wired into bots by the hub) |

### HTTP / SSE API (listens on `127.0.0.1:3789` by default)

- `GET  /` — Embedded multi-bot Web UI
- `GET  /healthz` — Liveness check
- `GET  /api/events` — Real-time SSE stream (`message` / `tool_use` / `bot_status` / `bot_created`)
- `GET  /api/bots` (alias `/api/status`) — List all bots and session stats
- `POST /api/bots` — Hot-create and start a new bot
- `GET  /api/bots/{id}` — Bot details and full `AGENTS.md` content
- `PATCH /api/bots/{id}` — Update a bot's name, description, harness, or model
- `GET  /api/bots/{id}/messages` — Persisted chat history (`?limit=`)
- `POST /api/bots/{id}/message` — Send a message (supports `fromBotId` / `hop` for cross-bot calls)
- `POST /api/bots/{id}/interrupt` — Interrupt the bot's in-flight turn
- `POST /api/bots/{id}/reset` — Archive active session while keeping workspace and `memory/`
- `POST /api/bots/{id}/rotate` — Archive active session immediately or after current turn (`handoffNote`, `defer`)
- `GET  /api/sessions`, `GET /api/bots/{id}/sessions` — List or search archived and active sessions (`?query=&limit=`)
- `GET  /api/sessions/{sessionId}`, `GET /api/bots/{id}/sessions/{sessionId}` — Sliced session transcript recall (`previous` supported)
- `GET  /api/memory`, `POST /api/memory`, `DELETE /api/memory/{id}` — Magpie Memory pass-through (global & bot-scoped)
- `GET  /api/settings`, `PUT /api/settings` — Read or update `modelAccess` (`magpie` / `self`), default models, and `magpieWebUrl` (where remote clients open `magpie web`; falls back to `MAGPIE_WEB_URL`)
- `GET  /api/models` — List available model IDs for `?harness=`
- `POST /api/harnesses/{id}/magpie` — Connect a harness CLI to Magpie with a chosen model
- `POST /api/magpie/open` — Open the Magpie app on the host to manage providers and models
- `POST /api/login` — Exchange `http.apiToken` for an `HttpOnly` session cookie (used by Web UI)
- `GET  /api/tailcat`, `GET /api/tailcat/qr.png` — Tailcat tunnel status, link, and QR code
- `POST /hooks/{id}` — Wake a bot from an external webhook (optional `X-Webhook-Secret` or `Bearer` auth)

### Project docs

<table>
  <tr>
    <th align="left" width="180">Document</th>
    <th align="left" width="520">What it covers</th>
  </tr>
  <tr>
    <td><a href="AGENTS.md"><code>AGENTS.md</code></a></td>
    <td>Repository layout, build &amp; test commands, and engineering rules.</td>
  </tr>
  <tr>
    <td><a href="DESIGN.md"><code>DESIGN.md</code></a></td>
    <td>Visual design system, color tokens, spacing, and motion rules for the Web &amp; desktop UI.</td>
  </tr>
  <tr>
    <td><a href="apps/apple/README.md"><code>apps/apple</code></a></td>
    <td>Native SwiftUI client for iPhone and iPad.</td>
  </tr>
  <tr>
    <td><a href="config/bots.example.json"><code>config/bots.example.json</code></a></td>
    <td>Starter configuration with multiple bots and scheduled routines.</td>
  </tr>
</table>

## Security

- **CSRF & DNS-rebinding protection:** Every request checks `Origin` and rejects cross-origin calls; when bound to a loopback address, the server also validates `Host` against loopback names.
- **API token enforcement:** When `http.apiToken` (e.g. `${PEBBLE_API_TOKEN}`) is set, `/api/*` requires `Authorization: Bearer <token>` or a `SameSite=Strict`, `HttpOnly` login cookie. Binding to a non-loopback `host` without `http.apiToken` refuses to start. Bots' `pebble mcp` subprocesses receive the token automatically.
- **Authenticated Tailcat tunnel:** Enabling `http.tailcat.enabled` requires `http.apiToken`, and every tunneled request must present the token. The `tailcat://` link embeds the server identity key—share it only with your own devices; delete `state/tailcat-identity.json` and restart to revoke old links immediately. Tailscale's public DERP map is used by default, or point `derpMapURL` at a self-hosted DERP server.
- **Webhook & secret hygiene:** `http.webhookSecret` is verified in constant time on `/hooks/{id}`. Creating or updating bots rewrites `config/bots.json` from the unexpanded source so `${VAR}` references never turn into plaintext secrets on disk.

## Acknowledgements

`Pebble` builds on these projects:

- **[OpenCode](https://opencode.ai)**, **Claude Code**, **Pi**, and **DeepSeek Harness:** The agent runtimes that execute tools and turns inside each workspace.
- **[Magpie](https://usemagpie.ai):** Local model gateway providing a unified model catalog, routing rules, and quota fallback across CLIs.
- **[Tailcat](https://github.com/tailscale/tailcat):** Account-free peer-to-peer encrypted tunneling over WireGuard and DERP.
- **[Wails](https://wails.io):** Lightweight native desktop window and system tray wrapper.
