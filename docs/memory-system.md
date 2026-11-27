# Pebble Memory System

Pebble Memory (`internal/memory`) provides a pure-Go, zero-CGO, file-first persistent memory engine for Pebble bots and shared user preferences. Markdown files under `workspaces/` are the canonical Source of Truth (SoT), while SQLite (`state/hub.db`) maintains a rebuildable derived index for hybrid search, progressive disclosure, usage statistics, and Dream consolidation.

## Architecture Overview

```text
workspaces/
├── _shared/
│   ├── USER.md                       # Read-only user profile (never overwritten by memory engine)
│   └── memory/
│       ├── MEMORY.md                 # Curated L0 global index (max 100 lines / ~4KB)
│       ├── items/<slug>-<shortid>.md # Durable global memory items (YAML frontmatter + Markdown)
│       ├── daily/YYYY-MM-DD.md       # Append-only global working logs
│       └── dreams/YYYY-MM-DD.md      # Dream consolidation audit diary
└── <bot>/
    └── memory/
        ├── MEMORY.md                 # Curated L0 bot index (max 150 lines / ~6KB)
        ├── items/<slug>-<shortid>.md # Durable bot memory items
        ├── daily/YYYY-MM-DD.md       # Append-only bot working logs & session flushes
        └── dreams/YYYY-MM-DD.md      # Bot Dream consolidation audit diary

state/hub.db (derived, rebuildable via Reindex())
├── memory_docs                       # Parsed frontmatter, body, mtime/size/sha256
├── memory_chunks                     # Heading/paragraph chunks with line ranges
├── memory_fts_uni                    # FTS5 unicode61 (remove_diacritics 2)
├── memory_fts_tri                    # FTS5 trigram (CJK substrings & code identifiers)
├── memory_embeddings                 # Optional Tier 1 float32 little-endian vectors
├── memory_stats                      # SQLite-only access_count & last_accessed_at
└── memory_dismissed                  # Fingerprints of dismissed memories (Dream guard)
```

### Core Invariants

1. **File-first Markdown SoT**: All durable memories, indexes, daily logs, and dream diaries live as human-editable Markdown files written atomically via temp-file + `fsync` + `rename`.
2. **Zero-Churn Reads**: `memory_stats` (access counts and last-accessed timestamps) are persisted exclusively in SQLite and **never** written back to Markdown, preventing `internal/syncer` and Git checkpoints from churning on reads.
3. **Protected Workspace Files**: The memory engine never touches `AGENTS.md` and never overwrites `_shared/USER.md`. Path reads via `Get` are strictly jailed to `_shared/USER.md` and `<owner>/memory/`.
4. **SecretGuard**: Every write (`Save`, `Patch`, `WriteIndex`, `ImportFromMagpie`, `ApplyDreamPlan`, `ApplyFlushNote`) rejects plaintext credentials (PEM private keys, `sk-...`, `ghp_...`, `AIza...`, `tvly-...`, `Bearer <token>`, AWS/Slack/Telegram/Stripe keys, and secret assignments) with a structured `SecretError` (`code: "secret_detected"`).

## Disk Schema (`items/<slug>-<shortid>.md`)

```yaml
---
id: mem_a1b2c3d4
scope: bot
bot_id: assistant
kind: rule
title: "Keep deployment changes behind feature flags"
status: active
confidence: 0.9
pin: true
tags: [deploy, safety]
created_at: "2026-11-27T08:00:00Z"
updated_at: "2026-11-27T08:00:00Z"
valid_from: "2026-11-27T08:00:00Z"
valid_until: ""
superseded_by: ""
source_session: ses_123
source_author: user
why: "Requested during release retrospective"
---

Always guard risky production migrations with a config flag before rollout.
```

Supported values:
- `scope`: `global` | `bot`
- `kind`: `profile` | `preference` | `rule` | `fact` | `decision` | `procedure` | `reference` | `episode`
- `status`: `active` | `candidate` | `superseded` | `archived`
- `source_author`: `user` | `agent` | `dream` | `magpie_import`

## Incremental Indexing & Hybrid Retrieval

### Incremental Sync & Reindex
- `SyncFromDisk(ctx)` walks `_shared/memory/` and `<bot>/memory/`, skipping files whose `(mtime_unix, size)` match `memory_docs`, falling back to `sha256` verification before re-parsing and re-chunking. Unchanged chunk embeddings are preserved across edits.
- `Reindex(ctx)` rebuilds `memory_docs`, `memory_chunks`, and both FTS5 tables from disk while preserving `memory_stats` and `memory_dismissed`.

### Progressive Disclosure (L0 / L1 / L2)
- **L0**: Frozen session snapshot (`BuildSnapshot`) injects `_shared/USER.md`, curated `MEMORY.md`, and recent 48h `daily/` headings at session startup.
- **L1**: `Search(ctx, SearchOpts{Compact: true})` returns scored hits with compact `Snippet` excerpts and metadata without full bodies.
- **L2**: `Get(ctx, GetOpts{ID | Path, StartLine, EndLine})` reads the complete item or line-sliced Markdown file on demand.

### Retrieval Tiers
- **Tier 0 (Zero-dep default)**: Parallel `memory_fts_uni` (`unicode61 remove_diacritics 2`) + `memory_fts_tri` (`trigram`) + `<3`-rune `LIKE` fallback (handles 1–2 character CJK queries and short acronyms) + BM25 weighting (`title` > `tags` > `body`) + exponential recency decay (exempting `pin=true` and `kind` in `rule`/`preference`/`profile`) + `memory_stats` logarithmic frequency boost + MMR diversity reranking.
- **Tier 1 (Optional Semantic)**: OpenAI-compatible `/v1/embeddings` client (`EmbeddingConfig{Enabled, BaseURL, APIKey, Model}`) storing little-endian float32 BLOBs in `memory_embeddings`, pure-Go cosine scan, fused with Tier 0 via Reciprocal Rank Fusion (RRF, `k=60`) and MMR (`lambda=0.75`). Falls back gracefully to Tier 0 if the embedding endpoint is unavailable.
- **Session Fusion**: When `IncludeSessions: true`, also queries `messages_fts` in `hub.db` (strictly honoring `botId` and `excludedBotIds` privacy isolation) and tags hits with `origin: "session"`.

### Pre-Write Duplicate Detection
When creating a new durable item (`Save` without `ID`), Pebble computes lexical n-gram overlap (English word tokens + CJK character bigrams) against active items in the same scope and returns high-overlap matches in `SaveResult.SimilarExisting`.

## Magpie Memory Coexistence & Migration

- **Detection (`DetectMagpieMemory`)**: Inspects `~/.config/magpie/library.json` (the `magpie-memory` MCP entry's `agents` list) and `~/.config/magpie/memory/index.json` (or `m_*.md` fallback) with a 30s TTL cache. Exposes `HarnessHasMagpieMemory(harnessKind)`.
- **Coexistence Modes**:
  - `auto` (default): When the bot's active harness has `magpie-memory` injected, `BuildSnapshot` omits global items with `source_author: "magpie_import"` from the rendered global summary and appends a concise 2-line single-source boundary directive (use Pebble for bot/team memory, treat ambient Magpie memory as read-only, never double-write; private bots must never write to external `magpie-memory`).
  - `isolated` (with legacy aliases `pebble_only` / `dual`): Disables automatic Magpie omission while preserving private-session boundaries.
- **Direct Importer (`ImportFromMagpie`)**: Reads `~/.config/magpie/memory/m_*.md` directly without spawning subprocesses:
  - `mode="bots_only"` (default): Migrates items tagged `#bot:<id>` into `workspaces/<bot>/memory/items/`.
  - `mode="all"`: Also migrates active `scope: global` items into `workspaces/_shared/memory/items/`.
  - Tags migrated items with `source_author: "magpie_import"`, validates via SecretGuard, and deduplicates idempotently by deterministic ID (`imp_<id>`) and content fingerprint.

## Frozen Session Snapshot

`BuildSnapshot(ctx, SnapshotOpts)` outputs a deterministic `<pebble_memory_snapshot kind="reference-data">...</pebble_memory_snapshot>` block bounded by `MaxChars` (default 6000) so prompt-cache prefixes remain stable across turns within a session.

## Dream Consolidation & Session Flush Lifecycle

1. **Pre-Rotation / Post-Overflow Flush**: `BuildFlushPrompt` and `ApplyFlushNote` extract concise working notes before session rotation and append them to `daily/YYYY-MM-DD.md` (ignoring `NOOP`/`NONE`, enforced by SecretGuard).
2. **Dream Consolidation**:
   - Tracked incrementally per scope via `GetDreamCursor` / `SetDreamCursor` in `state.Store` `meta`.
   - `BuildDreamPrompt` assembles active items, dismissed hints, recent daily logs, and session excerpts into a strict JSON consolidation prompt.
   - `ParseDreamPlan` extracts and validates `ADD`, `UPDATE`, `SUPERSEDE`, `ARCHIVE`, and `NOOP` operations.
   - `ApplyDreamPlan` enforces SecretGuard, refuses to mutate `pin=true` items, blocks re-adding `memory_dismissed` fingerprints, marks global inferred items as `status: "candidate"`, rebuilds `MEMORY.md` within its line/byte budget (`RebuildIndex`), and appends an audit record to `dreams/YYYY-MM-DD.md`.

## Shared HTTP / MCP / CLI Contract

Exported DTO JSON shapes in `internal/memory`:

- **`ListResponse`**: `backend`, `installed`, `available`, `error`, `magpie`, `semantic`, `counts`, `items`
- **`Item`**: `id`, `path`, `scope`, `botId`, `kind`, `title`, `text`, `snippet`, `why`, `tags`, `status`, `pin`, `confidence`, `validFrom`, `validUntil`, `supersededBy`, `sourceSession`, `sourceAuthor`, `origin`, `score`, `createdAt`, `updatedAt`
- **`IndexDoc`**: `path`, `content`, `lines`, `maxLines`
- **`StatusSummary`**: `backend`, `config` (redacted, never exposes `apiKey`), `docCount`, `chunkCount`, `counts`, `semantic`, `magpie`, `recentDreams`

Typical Wave-2 wiring surfaces:
- **HTTP API**: `GET /api/memory`, `POST /api/memory`, `GET /api/memory/{id}`, `PATCH /api/memory/{id}`, `DELETE /api/memory/{id}`, `GET/PUT /api/memory/index`, `GET /api/memory/status`, `POST /api/memory/reindex`, `POST /api/memory/import/magpie`, `POST /api/memory/dream`.
- **MCP Tools**: `recall_memory` (L1 compact `Search`), `read_memory` (L2 `Get`), `save_memory` (`Save`), `forget_memory` (`Forget`).
- **CLI**: `pebble memory search|get|save|patch|forget|index|status|reindex|import-magpie|dream`.
