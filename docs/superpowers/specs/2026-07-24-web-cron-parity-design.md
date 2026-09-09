# Web Cron/Scheduling Parity — Design

- **Date:** 2026-07-24
- **Status:** Approved (design) — pending implementation plan
- **Goal:** Bring the web/desktop UI to parity with the TUI's `/cron` command
  by exposing scheduled-job management (list/add/edit/remove/enable-disable),
  delivery history, and Telegram routing targets through a new top-level
  **Cron** tab. This is the first of several TUI→web parity specs (see
  context below); it was picked first because the backend REST surface for
  core CRUD already exists — this is primarily a web-wiring job plus one
  small backend addition.

## Context

An audit of TUI (`internal/tui/*.go`) vs. web (`web/src/*`) vs. desktop
(`cmd/ocode-desktop`, which just embeds the same web app in a Wails webview)
found several TUI-only features with no web equivalent: memory management,
learn/skill authoring, goal orchestration, image generation, docs/knowledge
base, GitHub integration, kaizen skill digest, cron/scheduling, the new
per-session Changes tab, and `/search`. Priority order agreed with the user:

1. **Cron/scheduling** (this spec) — backend API already exists, cheapest win
2. Per-session Changes tab — newest TUI feature (see
   `2026-07-22-changes-tab-design.md`, which explicitly deferred web/SPA
   support to a later spec)
3. Memory, learn, goal, imagegen, docs/KB, github, kaizen — each needs new
   backend API surface built from scratch; sequenced later, one spec each
4. `/search` — trivial, likely a bug-fix ticket rather than a design doc

## 1. User-facing summary

A new **Cron** tab (alongside Chat/Files/Git/Status/Logs/Assets) lists every
scheduled job for the current project:

| Name | Schedule | Next Run | Last Run | Last Status | Runs | Enabled | |
|------|----------|----------|----------|-------------|------|---------|---|

- **Add job** opens a form: Name, Message (the prompt to run), Notes,
  Schedule kind (Once / Every / Cron expression) with kind-specific fields
  (datetime picker for Once, duration input for Every, cron expression +
  IANA timezone for Cron expression), Deliver-to, and Permission mode
  (normal / yolo / locked, with a warning on yolo).
- **Enabled toggle** per row flips the job on/off without deleting it.
- **Delete** removes a job (confirm dialog).
- **Row click / describe** shows the full job JSON (mirrors `/cron describe`).
- A collapsible **Outbox** section shows delivery history (job name,
  result/error, timestamp) with a "clear" action.
- A **Targets** section (Telegram workdir→chatID routing) is a simple
  key-value editor — low-traffic, kept minimal since it's specific to the
  Telegram bridge integration.

## 2. Decisions (confirmed with user)

| Topic | Decision |
|-------|----------|
| UI placement | New top-level tab (not a sidebar panel or modal) — matches Status/Logs treatment |
| Create form scope | Full form matching the entire `cronAddRequest` surface (schedule kind, deliver-to, perm mode), not a minimal v1 |
| Outbox / targets | In scope for this spec — full parity with backend surface, not deferred |
| List columns | Full table: Name, Schedule, Next Run, Last Run, Last Status, Runs, Enabled, Delete |
| Enable/edit gap | Backend currently has no update endpoint (only add/remove/get/list) — **add `PATCH /api/cron/{id}`** as part of this spec rather than dropping the toggle from v1 |

## 3. Backend changes

The REST surface already live (`internal/server/scheduler.go`):
`GET /api/cron`, `POST /api/cron`, `GET /api/cron/{id}`,
`DELETE /api/cron/{id}`, `GET /api/cron/outbox`, `GET/POST /api/cron/targets`.

New in this spec:

- `scheduler.Service.UpdateJob(id string, patch JobPatch) error` in
  `internal/scheduler/scheduler.go`. `JobPatch` is a partial struct (pointer
  fields) covering `Enabled`, `Name`, `Schedule`, `Payload` — same shape as
  `cronAddRequest` but every field optional. Recomputes `State.NextRunAtMs`
  when `Schedule` changes, same logic path `AddJob` already uses.
- `PATCH /api/cron/{id}` handler in `internal/server/scheduler.go`, added
  next to the existing `cronHandler` methods and registered in
  `SetScheduler`. Body is a JSON object with only the fields to change.
  Returns the updated job.

No changes to `internal/scheduler` persistence format, no changes to the
`cron` tool, no changes to outbox/targets endpoints (already sufficient).

## 4. Web UI components

New files under `web/src/components/Cron/`:

- **`CronPanel.tsx`** — the tab's root: job table, "Add job" button, mounts
  `CronOutboxPanel` and `CronTargetsPanel` as collapsible sub-sections.
  Polls `GET /api/cron` on an interval (jobs have no SSE push) and refetches
  after any mutation.
- **`CronJobDialog.tsx`** — create/edit form (shared for both flows). Schedule
  kind selector switches the visible fields; a `cronDescribe`-equivalent
  helper renders the human-readable schedule string for the table (ported
  from `internal/tui/command_cron.go`'s `cronDescribe`).
- **`CronOutboxPanel.tsx`** — fetches `GET /api/cron/outbox`, renders delivery
  rows, "clear" button calls the same endpoint with `drain=true`.
- **`CronTargetsPanel.tsx`** — key-value editor over
  `GET/POST /api/cron/targets` (workdir → chatID).

`TopTabs.tsx` gets a new `cron` tab entry. `web/src/api/client.ts` gets
corresponding methods: `listCronJobs`, `addCronJob`, `updateCronJob`,
`deleteCronJob`, `getCronJob`, `getCronOutbox`, `drainCronOutbox`,
`getCronTargets`, `setCronTarget`.

## 5. Data flow

`CronPanel` is the sole owner of job list state (no global store entry
needed — cron data isn't referenced elsewhere in the app, unlike
`chatStore`/`projectStore`). Polling interval matches the existing
`LogPanel` pattern where applicable; mutations trigger an immediate refetch
rather than waiting for the next poll tick.

## 6. Testing

- **Backend:** Go unit tests for `scheduler.Service.UpdateJob` (toggle
  enabled, edit schedule recomputes `NextRunAtMs`, edit payload, not-found
  error) in `internal/scheduler/scheduler_test.go`; handler test for
  `PATCH /api/cron/{id}` in `internal/server/scheduler_test.go` mirroring
  existing add/remove handler tests.
- **Frontend:** Component tests for `CronJobDialog` schedule-kind-specific
  required-field validation, and `CronPanel` table rendering from mock API
  responses. No new E2E infra — uses whatever the project already has for
  other panels (e.g. `GitPanel`/`LogPanel` tests, if any exist, as the
  pattern to follow).

## 7. Out of scope

- Any changes to how the scheduler fires jobs or the `cron` tool's LLM-facing
  contract.
- SSE/push updates for job state (poll-based is sufficient given jobs change
  infrequently).
- Mobile-specific layout beyond what the existing responsive tab
  infrastructure already provides.
