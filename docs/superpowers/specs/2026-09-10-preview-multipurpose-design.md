# Multi-Use Preview — Design Spec

> Date: 2026-09-10  
> Status: Draft (design approved by user; advisor checkpoint pending final sign-off before implementation)  
> Scope: Sidebar `PreviewHost` preservation + new full session-level Preview tab  
> Related docs: `CHANGES.md` (2026-09-03/04 sidebar preview), `PLAN-live-preview.md` (different feature — dev-server manager)

## 1. Goal

Make preview multi-purpose:
- **Sidebar** (`PreviewHost`, existing side rail): stays untouched — agent (`preview_open` tool) and manual activation.
- **Full session-level sub-tab** (`Preview` sub-tab, added through existing `projectStore` / `SessionSubTabs` state rather than an isolated component): full-width preview/edit for any file the sidebar supports (`.pdf`, `.docx`, `.pptx`, `.xlsx`, `.mmd`/markdown, images, text/editable).

Both surfaces use shared rendering logic; activation is agent-driven (`PREVIEW_OPEN:` sentinel) or manual.

## 2. Architecture (confirmed)

- Extract shared `PreviewSurface` / renderer dispatch from `PreviewHost` (do NOT reuse the full `PreviewHost` component directly for full tab — side-rail layout, collapse, and browser-session lifecycle must stay isolated).
- `PreviewHost` (sidebar) stays as-is, consuming the shared renderer.
- `PreviewTabPage` (new full-width session sub-tab): left `FilePicker`, right preview/edit surface; state lives in `projectStore`.
  - Sub-tab ID: `"preview"` (added to `SessionSubTabId` restoration in `projectStore`).
  - Selected file state: `(path, kind, projectRoot)` stored in `PreviewTabPage`'s own state (not global store) so it survives reloads via `loadViewStateForProject` / `saveViewStateForProject` only if explicitly persisted; default is session-scoped (reset on session/project switch).
  - Page/slide index: stored locally in `PreviewTabPage` (not persisted) for `.pptx`/`.pdf`.
  - Dirty state: local to `PreviewTabPage`; new file selection replaces and clears dirty/edit state.
  - Promotion from sidebar → full tab: dispatch `ocode:preview-promote` event carrying `(path, kind, projectRoot)`; `App.tsx` listens, opens `PreviewTabPage` sub-tab with that file.
- `.md` behavior: **Monaco by default for both sidebar and full tab** (`TextViewer` with saved edits); `MarkdownViewer` remains available via a rendered-preview toggle in `PreviewSurface`. This aligns `PreviewHost` (sidebar) with the shared renderer: sidebar keeps existing `.md` behavior but gains the same Monaco+toggle contract.

## 3. Data Flow (confirmed)

```
File selected → GET /api/files/content (text/editable) OR
               GET /api/files/raw (binary, allowlisted, 32 MiB cap,
                                    project-root anchored, auth scoped)
               → PreviewSurface renders (Monaco / pdf.js / docx-preview /
                 jszip slide parser / mermaid / SheetJS / image)
Edit save → PUT /api/files/content (atomic write)
Agent trigger → preview_open tool → PREVIEW_OPEN: sentinel →
                usePreviewActivation → open sidebar PreviewHost →
                user can promote to full Preview tab
```

## 4. Components

### 4.1 Shared (extracted from `PreviewHost`)
- `PreviewSurface` — takes `(path, kind, projectRoot, onSave)`; dispatches to correct viewer/editor.
- Shared file-load/save hook (`loadFile`, `saveFile`).
- Shared preview state types (`path`, `kind`, `editable`, `dirty`, `loadState`, `error`).

### 4.2 Sidebar (existing, untouched structure)
- `PreviewHost.tsx` — two surfaces (`browser`, `preview`), uses `PreviewSurface`.

### 4.3 Full Session Tab (new)
- `PreviewTabPage` — full-width layout:
  - Left: `FilePicker` (not `FileTree` mini); selection contract: returns `(path, kind, projectRoot)` anchored to active project.
  - Right: `PreviewSurface` with full-width viewer/editor.
  - Empty state: file picker + message.
  - Dirty-state indicator (unsaved Monaco edits) survives file switch? **No** — new file selection replaces and clears (matches existing `PreviewHost` behavior: new selection replaces current playback/edit).

## 5. Security / Integrity (audit existing APIs first; extend only what's missing)

Before any backend change, compare current behavior in `internal/server/handler_files.go` (`GET /api/files/raw`) and `internal/server/handler_open.go` (`POST /api/files/open`) against the requirements below. Only implement protections that are confirmed missing:

- `GET /api/files/raw` concrete invariants:
  - Canonical path (`filepath.Clean` + `EvalSymlinks`) must resolve inside `project_root`; absolute paths without `project_root` rejected.
  - Explicit extension allowlist (`.pdf`, `.docx`, `.pptx`, `.xlsx`, `.xls`, `.csv`, `.png`, `.jpg`, `.jpeg`, `.gif`, `.webp`, `.svg`, `.mmd`, `.md`, `.txt`, `.html`).
  - Size enforced by bounded streaming read (`io.LimitReader` to 32 MiB), not pre-read `Stat`.
  - Auth/session/project scoping preserved.
  - `.html` served with `Content-Type: text/html` but without inline execution privileges.
- `POST /api/files/open` concrete behavior:
  - Headless/server deployments: return `{"open": false, "reason": "headless/no-app"}`.
  - Always pass validated canonical path (not a shell command string) to OS open API.
- `PUT /api/files/content`:
  - Atomic write: write to temp file, then rename (`os.Rename`).
  - Conflict detection: compare content or `ModTime` before write; return 409 if changed since load.
- Do NOT add duplicate APIs or alternate endpoints.
- Monaco (`TextViewer`):
  - Unique model URI per file path.
  - Proper disposal on component unmount / file switch.
  - Large-file limits; Ctrl/Cmd-S save; dirty indicator; concurrent external change detection.

## 6. Activation Semantics ("both" — agent + manual)

- `preview_open` agent tool:
  - Produces `PREVIEW_OPEN:` sentinel in tool result.
  - `App.tsx` scans chat stream (existing `usePreviewActivation`); opens sidebar `PreviewHost` at requested path.
  - If user wants full tab: sidebar provides "Open in full preview" action that promotes to `PreviewTabPage` (same path, same kind).
- Manual:
  - File tree "Preview in sidebar" → dispatches `ocode:open-preview` event (same as today).
  - Full session `Preview` tab → user picks file via file picker; `PreviewSurface` renders.

## 7. Error Handling

- Corrupt binary (`.pptx`, `.pdf`) → `PreviewSurface` shows error panel with "Open with OS app" fallback (`POST /api/files/open`).
- Empty full Preview tab → file picker + message: "Select a file to preview or edit."
- Malformed/unauthorized `PREVIEW_OPEN:` path → error surface (not silent navigation).
- `.md` ambiguity handled: default Monaco; if user wants rendered markdown, use toggle/button in `PreviewSurface` header.

## 8. Testing (before implementation)

- Agent activation: `preview_open` → sidebar opens → full tab promotion works.
- Manual activation: empty state, file selection, viewer render for each kind (`pdf`, `docx`, `pptx`, `xlsx`, `image`, `mmd`, `markdown`, `text`).
- `.md`: Monaco edit, save (`PUT`), dirty indicator, rendered toggle.
- `GET /api/files/raw`: traversal/symlink denial, size limit, allowlist enforcement, auth scoping.
- `POST /api/files/open`: privileged path passing (no shell), headless behavior.
- Monaco: model URI isolation, disposal, large-file limit, save conflict.
- Corrupt binary: error panel + OS-open fallback.
- Existing `PreviewHost` tests (`PreviewHost.test.tsx`) must still pass (no regression).

## 9. Open Questions (resolved)

- `.md` behavior: **Monaco default + toggle** (design section 3, advisor point 3).
- Full tab layout: file picker left, surface right (`PreviewTabPage`).
- Empty tab: file picker + message.
- Reuse: extract `PreviewSurface`; do not reuse `PreviewHost` shell for full tab.

---

*Self-review checklist (before user review):*
- [x] No placeholders (all sections have concrete behavior)
- [x] No contradictions (sidebar + full tab are independent surfaces sharing renderer)
- [x] Scope defined (sidebar preservation + new full tab + shared renderer + security hardening)
- [x] References existing code (`PreviewHost.tsx`, `previewKind.ts`, `handler_files.go`, `handler_open.go`, `App.tsx`)
- [x] Advisor feedback incorporated (shared renderer extraction, `.md` resolution, `/api/files/raw` hardening, `POST /api/files/open` privilege, Monaco details, activation semantics)

## 9. Implementation Order (incremental — protect existing working tree)

Based on advisor checkpoint: implement in this order:

1. **Tab/store plumbing** — add `preview` sub-tab through `projectStore` / `SessionSubTabs`; empty state (`PreviewTabPage` shell).
2. **Shared `PreviewSurface` extraction** — extract renderer/load/save primitives from `PreviewHost`; preserve `PreviewHost` behavior; run `PreviewHost.test.tsx` regression.
3. **Full tab rendering** — wire `PreviewTabPage` with `FilePicker` + `PreviewSurface`; reuse existing viewers.
4. **Activation paths** — agent (`preview_open`) and manual (`FileTree` event) to sidebar + promote to full tab.
5. **Edit/save/conflict** — Monaco (`TextViewer`) with `PUT /api/files/content`; reuse `FileEditor` conflict behavior.
6. **Security hardening** — audit `handler_files.go` / `handler_open.go`; add only confirmed missing protections; no duplicate APIs.
7. **Tests** — agent/manipulation, empty state, file-picker selection contract (`FilePicker` returns path/kind/projectRoot), promotion (`ocode:preview-promote`) and sub-tab persistence (`projectStore` / `SessionSubTabs` restore), `.md` Monaco + toggle, traversal denial, size limit, corrupt binary, OS-open fallback, Monaco URI isolation, existing regression.

> Note: No build/test/lint validation performed at design stage; validation will occur after `writing-plans` plan creation and during incremental implementation.

---

*Self-review checklist (updated after advisor):*
- [x] No placeholders; no contradictions; scope defined
- [x] References existing code (`PreviewHost.tsx`, `previewKind.ts`, `handler_files.go`, `handler_open.go`, `App.tsx`, `projectStore`, `SessionSubTabs`)
- [x] Advisor feedback incorporated (shared renderer extraction, `projectStore` tab state, audit-first security, no duplicate APIs, incremental order, Monaco details)
