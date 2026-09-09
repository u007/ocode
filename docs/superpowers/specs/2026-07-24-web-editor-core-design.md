# Web/Desktop Code Editor Core Parity — Design

- **Date:** 2026-07-24
- **Status:** Approved (design) — pending implementation plan
- **Goal:** Bring the web/desktop editor tabs (`FileEditor.tsx`) to parity
  with baseline code-editor UX: Ctrl/Cmd+P fuzzy file open (matching the
  TUI's `ctrl+p` file-search overlay), in-editor save with dirty tracking,
  and middle-click-to-close on editor tabs with a save/discard/cancel
  confirmation for unsaved changes.

## Context

The web editor (`FileEditor.tsx`, Monaco-based) currently only supports
opening files read-only-in-practice: `onChange` is accepted as a prop but
never wired by either caller (`SessionPage.tsx`, `App.tsx`), there is no
save endpoint, and closing a tab (`TopTabs.tsx` X button) never checks for
unsaved edits. There is also no keyboard-driven way to open a file — only
clicking through `FileTree`/`GitPanel`. The TUI's `ctrl+p` (`internal/tui/
model.go:4809`) opens a fuzzy file-search overlay that opens the picked
file into an editor tab; this spec reproduces that behavior in the web UI.

This is split from the companion spec (`2026-07-24-web-editor-context-diff-
design.md`, editor→chat context chip + inline diff decorations) because the
two are independently shippable: this spec is pure editor-tab mechanics,
the other is git/chat-aware enhancement layered on top.

## 1. User-facing summary

- **Ctrl/Cmd+P** opens a file picker (command-palette style) listing
  workspace files; typing fuzzy-filters; Enter opens the file as an editor
  tab (reusing existing `handleOpenFile`).
- **Ctrl/Cmd+S**, while an editor tab is active, saves that file.
- Editor tabs show a dirty-dot when the buffer differs from the last saved/
  loaded content.
- Closing a tab (left-click **X**, or **middle-click** the tab) when dirty
  opens a **Save / Discard / Cancel** confirmation dialog. Clean tabs close
  immediately either way.

## 2. Decisions (confirmed with user)

| Topic | Decision |
|-------|----------|
| Ctrl+P scope | Web-native fuzzy file picker (cmdk-based), not a literal port of the TUI overlay — TUI's own `ctrl+p` just opens files externally via `$EDITOR`, so "matching TUI behavior" means the file-search interaction, not a shared implementation |
| Save endpoint | Build it now (`PUT /api/files/content`) rather than limiting the close dialog to Discard/Cancel only — needed for "Save" to mean anything |
| File list source | Reuse existing `GET /api/files/tree`, flattened client-side — no new backend list endpoint |
| Error surfacing | Inline text (matches existing `SessionPage` error pattern) — no toast library in this codebase |

## 3. Backend changes

### `PUT /api/files/content` (new)

- File: `internal/server/handler_files.go`, alongside the existing
  `HandleFileContent` (GET).
- Request body: `{"path": string, "content": string}`.
- Writes via `os.WriteFile(path, []byte(content), 0644)`.
- **Path containment guard**: unlike the GET handler (read-only, low risk),
  a write endpoint must not allow escaping the workspace root. Resolve the
  path with `filepath.Clean` + `filepath.Abs`, resolve the configured work
  dir the same way, and reject (400) if the resolved path is not within it
  (or is a symlink escape — `filepath.EvalSymlinks` check). This is new
  scrutiny this spec introduces; the existing GET handler is unchanged.
- Registered in `internal/server/server.go` next to the existing
  `GET /api/files/content` route (same path, new method).
- Response: `{"path": string, "saved": true}` on success; standard
  `writeError` JSON on failure (404 if parent dir missing, 400 on path
  violation, 500 on write error).

## 4. Frontend changes

### File picker — `web/src/components/Files/FilePicker.tsx` (new)

- Built on the same `Command`/`CommandDialog` primitives as
  `CommandPalette.tsx` (`web/src/components/common/CommandPalette.tsx`) for
  visual/behavioral consistency.
- On open, fetches `GET /api/files/tree` and flattens the `FileNode` tree
  into a list of file paths (skip directories), same depth-3 limit as
  `FileTree`.
- cmdk's built-in fuzzy filtering handles the search-as-you-type.
- `onSelect` calls the existing `handleOpenFile(path)` and closes the
  picker.

### Keyboard wiring — `web/src/hooks/useKeyboard.ts`

- Add `onFilePicker` (Ctrl/Cmd+P, `e.preventDefault()` to suppress the
  browser print dialog) and `onSave` (Ctrl/Cmd+S, `e.preventDefault()` to
  suppress the browser save-page dialog) handlers.
- Wire both in `App.tsx` and `SessionPage.tsx` next to the existing
  Cmd+K/Cmd+N wiring. `onSave` is a no-op unless an editor tab is active.

### Dirty tracking — `SessionPage.tsx` / `App.tsx`

- `EditorTab` gains `originalContent: string` (set on load, updated on
  successful save) and derived `isDirty = content !== originalContent`.
- `FileEditor`'s existing `onChange` prop is wired to update the tab's
  `content` in state (currently unused — see `FileEditor.tsx:199`).

### Save action

- Shared `saveEditorTab(id)` function: `PUT`s current content to
  `/api/files/content`, on success sets `originalContent = content`
  (clears dirty). On failure, sets an inline error shown in the editor
  header (reuse the existing header bar in `FileEditor.tsx:161-178`).
- Triggered by Ctrl/Cmd+S and by the confirm dialog's Save button.

### Close handling — `TopTabs.tsx`, `ConfirmCloseDialog.tsx` (new)

- Tab's close `<button>` (currently `onClick` only, `TopTabs.tsx:92-101`)
  adds an `onMouseDown` check for `e.button === 1` (middle-click) calling
  the same close handler as the X button's `onClick`. `stopPropagation`
  preserved for both so it never also switches tabs.
- Close handler becomes `requestCloseTab(id)` in the parent
  (`SessionPage.tsx`/`App.tsx`): if the tab `isDirty`, opens
  `ConfirmCloseDialog` (new, `web/src/components/Files/
  ConfirmCloseDialog.tsx`, built on the same `Dialog`/`DialogFooter`
  primitives as `ChangesPanel.tsx:82-99`) with Save / Discard / Cancel;
  Save calls `saveEditorTab` then closes, Discard closes without saving,
  Cancel dismisses the dialog only. If not dirty, closes immediately
  (existing behavior, unchanged).
- Dirty-dot indicator added next to the tab label in `TopTabs.tsx`.

## 5. Error handling

- No file selected / picker fetch failure: picker shows empty state
  (cmdk's `CommandEmpty`), same as `CommandPalette`.
- Save failure (permissions, disk full, path violation): inline error in
  the editor header; tab stays dirty; confirm dialog (if open) stays open
  so the user can retry or fall back to Discard/Cancel.
- Path containment violation: 400 from backend, surfaced as the inline
  save error — this should not occur in normal use (all opened files come
  from the same-rooted file tree) but guards against a malformed path.

## 6. Testing

Manual QA via the `run` skill in a browser:

1. Ctrl+P → picker opens, fuzzy-filter narrows results, Enter opens file
   as a tab.
2. Edit content → dirty-dot appears on the tab.
3. Ctrl+S → dirty-dot clears; verify file content changed on disk.
4. Middle-click a dirty tab → confirm dialog appears; test all three
   actions (Save-then-close, Discard-then-close, Cancel-stays-open).
5. Middle-click / X a clean tab → closes immediately, no dialog.
6. Attempt to save with a path outside the workspace (manually crafted
   request) → backend rejects with 400.

No existing automated test suite covers the web frontend beyond
`handler_changes_test.go`-style Go handler tests; add a Go test for the new
`PUT /api/files/content` handler (success, missing path, path-escape
rejection) following the pattern in that file.
