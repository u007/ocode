---
type: Decision
title: Multi-Use Preview — Design Spec (Draft, 2026-09-10)
description: 'Historical Draft (2026-09-10) design for sidebar PreviewHost + a full session Preview sub-tab. Updated 2026-09-17 with as-built note in §2: Files-tab .md behavior now Edit/Preview/Split mode switch in FileTabContent (superseding the spec''s "Monaco by default" statement for that path); sidebar PreviewHost path unchanged. Known divergences §9 re-listed.'
resource: docs/superpowers/specs/2026-09-10-preview-multipurpose-design.md
tags:
  - preview
  - design
  - draft
  - spec
  - sidebar
  - files-tab
  - monaco
  - pdf
  - docx
  - pptx
  - markdown
  - split-mode
timestamp: 2026-09-17T07:53:56Z
---
# Multi-Use Preview — Design Spec (Draft, 2026-09-10)

**Type:** Decision  
**Description:** Historical Draft (2026-09-10) design for sidebar PreviewHost + a full session Preview sub-tab. Predates and does NOT cover the 2026-09-16 Files-tab auto-preview routing; diverges from shipped code on `.md` default (rendered MarkdownViewer, not Monaco+toggle) and on the PreviewSurface signature. See docs/gotchas/files-tab-preview-only-routing.md for shipped behavior.  
**Resource:** docs/superpowers/specs/2026-09-10-preview-multipurpose-design.md  
**Tags:** preview, design, draft, spec, sidebar, files-tab, monaco, pdf, docx, pptx  

---

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

<!-- as-built (2026-09-17): The above statement is now superseded for the **Files-tab** path.
     `FileTabContent` (the sole Files-tab routing point) now renders an Edit / Preview / Split mode
     switch for `.md`/`.markdown` files, with Edit (Monaco) as the default. The preview pane is
     resizable (`useResizableSplit`) and fed live (unsaved) content via `PreviewSurface`'s
     `content?: string` prop → `MarkdownViewer` controlled mode. The sidebar `PreviewHost` path is
     unchanged (still read-from-disk, no mode switch). See
     `docs/gotchas/files-tab-preview-only-routing.md` §Markdown mode switch for the as-built detail. -->

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

- Load failure → surface error in preview pane (not a toast — user is looking at it).
- File too large for raw endpoint → show size warning with link to external app.
- Permission denied → show lock icon + retry prompt (session may have expired).
- Concurrent edit (dirty state) → discard warning before switching files (same contract as `PreviewHost`).

## 8. Out of Scope

- Editing inside the preview (markdown WYSIWYG, PDF annotation). The preview is read-only; edits happen in Monaco.
- Collaborative preview (multiple cursors in the preview pane).
- The Files-tab editor tab body routing by file kind (implemented separately in 2026-09-16; see `docs/gotchas/files-tab-preview-only-routing.md`).

## 9. Divergences from Shipped Code

This spec is a **historical draft** and does not describe the current Files-tab behavior. Known divergences:

1. **Files-tab auto-preview is not in the spec.** The spec only covers sidebar `PreviewHost` and the session `Preview` sub-tab; it never routes Files-tab editor tabs by kind.
2. **`.md` default differs.** The spec says Monaco by default with `MarkdownViewer` behind a toggle; the shipped `PreviewSurface` renders `MarkdownViewer` when `kind === "markdown"` (rendered markdown is the default for the sidebar path).
3. **`PreviewSurface` signature differs.** The spec proposes `(path, kind, projectRoot, onSave)` (§4.1); the shipped component takes `(path, kind, projectRoot, projectHost, page, onPageChange, slide, onSlideChange, onOpenFile)` and has no `onSave`.
4. **Media streaming is newer than the spec.** The spec has no capability-token or range-streaming concept.
