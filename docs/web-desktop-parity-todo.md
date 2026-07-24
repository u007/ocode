# Web/Desktop UI Parity — Pending Features

Audit date: 2026-07-24. Desktop (`cmd/ocode-desktop`, Wails) embeds the same
`web/` React app, so "web" and "desktop" are the same UI surface here —
gaps below apply to both.

Priority order agreed 2026-07-24. Each item becomes its own
brainstorm → design spec → implementation plan cycle (see
`docs/superpowers/specs/` and `docs/superpowers/plans/`).

## 1. Cron / scheduling — in progress

- **Status:** Spec'd and planned.
- Design: `docs/superpowers/specs/2026-07-24-web-cron-parity-design.md`
- Plan: `docs/superpowers/plans/2026-07-24-web-cron-parity.md`
- Backend REST surface already existed (`GET/POST /api/cron`,
  `GET/DELETE /api/cron/{id}`, outbox, targets); plan adds
  `PATCH /api/cron/{id}` plus a new Cron tab (`CronPanel`, `CronJobDialog`,
  `CronOutboxPanel`, `CronTargetsPanel`).

## 2. Per-session Changes tab — not started

- TUI has a dedicated **Changes** tab (`internal/tui/changes_model.go`,
  `internal/tui/changes.go`) listing every file the current chat session
  touched, with whole-file and block-level undo. Explicitly deferred to a
  later spec in `docs/superpowers/specs/2026-07-22-changes-tab-design.md`
  ("Web SPA — Out of scope v1").
- Web only has the git-based **Git** tab (`web/src/components/Git/GitPanel.tsx`),
  which shows repo-wide `git status`/`git diff`, not a per-session tracked
  list, and has no undo action.
- No backend API exists yet for the session-scoped changes registry
  (`internal/changes` package) — needs a new `/api/changes` route before
  a web panel is possible.

## 3. Memory management (`/mem`) — not started

- TUI: `internal/tui/memory.go` — inspect/update memory files at
  user/project/global scope, toggle memory context injection.
- No HTTP API exists for `internal/memory` at all.
- No web UI.

## 4. Learn / skill authoring (`/learn`) — not started

- TUI: `internal/tui/learn.go`, `internal/skill/learn.go` — list project-root
  skills, guide skill creation/update.
- No HTTP API exists for skill authoring (skill *listing* exists via
  `GET /api/skills`, but not creation/update).
- No web UI.

## 5. Goal orchestration (`/goal`) — not started

- TUI: `internal/tui/goal.go`, `internal/cli/goal.go` — runs the multi-agent
  orchestration pipeline on a coding goal, streams status.
- No HTTP API exists.
- No web UI.

## 6. Image generation (`/image`) — not started

- TUI: `internal/tui/imagegen_cmd.go`, `internal/tool/imagegen.go`,
  `internal/config/imagegen_config.go` — toggle/configure imagegen
  provider/model.
- `TUIStatus` (web type) already carries `image_gen_enabled/provider/model`
  fields, but no web component reads or displays them — dead fields today.
- No setter/getter API exists for imagegen config.
- No web UI.

## 7. Docs / knowledge base (`/docs`) — not started

- TUI: `internal/tui/docs_knowledge.go`, `internal/knowledge/doc.go`,
  `internal/agent/knowledge_lookup.go` — init/update/cleanup/status/re-audit
  for the documentation knowledge bundle (OKF).
- No HTTP API exists.
- No web UI.

## 8. GitHub integration (`/github`) — not started

- TUI: `internal/tui/github_tui.go`, `internal/tool/github.go`,
  `internal/github/*.go` — PR/issue fetch, workflow generation.
- No HTTP API exists.
- No web UI.

## 9. Kaizen skill digest — not started

- TUI: force-injects per-model "Kaizen" tuning skill directives into the
  base prompt, announces once in transcript (`internal/tui/model.go`:
  `KaizenDigestBlock`, `KaizenSkillsForModel`, `announceKaizenDigest`).
- No HTTP API / web equivalent. Lower priority than the above — this is an
  automatic prompt-injection behavior, not an interactive feature, so it's
  unclear a dedicated web UI is even the right shape; needs a scoping
  conversation before a spec, not just an implementation.

## 10. `/search` (find message) — not started, smallest gap

- TUI: in-chat find bar via `/search`.
- Web: `ChatSearchBar.tsx` and `Ctrl/Cmd+F` already implement in-chat find —
  but the `/search` **slash command** itself is listed in
  `SlashCommandMenu.tsx`'s command list and falls through to the LLM
  unimplemented (`handled: false` in `commands.ts`).
- Likely a small bug-fix task (wire `/search` to open/focus the existing
  `ChatSearchBar`) rather than a full spec.

## Not gaps (web has, TUI doesn't — no action needed)

- Assets/uploads panel (`web/src/components/Assets/AssetsPanel.tsx`)
- Monaco editor + settings panel
- Mobile responsive layout
