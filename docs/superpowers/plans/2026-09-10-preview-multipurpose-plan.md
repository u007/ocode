## Plan: Multi-Use Preview — Implementation

Based on spec: `docs/superpowers/specs/2026-09-10-preview-multipurpose-design.md`

Status: Plan phase (no code changes yet). Design approved (Draft). Incremental order from spec section 9.

---

### Phase 1 — Tab/store plumbing (projectStore / SessionSubTabs)
- [ ] Add `"preview"` to `SessionSubTabId` enum / persistence in `projectStore`.
- [ ] Add `PreviewTabPage` shell component (`left: FilePicker, right: empty`) with sub-tab state read from `projectStore`.
- [ ] Empty state message; no renderer load yet.

### Phase 2 — Shared PreviewSurface extraction
- [ ] Extract `PreviewSurface` from `PreviewHost.tsx` (renderer dispatch + load/save primitives).
- [ ] Shared preview state types (`path`, `kind`, `editable`, `dirty`, `loadState`, `error`).
- [ ] Ensure `PreviewHost` (sidebar) still works unchanged; run `PreviewHost.test.tsx` regression.
- [ ] No new backend APIs.

### Phase 3 — Full tab rendering
- [ ] Wire `PreviewTabPage` with `FilePicker` + `PreviewSurface` using existing viewers (`PdfViewer`, `DocxViewer`, `PptxViewer`, `ExcelViewer`, `MmdViewer`, `MarkdownViewer`, `ImageViewer`, `TextViewer`).
- [ ] `FilePicker` selection contract: returns `(path, kind, projectRoot)` anchored to active project.
- [ ] `.md` behavior: Monaco (`TextViewer`) by default; `MarkdownViewer` toggle in `PreviewSurface` header for both sidebar and full tab.

### Phase 4 — Activation paths (agent + manual)
- [ ] `preview_open` tool: `PREVIEW_OPEN:` sentinel opens sidebar `PreviewHost`.
- [ ] Promotion: sidebar provides action that dispatches `ocode:preview-promote` with `(path, kind, projectRoot)`; `App.tsx` listens, opens `PreviewTabPage` sub-tab.
- [ ] Manual: full session `Preview` tab → user picks file via `FilePicker`.

### Phase 5 — Edit / save / conflict
- [ ] `TextViewer` (Monaco) saves via `PUT /api/files/content`.
- [ ] Reuse `FileEditor` atomic-write/conflict mechanism (`os.Rename`, conflict detection).
- [ ] Dirty indicator local to `PreviewTabPage`; new file selection replaces/clears.

### Phase 6 — Security hardening (audit first, extend only gaps)
- [ ] Audit `internal/server/handler_files.go` (`GET /api/files/raw`) for symlink (`EvalSymlinks`), allowlist, auth/project scoping.
- [ ] Audit `internal/server/handler_open.go` (`POST /api/files/open`) for privileged path passing (no shell), headless behavior.
- [ ] Add only confirmed missing protections: bounded streaming read (`io.LimitReader` 32 MiB), safe `.html` `Content-Type`, canonical path containment.
- [ ] No duplicate APIs.

### Phase 7 — Testing
- [ ] Agent activation + promotion (`preview_open` → sidebar → `PreviewTabPage`).
- [ ] Manual activation: empty state, file picker selection, each viewer kind.
- [ ] `.md`: Monaco edit, save (`PUT`), dirty indicator, rendered toggle.
- [ ] Sub-tab persistence: `projectStore` / `SessionSubTabs` restore (`"preview"` sub-tab ID).
- [ ] Security: traversal/symlink denial (`EvalSymlinks`), size limit (`io.LimitReader`), allowlist enforcement, auth scoping.
- [ ] Error: corrupt binary (`.pptx`, `.pdf`) → error panel + OS-open fallback (`POST /api/files/open`).
- [ ] Monaco: unique model URI, disposal, large-file limit, save conflict (`409`).
- [ ] Existing `PreviewHost.test.tsx` regression passes.

---

Validation gates (before declaring complete):
- `go build` + `go vet` clean
- `go test ./internal/preview/... ./internal/tool/... ./internal/server/...` passes
- `npm run build` + `npm run typecheck` (if available) clean
- Manual smoke test: open `.pptx`/`.docx`/`.md` in sidebar; open full `Preview` tab; edit `.md`; save; verify no regression in `PreviewHost`

No alternate APIs or duplicate viewers added. Scope is sidebar preservation + new sub-tab + shared renderer + security audit.
