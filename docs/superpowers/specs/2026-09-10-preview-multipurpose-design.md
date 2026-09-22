---
type: Decision
title: Multi-Use Preview — Design Spec (Draft, 2026-09-10)
description: 'Decision: Historical draft for sidebar PreviewHost + session Preview sub-tab, updated 2026-09-21 for .mdx support in extension lists.'
resource: ""
tags: []
timestamp: 2026-09-21T16:07:53Z
---
# Multi-Use Preview — Design Spec (Draft, 2026-09-10)

**Type:** Decision  
**Description:** Historical Draft (2026-09-10) design for sidebar PreviewHost + a full session Preview sub-tab. Predates and does NOT cover the 2026-09-16 Files-tab auto-preview routing; diverges from shipped code on `.md` default (rendered MarkdownViewer, not Monaco+toggle) and on the PreviewSurface signature. See docs/gotchas/files-tab-preview-only-routing.md for shipped behavior. As of 2026-09-21, `.mdx` is supported alongside `.md`/`.markdown` in the markdown kind (previewable, editor default Edit with mode switch).  
**Resource:** docs/superpowers/specs/2026-09-10-preview-multipurpose-design.md  
**Tags:** preview, design, draft, spec, sidebar, files-tab, monaco, pdf, docx, pptx, markdown, mdx, split-mode  

---

# Multi-Use Preview — Design Spec

> Date: 2026-09-10  
> Status: Draft (design approved by user; advisor checkpoint pending final sign-off before implementation)  
> Scope: Sidebar `PreviewHost` preservation + new full session-level Preview tab  
> Related docs: `CHANGES.md` (2026-09-03/04 sidebar preview), `PLAN-live-preview.md` (different feature — dev-server manager)

## 1. Goal

Make preview multi-purpose:
- **Sidebar** (`PreviewHost`, existing side rail): stays untouched — agent (`preview_open` tool) and manual activation.
- **Full session-level sub-tab** (`Preview` sub-tab, added through existing `projectStore` / `SessionSubTabs` state rather than an isolated component): full-width preview/edit for any file the sidebar supports (`.pdf`, `.docx`, `.pptx`, `.xlsx`, `.mmd`/markdown/MDX, images, text/editable).

Both surfaces use shared rendering logic; activation is agent-driven (`PREVIEW_OPEN:` sentinel) or manual.

<!-- as-built (2026-09-17): The above statement is now superseded for the **Files-tab** path.
     `FileTabContent` (the sole Files-tab routing point) now renders an Edit / Preview / Split mode
     switch for `.md`/`.markdown`/`.mdx` files, with Edit (Monaco) as the default. The preview pane is
     resizable (`useResizableSplit`) and fed live (unsaved) content via `PreviewSurface`'s
     `content?: string` prop → `MarkdownViewer` controlled mode. The sidebar `PreviewHost` path is
     unchanged (still read-from-disk, no mode switch). See
     `docs/gotchas/files-tab-preview-only-routing.md` §Markdown mode switch for the as-built detail. -->

## 2. Architecture (confirmed)

- Extract shared `PreviewSurface` / renderer dispatch from `PreviewHost` (do NOT reuse the full `PreviewHost` component directly for full tab — side-rail layout, collapse, and browser-session lifecycle must stay isolated).
- `PreviewHost` (sidebar) stays as-is, consuming the shared renderer.
- `PreviewTabPage` (new full-width session sub-tab): left `FilePicker`, right preview/edit surface; state lives in `projectStore`.
  - Sub-tab ID: `"preview"` (added to `SessionSubTabId` restoration in `projectStore`).
  - Selected file state: `(path, kind, projectRoot)` stored in `PreviewTabPage`'s own state (not global store) so it survives reloads via `loadViewStateForProject` / `saveViewStateForProject` only if explicitly persisted; default is session-scoped (reset on session/project switch).
  - Page/slide index: stored locally in `PreviewTabPage` (not persisted) for `.pptx`/`.pdf`.
  - Dirty state: local to `PreviewTabPage`; new file selection replaces and clears dirty/edit state.
  - Promotion from sidebar → full tab: dispatch `ocode:preview-promote` event carrying `(path, kind, projectRoot)`; `App.tsx` listens, opens `PreviewTabPage` sub-tab with that file.
- `.md`/`.mdx` behavior: **Monaco by default for both sidebar and full tab** (`TextViewer` with saved edits); `MarkdownViewer` remains available via a rendered-preview toggle in `PreviewSurface`. This aligns `PreviewHost` (sidebar) with the shared renderer: sidebar keeps existing `.md` behavior but gains the same Monaco+toggle contract. `.mdx` is rendered as Markdown in preview (never evaluated); editor uses Monaco `mdx` grammar. See `docs/gotchas/mdx-preview-not-evaluated.md`.

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
  - Explicit extension allowlist (`.pdf`, `.docx`, `.pptx`, `.xlsx`, `.xls`, `.csv`, `.png`, `.jpg`, `.jpeg`, `.gif`, `.webp`, `.svg`, `.mmd`, `.md`, `.markdown`, `.mdx`, `.txt`, `.html`).
  - Size enforced by bounded streaming read (`io.LimitReader` to 32 MiB), not pre-read `Stat`.
  - Auth/session/project scoping preserved.
  - `.html` served with `Content-Type: text/html` but without inline execution privileges.
- `POST /api/files/open` concrete behavior:
  - Headless/server deployments: return `{"open": false, "reason": "headless/no-app"}`.
  - Always pass validated canonical path (not a shell command string) to OS open API.
- `PUT /api/files/content`:
  - Atomic write: write to temp file, then rename (`os.Rename`).
  - Conflict detection: compare content or `ModTime` before write; return 409 if changed since load.

<!-- as-built (2026-09-17): See docs/gotchas/files-tab-preview-only-routing.md §Markdown mode switch.
     as-built (2026-09-21): `.mdx` added to the markdown kind. MDX preview renders as Markdown
     via react-markdown + remark-gfm and is NEVER evaluated (no arbitrary JS execution).
     The editor uses Monaco's `mdx` basic-language grammar while the preview uses the `markdown` kind.
     See docs/gotchas/mdx-preview-not-evaluated.md for the security rationale. -->

## Related

- `docs/gotchas/files-tab-preview-only-routing.md` — Files-tab routing, markdown mode switch, `.mdx` support
- `docs/gotchas/mdx-preview-not-evaluated.md` — MDX preview renders as Markdown, never evaluated (security rule)
