# Web Changes Tab Parity — Design

- **Date:** 2026-07-24
- **Status:** Approved (design) — pending implementation plan
- **Goal:** Bring the web/desktop UI to parity with the TUI's per-session
  **Changes** tab (`internal/tui/changes_model.go`) by exposing the
  already-built `internal/changes.Registry` over REST and rendering it as a
  new **Changes** tab in the web app: file list, unified diff, and
  whole-file / block undo.

## Context

Second item in the TUI→web parity priority order (see
`docs/web-desktop-parity-todo.md` and
`docs/superpowers/specs/2026-07-24-web-cron-parity-design.md`, item 1,
already implemented). The `internal/changes` package
(`docs/superpowers/specs/2026-07-22-changes-tab-design.md`) was built with
web exposure explicitly in mind — its own design doc states "Web SPA — Out
of scope v1 ... package API is shaped to allow `/api/changes` later without
breaking changes." That package is now fully implemented and TUI-wired
(`internal/changes/{changes,registry,undo,diff,bash,bash_registry}.go`),
but has zero HTTP surface. This spec adds that surface plus the web panel.

## 1. User-facing summary

A new **Changes** tab (current web tab bar per `TopTabs.tsx`: Chat / Files /
Git / Status / Logs / Cron / Assets — new tab inserted between Files and
Git, matching the TUI's position between its `files` and `git` tabs, giving
Chat / Files / Changes / Git / Status / Logs / Cron / Assets) lists every
file the current chat session has added or edited, aggregated across the
main agent and any sub-agents:

- **Left pane** — file rows: status icon (`+` added / `M` modified / `-`
  deleted), path, author summary (e.g. "main · 3", "build · 1"), a `(bash)`
  badge on entries that came from bash pre/post-stat detection (these are
  not undoable — no snapshot backup exists for them).
- **Right pane** — unified diff of the selected file: pre-session bytes vs.
  current bytes on disk. Reuses the same line-coloring renderer
  `GitPanel.tsx` already has for git diffs.
- **Undo file** — restores the selected file to its pre-session state.
  Confirm dialog first (matches the TUI's confirm-before-undo).
- **Undo last change** — undoes only the most recent tool call on the
  selected file. Confirm dialog first.
- Rows with `Undoable: false` (bash-only entries) show disabled undo
  buttons with a tooltip explaining why, instead of a no-op click.
- Empty state ("no changes in this session yet") when the registry is
  empty — this is also what renders when the session has no active agent
  (e.g. a resumed session before the agent finishes rebuilding its
  registry from disk), since a resumed agent's registry naturally starts
  from `List()` returning `[]` until re-hydration completes. No special
  "agent inactive" messaging — same empty state either way, matching TUI
  cold-start behavior.

## 2. Decisions (confirmed with user)

| Topic | Decision |
|-------|----------|
| Undo confirmation | Confirm dialog before both undo-file and undo-block, same as TUI — no optimistic/toast-based undo, no extra type-to-confirm friction |
| No-active-agent state | Same empty state as TUI cold start (`Registry.List()` empty) — no separate "resume session" messaging |
| Session scoping | Reuse `Handler.activeAgentForRuns(sessionID) *agent.Agent` (already used by `/api/agents/runs`) rather than inventing a new agent-lookup path |
| Diff rendering | Reuse `GitPanel.tsx`'s existing unified-diff line renderer (patch string split on `\n`, `+`/`-` coloring) rather than pulling in a diff library |
| Tab position | Between Files and Git, matching the TUI's tab order |

## 3. Backend changes

New file `internal/server/handler_changes.go`, following the existing
`Handler` method pattern (see `handler_runs.go`):

- `GET /api/changes?session={id}` → `[]FileChange` JSON. Resolves via
  `h.activeAgentForRuns(sessionID)`; nil agent or nil `Changes()` →
  `[]byte("[]")`, same "legitimate empty state" contract `runsSnapshot`
  already uses for runs.
- `GET /api/changes/diff?session={id}&path={path}` → `{path, patch}`.
  Looks up the `FileChange` for `path` in the current `List()`, calls
  `changes.RenderDiff(fc.FirstBackupPath, path)`. 404 if `path` isn't in
  the current list (stale row on the client).
- `POST /api/changes/undo-file?session={id}` body `{"path": string}` →
  `registry.UndoFile(path)`. Both `UndoFile` and `UndoBlock` return errors
  wrapped via `errors.Join` (e.g. `errors.Join(ErrNoChanges, storeErr)`), so
  handlers MUST check with `errors.Is(err, changes.ErrNotUndoable)` /
  `errors.Is(err, changes.ErrNoChanges)`, never `==`. Status mapping:
  `200 {}` on `nil`; `409 {"error": "not_undoable"}` on
  `errors.Is(err, changes.ErrNotUndoable)` (bash-only entry, or a file
  whose only recorded state is an empty backup); `404 {"error":
  "no_changes"}` on `errors.Is(err, changes.ErrNoChanges)` (unknown path,
  or the underlying `snapshot.Store.UndoByToolCallID` refused — conflict,
  expiry, or not-found); `404` (no body-specific error) when
  `activeAgentForRuns` returns nil; `400` for any other error.
- `POST /api/changes/undo-block?session={id}` body `{"path": string}` →
  calls `LatestToolCall(path)` then, on success, `UndoBlock(path, tcid)`.
  `LatestToolCall` itself can return `ErrNoChanges` (no snapshot for path)
  or `ErrNotUndoable` (bash-only) — map those the same way before even
  attempting `UndoBlock`. Same status code contract as undo-file for the
  `UndoBlock` call itself.

All four handlers registered in `server.go`'s route table (`s.mux.HandleFunc`
calls in the same function that registers `/api/agents/runs`, around
`server.go:106`), next to that existing registration. No changes to
`internal/changes` itself — its API was already shaped for this.

**Distinct from the existing global undo.** `POST /api/files/undo` and
`POST /api/files/redo` already exist (`handler_files.go`) and call the
package-level `snapshot.Undo()`/`snapshot.Redo()` — the same global,
last-write-wins mechanism as the TUI's global `u`/`r` keys. The new
`/api/changes/undo-file` and `/api/changes/undo-block` routes are a
separate, per-file-scoped mechanism through `Registry.UndoFile`/
`UndoBlock`, mirroring the TUI changes tab's own `u`/`U` keys (which
already shadow the global `u`/`r` bindings per
`2026-07-22-changes-tab-design.md` §5.3). Do not merge these into the
existing `/api/files/undo` endpoint.

## 4. Web UI components

New directory `web/src/components/Changes/`:

- **`ChangesPanel.tsx`** — tab root. Owns file list state, polls
  `GET /api/changes` on the same interval pattern as `CronPanel`/
  `LogPanel`, refetches immediately after any undo mutation. Two-pane
  layout matching `GitPanel.tsx`'s structure (file list left, diff right).
- **`ChangesFileList.tsx`** — row rendering: status icon, path, author
  summary, `(bash)` badge, disabled-with-tooltip undo buttons for
  non-undoable rows. Expandable per-row detail strip (mirrors the TUI's
  `?` toggle) showing `lastBashCommand`/`lastBashExitCode` when present.
- **`ChangesDiffView.tsx`** — fetches `GET /api/changes/diff` for the
  selected row, renders the unified diff (port of `GitPanel.tsx`'s
  `patch.split("\n")` line-coloring block into a shared/standalone
  component).
- **`UndoConfirmDialog.tsx`** (or inline Radix `AlertDialog` usage within
  `ChangesFileList.tsx` if small enough) — confirm-before-undo, wording
  matches the TUI's `undo foo.go to pre-session state? [y/N]`.

`web/src/api/client.ts` additions: `listChanges(session)`,
`getChangeDiff(session, path)`, `undoChangeFile(session, path)`,
`undoChangeBlock(session, path)`.

`web/src/api/types.ts` additions (camelCase mirror of the Go structs in
`internal/changes/changes.go`):

```ts
type FileStatus = "added" | "modified" | "deleted";

interface ChangeAuthor {
  agentId: string;
  agentName: string;
  changes: number;
}

interface FileChange {
  originalPath: string;
  status: FileStatus;
  firstBackupPath: string;
  undoable: boolean;
  undoAllTcId: string;
  changeCount: number;
  authors: ChangeAuthor[];
  createdAt: string;
  updatedAt: string;
  lastBashCommand: string;
  lastBashExitCode: number;
}
```

`TopTabs.tsx` gets a new `changes` tab entry positioned between `files` and
`git`. `App.tsx` and `SessionPage.tsx` each get
`{activeTab === "changes" && <ChangesPanel />}` inserted at the same
position.

## 5. Data flow

`ChangesPanel` is the sole owner of file-list state — no global store
entry, same rationale as the Cron panel (changes data isn't referenced
elsewhere in the app). Diff content is fetched lazily per-selected-row, not
prefetched for the whole list (matches the TUI's lazy-load-on-select
pattern for the preview pane). Undo mutations trigger an immediate
`listChanges` refetch rather than local optimistic state mutation, since a
successful undo can shift other rows' `ChangeCount`/`Authors` if the undone
tool call wasn't actually the file's only change.

## 6. Testing

- **Backend:** Go unit tests for the four new handlers in
  `internal/server/handler_changes_test.go` — happy path for each,
  `ErrNotUndoable` → 409, `ErrNoChanges` → 404, nil-agent → empty list /
  404, mirroring the existing `handler_runs_test.go` / cron handler test
  patterns.
- **Frontend:** No test framework exists in `web/` (documented constraint,
  same as the Cron parity plan) — verified via `tsc --noEmit` and
  `vite build` only, no fabricated component tests.
- **Manual smoke:** run a session in the web UI, edit a file via chat,
  open the Changes tab, confirm the row appears, undo it, confirm the file
  reverts on disk.

## 7. Out of scope

- Renames/moves detection (already out of scope in the TUI spec).
- Filesystem watcher / non-bash change detection beyond what
  `internal/changes` already does.
- Cross-session diffing.
- Per-hunk (sub-file) undo UI — block-undo remains "undo the file's most
  recent tool call," same granularity as the TUI's `U` key.
- SSE/push updates for the change list (poll-based, same rationale as the
  Cron panel — changes don't need sub-second freshness).
