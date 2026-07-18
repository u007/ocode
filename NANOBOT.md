# NANOBOT.md — Capability Comparison: `~/www/nanobot` vs `ocode`

> Purpose: a reference notes file answering *"what can `~/www/nanobot` do that
> our agent (ocode) cannot?"*. Source of truth for the comparison: nanobot
> source at `/Users/james/www/nanobot` (`CLAUDE.md`, `README.md`, `docs/`,
> `nanobot/`) and ocode source at this repo (`AGENTS.md`, `internal/`).
> Last reviewed: 2026-07-17.

## TL;DR

- **nanobot** = an ultra-lightweight **personal AI assistant framework**
  (Python, ~4k LOC core) — a multi-channel, multi-tenant **assistant/ops
  platform** (chat platforms, remote-machine management, ticketing, network
  ops). See `nanobot/CLAUDE.md:5-7`, `nanobot/README.md:65-85`.
- **ocode** = a Go-based **AI coding agent** — TUI/CLI/web/desktop, built
  around filesystem/shell/LSP/git tooling for software work. See `AGENTS.md`.

They target different jobs. The gap below is essentially "everything outside
*write/run code in a terminal*": conversational presence on 10+ chat/email
platforms, scheduled jobs, remote-machine patch/desktop management, ticketing /
network ops, SQL enrichment, multi-tenant user isolation, and structured
document evaluation.

---

## What nanobot can do that ocode **cannot**

### 1. Multi-platform conversational channels (inbound chat)
nanobot connects as a bot/user on 10+ platforms with inbound messaging and
media routing.
- Evidence: `nanobot/channels/` → `telegram.py`, `discord.py`, `whatsapp.py`,
  `feishu.py`, `mochat.py`, `dingtalk.py`, `slack.py`, `email.py`, `qq.py`,
  `matrix.py`, `http_channel.py`; `nanobot/README.md:196-209`, `:652`.
- ocode: terminal/IDE only. It has a **Telegram remote-control** path
  (`internal/server/handler_sse.go:88`, `internal/tui/model.go:10851`) but that
  is external *approval/driving of the agent*, not a conversational channel.

### 2. Proactive outbound messaging to any contact
A `message` tool resolves a contact by name and sends on any connected channel;
`user_info` does scoped contact lookup.
- Evidence: `nanobot/docs/ARCHITECTURE.md:50-67`
  (`nanobot/agent/tools/message.py`, `user_info.py`).
- ocode: cannot message external users on any platform.

### 3. Scheduled / recurring tasks (cron)
`nanobot cron add --cron "0 9 * * *"` runs jobs independent of any live session.
- Evidence: `nanobot/cron/` (`service.py`, `types.py`);
  `nanobot/README.md:1351-1362`.
- ocode: has a scheduler — new `internal/scheduler` package (persistent disk-backed cron engine with `at`/`every`/`cron` schedules, headless `agent.Step` dispatch via persistent `cron:<id>` sessions, per-job permission modes, and multi-sink delivery to Telegram + the live TUI). Exposed through a `cron` LLM tool, a `/cron` TUI slash command, and REST endpoints (`/api/cron`, `/api/cron/{id}`, `/api/cron/outbox`, `/api/cron/targets`). Runs in the long-lived `serve`/`web`/desktop hosts; the TUI only authors/manages jobs. (`docs/scheduled-jobs.md`, `internal/scheduler/*`, `internal/tool/cron.go`).

### 4. Remote Worker + Patch Management (ops)
A cross-platform Go **worker agent** (`nanobot/worker/*.go`) runs on remote
machines for software inventory, version monitoring, patching/updates, and
service installation — plus WebRTC **remote desktop** (clipboard sync, file
drop, session replay).
- Evidence: `nanobot/worker/` (`inventory.go`, `monitor/`, `executor/`,
  `nanoworker*`); `nanobot/nanobot/worker/` (`manager.py`, `control/`);
  `nanobot/docs/PATCH_MANAGER.md`, `nanobot/docs/REMOTE_CONTROL.md`.
- ocode: can only run `bash` on the **local** machine; no fleet/patch/remote-
  desktop concept.

### 5. PSA ticketing integration
Two-way sync with ticketing systems (Freshservice), dedup, auto-resolve, reaper.
- Evidence: `nanobot/psa/` → `freshservice.py`, `webhook.py`, `sync.py`,
  `reaper.py`, `dispatcher.py`; `nanobot/docs/PSA.md`.
- ocode: none.

### 6. Network monitoring + rule engine + alerting
- Evidence: `nanobot/network/` → `rules.py`, `alerts.py`, `presets.py`,
  `enrichment.py`; `nanobot/docs/superpowers/specs/2026-04-16-network-monitor-design.md`.
- ocode: none.

### 7. SQL database integration & enrichment
Connect to PostgreSQL/MySQL/SQL Server, list tables, execute queries, LLM-powered
column descriptions, encrypted connection storage, web UI page.
- Evidence: `nanobot/README.md:29`, `:1150` ("SQL Database Enrichment").
- ocode: no SQL/dataset tool (grep of `internal/` found none).

### 8. Multi-tenant users & teams + per-user isolation + roles
Canonical `user_id` (ContextVar), per-user home (`~/.nanobot/users/{id}/`),
team namespaces, Admin/CLI/Channel-user roles, per-user/team config overrides,
sandboxing.
- Evidence: `nanobot/ROLES.md:1-50`, `nanobot/docs/CONFIGURATION.md`,
  `nanobot/README.md:1233-1272`.
- ocode: single local developer; permission modes are about *tool safety*,
  not multi-tenant user isolation (`AGENTS.md` permissions section).

### 9. Deterministic document requirement evaluation
`evaluate_requirements`: 6-stage pipeline judging whether an output doc satisfies
a source doc's requirements, with confidence scores.
- Evidence: `nanobot/docs/ARCHITECTURE.md:69-80`.
- ocode: none.

### 10. Document Tree Retrieval
Section-level structure extraction + LLM traversal + page citations.
- Evidence: `nanobot/docs/DOCUMENT_TREES.md`.
- ocode: web fetch/search only; no section-level doc-tree retrieval.

### 11. Corpus knowledge search across network drives (FTS5, namespace-scoped)
`knowledge_search` over network/OneDrive/Google-Drive mounts, scoped
`user`/`team`/`global`, FTS5 with mtime idempotency.
- Evidence: `nanobot/docs/ARCHITECTURE.md:82-87` (`knowledge_base.py`).
- ocode: OKF knowledge bundle is markdown-embedding semantic search
  (`docs/`), narrower scope.

### 12. Agent Social Network
Joins an external agent community automatically.
- Evidence: `nanobot/README.md:652-654`.
- ocode: none.

### 13. Email channel + email tool
Personal email assistant — IMAP poll / SMTP reply.
- Evidence: `nanobot/README.md:564-595`, `nanobot/channels/email.py`.
- ocode: none.

### 14. Additional nanobot subsystems ocode lacks
- `nanobot/doccompare/` — document comparison.
- `nanobot/heartbeat/` — fleet heartbeat monitoring.
- `nanobot/pairing/` + device pairing / remote-control of other instances
  (`nanobot/docs/REMOTE_CONTROL.md`).
- `nanobot/manus/` — Manus-protocol high-precision autonomous loop
  (Planner/Executor/Verifier) (`nanobot/README.md:46`).
- `nanobot/apps/` + `nanobot/frontend/` — end-user React web UI (distinct from
  ocode's dev-facing web SPA).
- `nanobot/docs/LCM.md` — Lossless Context Management (hierarchical
  summarization) vs ocode's compaction/truncation.
- `nanobot/recipes/`, `nanobot/security/` (hardening), `nanobot/failures/`
  (failure handling), `nanobot/bus/` (async message-bus decoupling channels
  from agent core — `nanobot/docs/ARCHITECTURE.md:32-36`).

---

## For balance — what ocode can do that nanobot generally can't
- Rich **TUI** (mouse selection, find bar, sidebar, themes, in-app text
  selection) + **native desktop app** (Wails v3).
- **LSP integration** (go-to-def, diagnostics, autocomplete) — nanobot has no LSP.
- Code-centric tooling: **file-edit snapshots + `undo_file_change`**, git
  integration, code-review/orchestrator sub-agents.
- **Plugin system** (`plugin.json`: custom tools/slash/MCP), **prompt-cache**
  optimization, reasoning-effort controls.
- Broader native provider set (Z.AI, Alibaba, Minimax) + its own **OKF
  knowledge bundle** with a context-writer sub-agent.
- Fine-grained **permission modes** (YOLO/locked, exfiltration detection, path
  scoping).
- OCR + image generation, Google Workspace skills, browser-automation skill.

---

## Suggested next steps (if we want to close the gap)
1. **Cron/scheduling** — smallest, highest-value port (a `cron` subsystem +
   scheduled-job executor). ocode already has the agent loop; it just needs a
   trigger source.
2. **One chat channel (Telegram/Discord)** — inbound chat adapter feeding the
   existing agent loop; would turn ocode into a conversational agent. Note the
   existing Telegram remote-control path is a head-start for approvals.
3. **SQL enrichment tool** — a `sql` tool + connection store; natural fit for a
   coding agent that already runs `bash`.
4. **Remote worker / patch** and **PSA ticketing / network monitoring** are the
   largest efforts and the least aligned with ocode's coding-agent mission —
   likely out of scope unless ocode pivots toward ops.

## Validation
- nanobot side: read `CLAUDE.md`, `SKILLS.md`, `ROLES.md`, `docs/ARCHITECTURE.md`,
  `README.md` (feature/heading map); enumerated `nanobot/{channels,cron,psa,
  network,worker,patch,manus,...}`.
- ocode side: grepped `internal/` for `cron|sql|email|whatsapp|telegram|discord|
  slack|ticketing|psa` (no tooling hits beyond the Telegram remote-control
  approval path); confirmed tool registration in `internal/tool/` +
  `internal/agent/`.
- No code was changed; this is a research/analysis reference doc.
