# File Tab Upgrade Design

Date: 2026-05-18

## Goal

Make the Files tab usable as a real file workspace while keeping the existing tree + right-pane layout. The right pane remains a preview by default and can switch into direct edit mode for text files.

## Scope

- Scroll the right preview with mouse wheel while the Files tab is active.
- Show selected file path and metadata above the preview.
- Refresh preview after external editor exits.
- Add file actions: create file, create directory, rename, delete, copy path.
- Add direct inline edit mode for text files with save/cancel.
- Add simple syntax-aware visual treatment and git status badges in the tree.

## Interaction Model

- `enter` / `e`: open selected file in external editor, preserving current behavior.
- `i`: edit selected text file in the right pane.
- `ctrl+s`: save inline edits.
- `esc`: leave inline edit mode without saving.
- `n`: create file in the selected directory or selected file's parent.
- `N`: create directory in the selected directory or selected file's parent.
- `r`: rename selected file or directory.
- `D`: delete selected file or empty directory after confirmation.
- `y`: copy relative path.
- `R`: reload selected preview.
- `/`: fuzzy file search remains unchanged.

## Architecture

Keep `filesModel` as the owner of all file-tab state. Add a small mode field for normal, inline edit, prompt, and delete confirmation. Continue using `viewport.Model` for read-only preview. Add a `textarea.Model` for inline edit content instead of overloading the viewport.

Git status badges are loaded from `git status --short` when available. If the directory is not a git repository or git fails, the tree simply renders without badges and records a visible status message rather than blocking the file tab.

Syntax treatment stays intentionally small: detect common extensions and label the preview language in the header. Full token coloring is deferred because a robust highlighter would add dependency and rendering complexity.

## Data Flow

Selection changes call preview loading for files and directory markers for directories. Inline edit mode loads the selected file content into the textarea. Saving writes the textarea content to the selected path, exits edit mode, refreshes the preview, and rebuilds visible tree metadata.

External editor completion triggers a preview reload of the current selection. File operations update disk first, then rebuild the tree and preview around the affected path.

## Error Handling

All file-operation failures surface in the Files tab status line. Destructive delete requires confirmation. Binary files and files over the preview size cap cannot enter inline edit mode.

## Testing

Add unit tests around `filesModel` behavior: wheel routing, preview metadata, edit/save/cancel, create/rename/delete, copy path command state, external editor refresh command result, and git badge parsing. Existing tab routing tests should continue passing.
