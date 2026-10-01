---
type: concept
title: Scheduled Jobs / Cron Dispatch
description: Persistent, disk-backed cron engine + headless agent dispatcher for ocode, modeled on nanobot's CronService and Claude Code's CronCreate/CronList/CronDelete semantics. Also covers the reminders/tasks engine behind the Cron tab's Reminders and Tasks sub-views.
tags: [scheduler, cron, dispatch, agent, automation]
status: active
created: 2026-07-17
timestamp: 2026-09-30T14:23:10Z
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
  runtime mutation point in `internal/agent/permissions.go:2372`). Safe default
  is `normal`; `yolo`/`locked` are explicit opt-in.

### Dispatch semantics (mirroring Claude Code)
- Fires **between turns** (low-priority enqueue).
- **Jitter** (≤30s) on recurring schedules so many jobs don't hit the API on the
  same wall second.
- **Local timezone** for cron unless `tz` is set.
- **One-shot auto-delete** after firing.
- **7-day recurring expiry** (Claude Code's `/loop` rule).
- **Minimum `every` interval** — `every` schedules are clamped to ≥30 s (`minEveryMs` in `internal/scheduler/types.go`) so a misconfigured interval (e.g. seconds vs ms) cannot fire forever every few seconds.
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
- `GET  /api/cron/{id}/runs?limit&offset` — paginated run history for one job (`RunRecord` with `started_at`/`finished_at`/`duration_ms`/`status`/`input`/`output`/`error` + datetime-stamped `logs`); backed by `runs.jsonl` via `RunHistory` in `internal/scheduler/runs.go` and rendered in web by `CronHistoryPanel`

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

### LLM `cron` tool — scoped to the session's project
The REST surface was already per project; the LLM-facing `cron` tool was not.
`buildAgentSession` injected one process-wide `scheduler.Service`
(`h.scheduler`, keyed on the server's boot directory) into every agent's tool
set, so `cron add` from a chat in any project filed the job in the boot
project's store — the last unscoped path into the same data.

How it works:

- `Handler` has a `cronServices func(root string) (*scheduler.Service, error)`
  field (`internal/server/handler.go`), installed by `Server.SetScheduler`,
  which builds it from `Server.cronServiceResolver()`
  (`internal/server/cron_scope.go`).
- `buildAgentSession` normalises `projectRoot` (`"" -> h.workDir`) BEFORE any
  consumer keys off it. This ordering is load-bearing and was a real trap: the
  normalisation used to sit just above `SetWorkDir`, so the LSP manager and the
  cron tool — built in between — saw the RAW root. A session with no project
  would resolve its engine against `""`, a different store directory from the
  default project's.
- The tool registry receives a `tool.ProjectCronService{Root, Resolve}`
  (`internal/tool/cron.go`) rather than a service. It is still passed through
  the existing `svc any` indirection, which exists to avoid a tool ↔ scheduler
  import cycle.
- **The tool resolves on EVERY `Execute`, not once at build time.** This is
  deliberate: the per-project engines are stopped by an idle sweeper after 30
  minutes (next subsection), so a captured service pointer would keep
  addressing a stopped engine for the rest of the session's life.
  Re-resolving per call also keeps the project's registry entry warm.
- **A resolution failure is surfaced to the model and is never satisfied from
  another project.** Falling back to the boot project's engine would file a job
  where the user is not looking — the exact bug being fixed. The ONLY fallback
  is for a host that has no per-project scope at all (it hands the tool a bare
  `*scheduler.Service`), which keeps the previous single-service behaviour; a
  host with no scheduler still gets no `cron` tool in its tool set (the
  existing `if svc != nil` registration rule is unchanged).

Tests: `internal/server/cron_tool_scope_test.go` — project isolation of
tool-created jobs, the `"" -> h.workDir` fallback, the no-fallback-on-error
rule, per-call re-resolution after a project's engines were reclaimed,
omission when no scheduler is attached, and the `SetScheduler` wiring.

### Host-seeded default project entry is pinned against idle eviction
`SetScheduler` seeds the default project's registry entry with the HOST's own
engines rather than letting the scope start its own, because those engines
already carry the Telegram drainer sink and the RC-bridge fan-out.

The idle sweeper (`evictIdleCronProjects`, `cronProjectIdleTimeout` = 30 min,
both in `internal/server/cron_scope.go`) originally had no exemption for that
entry. It stopped the host's engines ~30 minutes after launch, and the next
resolve lazily started a replacement WITHOUT those sinks — the precise outcome
the seeding code's comment warns against. Since nothing re-seeds after boot,
Telegram and RC delivery for the boot project died permanently and silently.

The fix: `cronProjectEntry.pinned`. Two non-obvious points:

- **It is an `atomic.Bool`, not a plain bool**, because the writer
  (`setCronScopeConfig`) holds the ENTRY's mutex while the sweeper holds the
  REGISTRY's, and this file's lock order is `entry.mu -> scope.mu` — so the
  sweeper cannot take `entry.mu` to read the flag without inverting that order.
  (`lastUsedNs` is atomic for the same reason.)
- **Pinned exempts an entry from IDLE eviction ONLY.** `stopAllCronServices`
  still stops pinned entries, otherwise every server restart would leak a run
  loop and a drainer.

`scheduler.Service` gained a `Stopped()` accessor because `Stop()` otherwise
leaves no public trace, making a stopped engine indistinguishable from a live
one.

Tests: `internal/server/cron_scope_eviction_test.go` — the pinned default entry
survives idle eviction and is still the SAME engine (not a replacement), other
projects are still reclaimed, and shutdown still stops it.

## File map
| File | Purpose |
|------|---------|
| `internal/scheduler/types.go`         | Job / Schedule / Payload / Store types + constants |
| `internal/scheduler/scheduler.go`     | Engine: timer loop, persistence, dispatch, panic recovery |
| `internal/scheduler/dispatch.go`      | `Dispatcher` headless agent runner (OnJob glue) — also appends `RunRecord` to `runs.jsonl` via `RunHistory` |
| `internal/scheduler/host.go`          | `DefaultStorePath`, `StartForHost` host helpers |
| `internal/scheduler/runs.go`          | `RunRecord`/`RunLogEntry`/`RunHistory` — persistent per-run history (`runs.jsonl`, paginated `GET /api/cron/{id}/runs`, `CronHistoryPanel`) |
| `internal/scheduler/types.go`         | Job / Schedule / Payload / Store types + `minEveryMs` (30 s floor) |
| `internal/scheduler/scheduler.go`     | Engine: timer loop, persistence, dispatch, panic recovery |
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

## Reminders & Tasks engine (Cron tab → `Jobs | Reminders | Tasks`)

The web/desktop **Cron tab** has three sub-views — `Jobs | Reminders | Tasks`.
Jobs is the cron UI described above. Reminders and Tasks are served by a
second, purpose-built engine in `internal/reminders/` that deliberately
mirrors `internal/scheduler`'s persist pattern: its own JSON store
(`<GlobalDataDir>/scheduler/<project-slug>/reminders.json`, a sibling of
`jobs.json` — `DefaultStorePath` at `internal/reminders/types.go:166`), its
own run loop, and its own mutex.

### Why a separate store and engine, not a `kind` field on `scheduler.Job`
Because `executeJob` hardcodes *delete on `KindAt` fire*: when a one-shot `at`
job runs, the engine removes it from the store
(`internal/scheduler/scheduler.go:305-306`, `case KindAt:` →
`s.removeJobLocked(j.ID)`). A reminder is one-shot by definition, so modelling
it as an `at` job would erase the record on the very first fire — exactly the
record the user opened the Cron tab to see (fired time, status, outcome).
`scheduler.Payload` also has no home for `Status`/`Action`/`AutoComplete`.
Everything reusable — outbox, run history, targets, the agent runner and its
per-firing cleanup — is shared rather than forked (below).

### Model: `Kind`, `Status`, `Action`
`Item` (in `internal/reminders/types.go`):

- **`Kind`** — `reminder` (a one-shot nudge; a due time is REQUIRED, and
  `types.go:277-278` rejects `KindReminder` with `DueAtMs <= 0`) or `task`
  (checklist item; due date OPTIONAL — `DueAtMs == 0` means "no due date",
  never "due now").
- **`Status`** — `pending | in_progress | completed | cancelled`.
  `ValidStatus` (`types.go:179`) is the ONLY definition of the set: an
  unrecognised value is rejected, never coerced to a default.
- **`Action`** — `notify` (desktop/in-app notification, no LLM: the agent
  runner is invoked only when `Action == agent`, `internal/reminders/fire.go:85`)
  or `agent` (a real agent turn through the injected `AgentRunner` seam,
  `fire.go:26-28`).
- **`AutoComplete`** (task + `agent` only) — on validate it is normalised to
  `false` for every other shape (`types.go:278-280`), so a stored flag always
  means something. When set, a task whose agent turn returns **without error**
  settles on `completed` (`fire.go:125`); there is deliberately **no** parsing
  of the model's output for a magic completion marker — completion is derived
  from the flag plus the error result only (`autoCompleteTarget` comment at
  `fire.go:71-78`; applied through `CanTransition` in `commitFire`,
  `fire.go:176`).
- Remaining fields: `Title`, `Message`, `Notes`, `Owner`, `DueAtMs`,
  `PermMode`, `CreatedAtMs`, `UpdatedAtMs`, `FiredAtMs`, `Runs`,
  `LastStatus`, `LastError`.
- **There is no `deliver_to` on an `Item`** — no per-item delivery target
  exists; Telegram routing is by `Owner`/workdir only (see below).

### Status transition table (`internal/reminders/transition.go:40`)
The whole state machine lives in `allowedTransitions`
(`transition.go:40`); every legality question — the PATCH handler, the
auto-complete path, `NextStatuses` (`transition.go:98`) which drives the web
buttons, and the tests — resolves through it (`Transition`,
`transition.go:79`; errors wrap `ErrTransition`, `transition.go:11`).

```
pending ─────► in_progress ─────► completed
   ▲  │             │  │              │
   │  └─────────────┼──┘              │
   │                ▼                 │
   └──────────── cancelled ◄──────────┘
```

- `pending → in_progress | completed | cancelled`
- `in_progress → completed | cancelled | pending`
- `completed → pending` and `cancelled → pending` (only).
- `completed` and `cancelled` are terminal **with respect to each other**:
  there is no `completed → cancelled` nor `cancelled → completed` in one
  step — route through `pending`, keeping "reopen" a single auditable move.
- Every non-terminal state can return to `pending` — the deliberate unmark
  escape hatch, available from any state.
- A same-status PATCH is an idempotent no-op → 200 (the handler skips the
  check and `Service.SetStatus` returns early, `service.go:371-374`);
  re-sending `pending` re-arms instead (see below).

HTTP behaviour:

- **Illegal transition → 409.** The PATCH handler pre-validates the requested
  status against the CURRENT status and answers `http.StatusConflict`
  (`internal/server/reminders.go:170-175`); `statusForErr`
  (`reminders.go:343-351`) maps `ErrNotFound → 404`, `ErrTransition → 409`,
  everything else → 400.
- **Unknown status values → rejected, never coerced to a default:**
  - create (`POST`) with any status other than `pending` → **400**
    (`reminders.go:117-119`, "a new item is always created pending; set
    status with PATCH");
  - list filter `?status=archived` → **400** (`reminders.go:69-72`);
  - `PATCH` with an unknown status → **400** (`reminders.go:170-175`). The
    handler checks `ValidStatus` BEFORE consulting `Transition`, because both
    an unknown value and an illegal move come back from `Transition` wrapped in
    the same `ErrTransition` sentinel: an unknown value is a malformed request
    (400, a field error the form can show) while a known value the machine
    forbids is a state CONFLICT (409, meaning re-read and re-render). Collapsing
    them made an unknown status answer 409, which is what
    `TestUnknownStatusIsRejected` (`internal/server/reminders_test.go:165`)
    caught — the two are now checked separately and the whole
    `internal/server` package is green.
- **PATCH validates the transition BEFORE applying field edits**
  (`reminders.go:149-193`): the body's status is checked first, then field
  edits via `Service.Update`, then `SetStatus` — so a rejected status change
  cannot half-apply the rest of the PATCH (pinned by
  `TestRejectedStatusChangeLeavesFieldsUnapplied`, `reminders_test.go:179`).

### One-shot firing and the `FiredAtMs` re-arm rule
The firing gate is one predicate, `Item.Due(now)` (`types.go:225-226`):
active (pending/in_progress) AND `DueAtMs > 0 && <= now` AND **`FiredAtMs == 0`**.

- **Claim before run:** `fireOnce` stamps `FiredAtMs = now` while still
  holding the mutex (`fire.go:58`), so a concurrent tick cannot double-fire;
  a crash between claim and write-back leaves the item re-armed rather than
  lost — a duplicate notification beats a dropped one.
- **Re-arm = move back to `pending`:** `SetStatus(→ pending)` clears
  `FiredAtMs` along with `LastStatus`/`LastError`
  (`internal/reminders/service.go:378-382`). That is the only way a fired
  one-shot rings again. `commitFire` deliberately refuses to resurrect a
  claim the user cleared mid-flight (`fire.go:158-166`), and it only settles
  status when the live status still equals the one at claim time
  (`fire.go:176`) — a cancel during the agent turn is never overwritten.
- **Settle:** a fired **reminder** settles on `completed` (a nudge that rang
  is done; the record is the point). A fired **task** settles on `completed`
  only per the `AutoComplete` rule above; otherwise it stays put so an
  overdue task remains visible and the user decides.

### Shared outbox, run history, targets — and the id prefixes
Firing appends to the SAME files the cron engine uses, so a reminder appears
in the Cron tab's **Outbox** panel and gets the same **history** panel with
no new plumbing:

- `scheduler.Outbox` (`deliveries.jsonl`) — `appendDelivery`,
  `internal/reminders/fire.go:191-207`.
- `scheduler.RunHistory` (`runs.jsonl`) — `appendRunRecord`,
  `fire.go:215-240`; the `GET .../{id}/runs` route is literally wired to
  cron's `handleCronRuns` handler (`reminders.go:321`).
- Both records key their `JobID` with the prefix **`<kind>:<id>`**
  (`fire.go:198` and `fire.go:221`, e.g. `reminder:3f2a9c01`). That prefix
  is what lets the shared drainer tell a routable reminder delivery from a
  genuinely orphaned cron delivery.
- **Agent turns reuse the existing cron runner:**
  `reminderItemAsCronJob` (`internal/server/scheduler_reminders_runner.go:56`)
  projects an `Item` onto a `*scheduler.Job` with id **`rt-<id>`**
  (`:51`; session id `cron:rt-<id>`, `:26`) and `Schedule{Kind: KindAt}`,
  then calls `server.RunScheduledJob` (`:46`) — so a reminder turn inherits
  the cron runner's per-firing `lspMgr.Close()` / `ag.Shutdown()` cleanup.

**Reminders and tasks ARE Telegram-routable**, through the same per-project
`(workdir → chatID)` registry (`cron-targets.json`,
`internal/scheduler/targets.go`) as jobs. The `SetTelegramCronSink` drainer
sink previously DROPPED any delivery whose `JobID` was not in the cron store —
which would have silently lost every reminder push (visible locally in the
Outbox panel, lost remotely). It now recognises a `"reminder:"`/`"task:"` id
via `isReminderDeliveryID` (`internal/server/scheduler.go:513`; gate at
`scheduler.go:493`) and resolves it with a **synthetic Job** carrying the
delivery's `Owner` as `Payload.Owner` (`scheduler.go:497`) — the same
workdir hint `NewCronChatResolver` already reads. So a reminder is routed by
exactly the same rule as a job, with no resolver contract change. Note:
**`deliver_to` does NOT exist on an `Item`** — there is no per-item delivery
target; routing is by `Owner`/workdir only.

### REST surface (`internal/server/reminders.go`, registered by `attachReminders` at `:296`)
One handler is parameterised by kind, so the identical set is mounted under
both collections:

- `GET    /api/reminders` (and `/api/tasks`) — list, sorted due-time
  ascending with **UNDATED LAST**, then `created_at_ms`, then id
  (`internal/reminders/service.go:165-174`, sort call at `:191`), and
  paginated: `limit` (default **50**, max **200** — `DefaultPageSize` /
  `MaxPageSize`, `service.go:147,149`; the effective value is echoed back
  via `effectiveLimit`, `reminders.go:403`), `offset`, optional `status`
  filter. Response shape `{items,total,limit,offset}`
  (`reminderListResponse`, `reminders.go:53`). A malformed `limit`/`offset`
  is a **400**, never a silent `0` (`reminders.go:76-84`).
- `POST   /api/reminders` — create (title required; the kind comes from the
  route, not the body).
- `GET    /api/reminders/{id}` — read one.
- `PATCH  /api/reminders/{id}` — edit fields and/or change status (transition
  rules above).
- `DELETE /api/reminders/{id}` — delete.
- `POST   /api/reminders/{id}/run` — "run now", bypassing the due gate;
  terminal items are refused → **400** (`FireNow`, `service.go:558-578`),
  missing id → 404.
- `GET    /api/reminders/{id}/runs?limit&offset` — run history (same file and
  same handler cron uses, keyed by the prefixed id).

**Cross-kind ids are 404s, not edits:** a task id presented under
`/api/reminders` (or vice versa) is reported as 404 — `lookup` enforces the
kind (`reminders.go:249`) so one collection cannot be read or mutated
through the other's route.

### Host wiring
`Server.AttachReminders(workDir, cfg, notifier)`
(`internal/server/reminders_host.go:72`) starts the engine and mounts the
routes. When the cron service is attached it reuses that service's
`Outbox`/`RunHistory` instances, so the drainer fan-out (Telegram, TUI bridge,
web Outbox panel) applies to reminder deliveries too. A `busNotifier`
(`reminders_host.go:32`) publishes a project-level **`reminder_fired`** SSE
event (`ReminderFiredEvent`, `reminders_host.go:20`). Call sites:
`internal/desktop/scheduler.go:69` and `main.go`'s `schedulerSetup()`
(`main.go:116`).

### Frontend (web/desktop Cron tab)
- `web/src/components/Cron/CronSubTabs.tsx` — the `Jobs | Reminders | Tasks`
  switcher (`CronSubView`, `:10`).
- `web/src/components/Cron/ReminderTaskView.tsx` — ONE component serves both
  kinds; a `kind` prop selects the `/api/reminders` vs `/api/tasks`
  collection.
- `web/src/components/Cron/ReminderTaskDialog.tsx` — create/edit dialog.
- `web/src/components/Cron/reminderFormat.ts` — due-date/status formatting
  helpers.
- `web/src/components/Cron/CronHistoryPanel.tsx` — gains an optional
  `fetchRuns` prop (`:47`) so a reminder reuses the cron history panel
  against the shared `runs.jsonl`.
- `web/src/components/Cron/CronPanel.tsx` — holds `SUB_VIEW_FOR_KIND`, the
  singular→plural kind→collection map (`:232`). All three panes stay
  mounted but load **lazily** (a pane fetches the first time it is shown)
  and only the front pane polls.

### File map (reminders/tasks additions)
| File | Purpose |
|------|---------|
| `internal/reminders/types.go`        | `Item`, `Kind`/`Status`/`Action`, `DefaultStorePath` → `reminders.json`, firing gate `Due()`, validate + normalisation |
| `internal/reminders/transition.go`   | `allowedTransitions` table, `Transition`/`CanTransition`/`NextStatuses`, `ErrTransition` |
| `internal/reminders/service.go`      | Store + mutex + run loop: `Add`, `Update`, `SetStatus` (re-arm), `List` (sort/paging), `FireNow` |
| `internal/reminders/fire.go`         | Claim (`FiredAtMs`), notify/agent delivery, shared outbox + run-history appends, `commitFire` write-back |
| `internal/reminders/host.go`         | Host seams (`AgentRunner`, `Notifier`, `OutboxAppender`, `RunRecorder`) keeping the package free of agent imports |
| `internal/reminders/reminders_test.go` | State machine, one-shot/re-arm, auto-complete, cross-kind tests |
| `internal/server/reminders.go`       | One kind-parameterised REST handler set for `/api/reminders` + `/api/tasks` |
| `internal/server/reminders_host.go`  | `AttachReminders`, `busNotifier` → `reminder_fired` SSE |
| `internal/server/scheduler_reminders_runner.go` | `reminderItemAsCronJob` → `RunScheduledJob` (`rt-` id prefix) |
| `internal/server/scheduler.go` *(changed)* | Drainer `isReminderDeliveryID` + synthetic-Job resolution so reminder/task deliveries keep their Telegram target |
| `internal/server/reminders_test.go`  | HTTP tests: 409/400/404, pagination, sorting, cross-kind isolation, Telegram id gate |
| `web/src/components/Cron/CronSubTabs.tsx` | Sub-view switcher |
| `web/src/components/Cron/ReminderTaskView.tsx` | Shared reminders/tasks list view |
| `web/src/components/Cron/ReminderTaskDialog.tsx` | Create/edit dialog |
| `web/src/components/Cron/reminderFormat.ts` | Formatting helpers |
| `web/src/components/Cron/CronHistoryPanel.tsx` | History panel (+ optional `fetchRuns`) |
| `web/src/components/Cron/CronPanel.tsx` | Sub-view state, `SUB_VIEW_FOR_KIND`, lazy mount + front-pane polling |

## Design decisions (advisor-verified)
- **Tool ordering for prompt cache**: registering a `cron` tool later will change
  the tools array and bust the Anthropic prompt cache (CLAUDE.md: tools come
  first in the prefix). Keep `InitBuiltinTools` deterministic; treat the tool
  set as grow-only/sticky within a session.
- **No exported `Agent.SetPermissions`**: `SetMode` is workflow mode, not
  permission mode. Per-job permission mode is set on the `PermissionManager`
  after `NewAgent` (`internal/agent/permissions.go:2372`).
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