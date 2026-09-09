# Web/Desktop Code Editor — Chat Context + Inline Diff — Design

- **Date:** 2026-07-24
- **Status:** Approved (design) — pending implementation plan
- **Goal:** Make the code editor (`FileEditor.tsx`) chat-aware (the active
  file and current selection are attachable as context to the next chat
  message) and git-aware (added/modified lines highlighted inline, deleted
  lines shown as read-only blocks that can be copied but not edited — VS
  Code-style).
- **Depends on:** `2026-07-24-web-editor-core-design.md` (editor tab/dirty
  mechanics) — independently shippable, but assumes that spec's
  `EditorTab` state shape exists.

## 1. User-facing summary

**Context chip**
- Whenever an editor tab is active, a chip appears above `ChatInput`
  showing the current file (and selected line range, if any), e.g.
  `handler_files.go:39-56`.
- The chip is live — it tracks whichever file/selection is currently
  active, not a fixed snapshot — and gets attached to the next message
  automatically when sent, no manual step required.

**Inline diff decorations**
- Files with uncommitted git changes show:
  - Added/modified lines highlighted with a background color in the
    editor gutter/line.
  - Deleted lines rendered as read-only red-tinted blocks positioned where
    the deletion occurred, with a copy button. These blocks are not part
    of the editable buffer — they cannot be typed into or deleted via
    editor keystrokes, only removed by external git operations (revert/
    stage/etc., outside this feature's scope).

## 2. Decisions (confirmed with user)

| Topic | Decision |
|-------|----------|
| Context delivery | Auto-attach chip (not manual insert) — always reflects current file/selection, attached at send time |
| Attachment format | `@path#Lstart-Lend` (or `@path` with no selection) prepended to the outgoing message, reusing the exact convention `ChatInput.tsx` already uses for uploaded-file `@ref`s (`handleSend`, `ChatInput.tsx:99`) — no backend parsing changes needed, the agent reads it as plain text same as any other `@` file mention |
| Diff data source | Reuse existing `api.getChangeDiff(session, path)` (already powers `ChangesDiffView.tsx`) — no new backend endpoint |
| Deleted-line editability | Must be viewable/copyable but not modifiable, matching VS Code's inline diff peek — implemented as Monaco view zones (outside the editable text model) rather than editable regions with a read-only flag |
| Diff scope | This spec is for the code editor (`FileEditor.tsx`) specifically — the existing Changes-tab diff viewer (`ChangesDiffView.tsx`) is untouched and unrelated |

## 3. Frontend changes

### Context chip

- New state (lives alongside `EditorTab[]` in `SessionPage.tsx`/`App.tsx`):
  `activeEditorContext: { path: string; selection?: { startLine: number;
  endLine: number } } | null`.
- Updated on: (a) active tab switching to/from an editor tab — sets
  `path`, clears `selection`; (b) selection change inside the active
  `FileEditor` — via a new `onSelectionChange?: (sel: {startLine,
  endLine} | null) => void` prop, wired to Monaco's
  `editor.onDidChangeCursorSelection`, filtering out empty/collapsed
  selections.
- New component `web/src/components/Chat/EditorContextChip.tsx`, rendered
  in `ChatInput.tsx` in the same visual slot as the existing
  `attachedFiles` chip row (`ChatInput.tsx:152-170`) — shown only when
  `activeEditorContext` is non-null.
- `ChatInput`'s `handleSend` (`ChatInput.tsx:64-103`) extended: build the
  context ref the same way `refs` is built from `attachedFiles` (line 99),
  prepend it alongside those refs. The chip has no per-message dismiss
  state — it reflects live editor state; there's no "detach for this
  message only" affordance (YAGNI unless requested later).

### Inline diff decorations — `FileEditor.tsx`

- On mount and whenever `path` changes (or after a save from the
  companion spec's `saveEditorTab`), fetch `api.getChangeDiff(session,
  path)`. If it 404s/errors (no session, file has no diff), skip
  decorations silently — no error UI, this is the common case.
- Small client-side unified-diff parser (new util, e.g.
  `web/src/lib/parseDiffPatch.ts`): given the patch text, produce hunks of
  `{oldStart, oldLines, newStart, newLines, lines: {type: 'context'|'add'|
  'del', text}[]}`.
- Added/modified lines → `editor.deltaDecorations` with `isWholeLine:
  true` and a `linesDecorationsClassName`/`className` for background
  color (green for pure additions, blue for modified — i.e. a `del`
  immediately followed by `add` at the same position).
- Deleted lines → `editor.changeViewZones`, inserting a DOM node directly
  above the following context/added line for each contiguous `del` run.
  The node renders the removed text in a monospace, red-tinted block with
  a small copy icon (`navigator.clipboard.writeText(text)`). Being a view
  zone, it is not part of the Monaco text model, so it is inherently
  unaffected by typing/deletion in the real buffer.
- Decorations/zones are cleared and recomputed on each diff refetch
  (store decoration/zone IDs in a ref, dispose old ones first).

## 4. Error handling

- Diff fetch failure (network error, no active session): decorations
  simply don't render; no inline error (matches "silent skip" decision —
  most open files have no diff, so a visible error would be noise).
- Selection reporting: if Monaco's selection API returns a collapsed
  range (cursor with no selection), `onSelectionChange` reports `null`,
  clearing the range from the chip (file path alone remains).

## 5. Testing

Manual QA via the `run` skill in a browser:

1. Open a file with uncommitted git changes → added lines highlighted,
   deleted-line blocks appear at correct positions.
2. Attempt to click into a deleted-line block and type → confirm no
   effect on the buffer (view zone, not editable).
3. Copy button on a deleted block → clipboard contains the exact removed
   text.
4. Open an editor tab with no diff → no decorations, no errors in
   console.
5. Select a range of code → chip updates to show `file:startLine-
   endLine`; switch to a different editor tab → chip updates to new file,
   no selection.
6. Send a message with the chip showing → verify the outgoing message is
   prefixed with the `@path#Lstart-Lend` ref, same as an attached upload.
7. Save the file (from the core spec) → diff decorations refresh to
   reflect the new working-tree state.
