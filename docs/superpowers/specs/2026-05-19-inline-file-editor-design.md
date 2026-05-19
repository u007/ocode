# Inline File Editor Design

Date: 2026-05-19

## Goal

Make the Files tab preview pane editable for text files with a minimal vim-like inline editor. The embedded editor should support modal editing and vim-style save/quit commands without changing the existing external editor workflow.

## Scope

- Add vim-like inline edit mode for editable text files from the Files tab.
- Enter inline edit mode with `i` from either the file tree or preview pane.
- Start edit mode in vim normal mode.
- Support insert mode entry with `i` and `a`.
- Support `esc` to return from insert mode to normal mode.
- Support command mode with `:`.
- Support `:w`, `:q`, `:q!`, and `:wq`.
- Support basic normal-mode movement with `h`, `j`, `k`, `l`, arrow keys, `0`, and `$`.
- Keep existing external editor behavior, tmux editor modes, and editor picker unchanged.
- Refuse inline editing for directories, binary files, unreadable files, and files above the current preview size cap.

## Interaction Model

- `i` from the file tree or preview pane: load the selected file into the right pane as a vim-like inline editor in normal mode.
- `i` in inline normal mode: enter insert mode before the cursor.
- `a` in inline normal mode: enter insert mode after the cursor.
- `esc` in insert mode: return to normal mode.
- `:` in normal mode: enter command mode.
- `:w`: write the current buffer to disk and stay in edit mode.
- `:q`: exit only if the buffer is clean; otherwise show an unsaved-changes status.
- `:q!`: discard changes and exit edit mode.
- `:wq`: write the current buffer to disk, exit edit mode, refresh the preview, and rebuild file metadata.
- `e` / `enter`: unchanged external editor behavior.

The preview hint should mention both edit paths:

```text
tab jump  i vim edit  e external  E choose editor  /editor set default
```

For tmux editor modes, the external editor hint keeps naming the tmux target:

```text
tab jump  i vim edit  e tmux split: nvim  E choose editor  /editor set default
```

## Architecture

Extend `filesModel` with a new `filesModeEdit` state and a small inline editor state object owned by `filesModel`. The inline editor stores the buffer as lines, cursor row and column, editor sub-mode, command input, dirty flag, original file path, original modtime, and original size.

Inline editing belongs entirely to `filesModel`. It does not use `editorOpener`, `editor_mode`, or tmux helpers. This keeps embedded editing independent from the existing external editor path.

The inline editor intentionally implements a small vim-like subset instead of embedding Vim, Neovim, Helix, or another terminal application. The first version covers safe editing, movement, insert/normal/command mode, and save/quit commands only.

## Data Flow

Selection changes continue to load read-only preview data with `loadPreviewCmd`. When the user presses `i`, the model validates the selected node, confirms the preview is editable, reads the selected file content from disk, captures the file's modtime and size, and initializes the inline editor buffer.

On save, the model checks whether the file changed on disk since edit mode started. If the file is unchanged, it writes the buffer back to the same selected path, updates the clean baseline, refreshes git status, and reloads the preview. `:wq` then exits edit mode. `:q!` exits edit mode and leaves the file unchanged.

## Error Handling

Validation failures appear in the Files tab status line. Inline edit mode must fail fast for directories, missing selections, binary previews, oversized previews, and disk read/write errors.

Write errors keep the user in edit mode so content is not lost. `:q` refuses to exit when the buffer is dirty. `:q!` is the only discard path. Save refuses to overwrite if the target file's modtime or size changed since edit mode started.

## Testing

- Enter vim-like edit mode for an editable text file.
- `i`, typed text, and `esc` update the buffer and return to normal mode.
- Normal-mode movement updates the cursor.
- `:w` writes changed content to disk and keeps edit mode active.
- `:wq` writes changed content, exits edit mode, and refreshes preview state.
- `:q` refuses to exit with unsaved changes.
- `:q!` exits edit mode without changing disk content.
- Save refuses when the file changed on disk since edit mode started.
- Directories and non-editable previews do not enter edit mode and show status.
- Preview hints include `i vim edit` in external and tmux editor modes.

## Non-Goals

- Do not embed Vim, Neovim, Helix, or any other terminal application inside Bubble Tea.
- Do not attempt full Vim compatibility.
- Do not implement advanced Vim commands such as undo trees, visual mode, search, macros, registers, counts, or operators in this change.
- Do not change the configured external editor workflow.
- Do not add fallback editor behavior.
