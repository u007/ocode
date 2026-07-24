---
type: concept
title: Scheduled Jobs / Cron Dispatch
description: Persistent, disk-backed cron engine + headless agent dispatcher for ocode, modeled on nanobot's CronService and Claude Code's CronCreate/CronList/CronDelete semantics.
tags: [scheduler, cron, dispatch, agent, automation]
status: active
created: 2026-07-17
---

# Scheduled Jobs / Cron Dispatch

## What it is
ocode's scheduled-job system. A **job** = a prompt that runs itself later, on
a clock, without you at the keyboard. Built on `internal/scheduler/`.

The engine is decoupled from the agent: it owns timing + persistence + recovery;
the actual "run an agent turn" is an injected `OnJobFunc` supplied by the host.

## Architecture
```
ADD ─► STORE (jobs.json on disk, per-project) ─► TIMER LOOP (≤60s poll legs)
                                                         │ due?
                                                         ▼
FIRE ─► build "[Scheduled job context]" prefix ─► agent.Step(prompt)
                                                         │
                                                         ├─ ok   → record state, reschedule
                                                         ├─ err  → record state, keep
                                                         ├─ at   → auto-delete
                                                         └─ every (>7d) → auto-delete
```

### Three schedule kinds
- **`at`** — one-shot at epoch ms, deletes after firing.
- **`every`** — fixed interval in ms.
- **`cron`** — 5-field expression + optional IANA `tz` (via `github.com/robfig/cron/v3`).

### Host model (advisor-verified)
The scheduler lives in a **long-lived host** (the `serve`/`web` subcommands
through `internal/server`, and `cmd/ocode-desktop` which boots the server in a
background goroutine). The TUI (`main.go` no-subcommand) is ephemeral and only
**authors/manages** jobs via the shared on-disk store.

Caveat (matches Claude Code "Desktop tasks need your machine on"): jobs don't
fire unless a long-lived host is running. Document this in user-facing docs.

### Dispatch glue (`internal/scheduler/dispatch.go`)
- `Dispatcher.OnJob(ctx, job)` is the host-supplied `OnJobFunc` for the
  scheduler.
- Builds the context prefix:
  ```
  [Scheduled job context]
  Job: <name>
  Purpose: <notes>
  Scheduled by: <owner>
  Created: <UTC>
  Schedule: <human schedule>
  ---
  <prompt>
  ```
- Constructs a per-job agent via `agent.NewAgent(client, tools, cfg, lspMgr)`
  and runs `ag.Step(msgs)` — the same entry the server SSE and TUI stream paths
  use (`internal/server/handler_sse.go:198-229`, `internal/tui/model.go:11611`).
- Uses a **persistent per-job session** `cron:<id>` so context accumulates
  across firings. Retention: `capMessages(result, 80)` keeps the seeded context
  + the 79 most recent turns so the transcript never grows unbounded
  (`session.Save` overwrites the full transcript and never prunes).
- Per-job permission mode is bound via `ag.Permissions().SetMode(...)` (the
  runtime mutation point in `internal/agent/permissions.go:2219`). Safe default
  is `normal`; `yolo`/`locked` are explicit opt-in.

### Dispatch semantics (mirroring Claude Code)
- Fires **between turns** (low-priority enqueue).
- **Jitter** (≤30s) on recurring schedules so many jobs don't hit the API on the
  same wall second.
- **Local timezone** for cron unless `tz` is set.
- **One-shot auto-delete** after firing.
- **7-day recurring expiry** (Claude Code's `/loop` rule).
- **Panic isolation** — `executeJob` recovers per-job panics.
- **External-edit reload** — mtime-based; TUI edits to the store are picked up
  by the host's loop on the next `syncFromDisk` tick (max ≤60s leg).

## Public surface

### REST (added to `internal/server/server.go` via `attachScheduler`)
- `GET    /api/cron`        — list jobs
- `GET    /api/cron/{id}`   — describe one
- `POST   /api/cron`        — add (`name`, `message`, `notes`, `owner`,
  `deliver_to`, `perm_mode`; `schedule: {kind, at_ms|every_ms|expr, tz}`)
- `PATCH  /api/cron/{id}`   — partial update (any of: `enabled`, `name`, `message`,
  `notes`, `owner`, `deliver_to`, `perm_mode`, `schedule`); fields not sent are
  left unchanged. Uses `JobPatch` internally.
- `DELETE /api/cron/{id}` — remove
- `GET  /api/cron/outbox?drain=true&limit=N` — read/clear the JSONL delivery
  log (one entry per executed job; `drain=true` truncates on read)

### Programmatic (`internal/scheduler/scheduler.go`)
```go
svc := scheduler.NewService(storePath)
svc.SetOnJob(dispatcher.OnJob)
svc.SetMaxJobs(50)
svc.SetDrainerSink(func(d scheduler.Delivery) {
    // forward to Telegram, RC, web push, etc.
})
svc.Start()
defer svc.Stop()
id, _ := svc.AddJob(scheduler.Job{
    Schedule: scheduler.Schedule{Kind: scheduler.KindEvery, EveryMs: 60_000},
    Payload:  scheduler.Payload{Message: "say hi", PermMode: scheduler.PermNormal},
})
svc.RemoveJob(id)

# Partial update via JobPatch
enabled := false
svc.UpdateJob(id, scheduler.JobPatch{Enabled: &enabled})
newSchedule := scheduler.Schedule{Kind: scheduler.KindCron, Expr: "0 9 * * 1-5", TZ: "America/New_York"}
svc.UpdateJob(id, scheduler.JobPatch{Schedule: &newSchedule, Name: strPtr("weekday morning check")})
```

### Host wiring helper (`internal/scheduler/host.go`)
- `scheduler.DefaultStorePath(workDir)` → `<GlobalDataDir>/scheduler/<slug>/jobs.json`
- `scheduler.StartForHost(cfg, workDir, runner)` → constructs, wires
  Dispatcher, starts the loop AND a Drainer (default sink: log-only).
  Hosts override the sink via `svc.SetDrainerSink(...)`.

`main.go`'s `serve` and `web` paths call this via a `setup` hook into
`server.Run` (`internal/server/server.go`); the desktop uses
`desktop.AttachScheduler` (its `serverSchedulerRunner` reuses
`server.RunScheduledJob`). The server adds `Server.SetTelegramCronSink(bot,
resolve)` for hosts that want to forward cron results to a Telegram chat
(see `internal/telegram/bot.go::PushCronResult`).

## File map
| File | Purpose |
|------|---------|
| `internal/scheduler/types.go`         | Job / Schedule / Payload / Store types + constants |
| `internal/scheduler/scheduler.go`     | Engine: timer loop, persistence, dispatch, panic recovery |
| `internal/scheduler/dispatch.go`      | `Dispatcher` headless agent runner (OnJob glue) |
| `internal/scheduler/host.go`          | `DefaultStorePath`, `StartForHost` host helpers |
| `internal/scheduler/scheduler.go`     | Engine: timer loop, persistence, dispatch, panic recovery |
| `internal/scheduler/dispatch.go`      | `Dispatcher` headless agent runner (OnJob glue) |
| `internal/scheduler/deliver.go`       | `Outbox` JSONL delivery log + Append/Peek/Drain |
| `internal/scheduler/drainer.go`       | `Drainer` goroutine: polls outbox, hands entries to a host sink |
| `internal/scheduler/targets.go`       | `Targets` registry: per-project `(workdir → chatID)` JSON, persistent |
| `internal/scheduler/host.go`          | `DefaultStorePath`, `StartForHost` host helpers |
| `internal/scheduler/scheduler_test.go`| Engine tests (next-run, persistence, expiry, panic, external reload) |
| `internal/scheduler/deliver_test.go`  | Outbox round-trip / peek / missing-file tests |
| `internal/scheduler/targets_test.go`  | Targets round-trip / zero-removes / All-copy tests |
| `internal/scheduler/drainer_test.go`  | Drainer sink + idempotent Stop tests |
| `internal/scheduler/dispatch_test.go` | Dispatcher writes outbox on success + error |
| `internal/server/scheduler.go`        | `Server.SetScheduler` + `/api/cron/*` REST + outbox + targets + Telegram sink (now also fans out to the TUI bridge) |
| `internal/server/scheduler_runner.go` | `schedulerRunner`: builds per-job agent, runs Step, persists |
| `internal/server/rc_bridge.go`        | `RCBridge.CronDeliveryCh` + `PushCronDelivery`; TUI pulls from this |
| `internal/server/scheduler_rc_test.go` | RCBridgePusher + fan-out tests |
| `internal/server/scheduler_runner_test.go` | Token-budget retention tests |
| `internal/server/scheduler_telegram_test.go` | Telegram sink wiring test |
| `internal/server/scheduler_resolver_test.go` | CronChatResolver + end-to-end Telegram test |
| `internal/server/scheduler_targets_http_test.go` | `/api/cron/targets` HTTP test |
| `internal/server/scheduler_update_http_test.go`  | `PATCH /api/cron/{id}` HTTP test |
| `internal/desktop/scheduler.go`        | `AttachScheduler` for the desktop shell |
| `internal/telegram/bot.go`             | `PushCronResult`; auto-registers cron target on `/session` |
| `internal/telegram/bot_cron_target_test.go` | Bot cron-target persistence test |
| `internal/tool/cron.go`               | LLM-facing `cron` tool (add/list/remove/describe) |
| `internal/tool/cron_test.go`          | Cron-tool unit tests |
| `internal/tui/command_cron.go`        | `/cron` slash command (list/describe/remove/add) |
| `internal/tui/model.go`                | `cronDeliveryMsg` + listener + `formatCronDelivery`; renders cron results in chat |
| `main.go`                             | `schedulerSetup` hook wiring into `serve`/`web` |
| `cmd/ocode-desktop/main.go`           | Calls `desktop.AttachScheduler` after `StartServer` |

## Delivery / result forwarding
- `Dispatcher.OnJob` always writes a `Delivery` record to the per-project
  `Outbox` (JSONL at `<store-dir>/deliveries.jsonl`), regardless of success
  or failure. The outbox is the durable receipt the user can consult.
- A `Drainer` goroutine polls the outbox (default 10s) and hands each entry
  to a host-supplied sink. Default sink: log-only. Hosts override via
  `svc.SetDrainerSink(...)` or the convenience `Server.SetTelegramCronSink`.
- Telegram forwarding: the bot satisfies the small `cronPusher` interface
  with `PushCronResult(chatID, jobID, name, owner, result, errStr)`. The
  host passes a `cronChatResolver` that maps the originating job to a
  chat id. Jobs that opt in by setting `deliver_to` are forwarded; others
  fall back to the log-only sink.
- Auto-registration: when the user selects an instance with `/session
  <id>`, the bot records `(workdir → chatID)` to the per-project
  `cron-targets.json` (see `internal/scheduler/targets.go`). The
  canonical host wiring is one line:
  `srv.AttachTelegramBot(workdir, bot)` — it constructs the Targets
  registry and wires a `NewCronChatResolver` that looks up the chat id
  for the job's owner (or the default workdir). Subsequent cron
  deliveries for that workdir go straight to the chat.
- **TUI as a sink**: when `/rc` is on, the TUI registers a
  `CronDeliveryCh` on the server's `RCBridge` and listens for deliveries.
  Each one is rendered as an assistant-style message in the chat (no
  agent turn, no permission prompts). The drainer fans out to the TUI
  **in addition to** the Telegram bot — both are independent sinks.
  See `internal/server/rc_bridge.go::CronDelivery` and
  `internal/tui/model.go::cronDeliveryMsg` + `formatCronDelivery`.
- The outbox is also exposed at `GET /api/cron/outbox?drain=true&limit=N`
  so the web UI and RC clients can fetch results without going through
  Telegram. The targets registry is exposed at
  `GET/POST /api/cron/targets` so operators can list/clear mappings.

## Design decisions (advisor-verified)
- **Tool ordering for prompt cache**: registering a `cron` tool later will change
  the tools array and bust the Anthropic prompt cache (AGENTS.md: tools come
  first in the prefix). Keep `InitBuiltinTools` deterministic; treat the tool
  set as grow-only/sticky within a session.
- **No exported `Agent.SetPermissions`**: `SetMode` is workflow mode, not
  permission mode. Per-job permission mode is set on the `PermissionManager`
  after `NewAgent` (`internal/agent/permissions.go:2219`).
- **Persistent session trade-off**: a recurring `cron:<id>` session
  accumulates context across firings (good for jobs that need history) but
  must be capped — `session.Save` never prunes. The current cap is 80 messages;
  a future improvement is a token-ceiling cap or `MaybeCompactAsync` integration.
- **Headless runner entry point**: `agent.Step(...)` is correct for scheduled
  turns. `streamStep` (TUI) is UI-only; do not call it. `NewAgent` automatically
  starts compaction/memory workers, so a long-lived per-job agent gets those
  for free.

## See also
- `docs/knowledge-bundle.md` — context-agent-only OKF writer; this concept doc
  is read by the agent directly, not written by the bundle.
- Plan: `.opencode/plans/2026-07-17.md`.
- `NANOBOT.md` (repo root) — comparison with nanobot that motivated this work.
