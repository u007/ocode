# Tabs, File Explorer & Git Browser — Design Spec

**Date:** 2026-05-17

## Overview

Add two new TUI tabs to ocode alongside the existing chat view: a file explorer (tree + preview) and a full git browser (lazygit-style, 4 sections). A numbered tmux-style tab bar in the header switches between all three. Chat remains live (streaming, permission prompts, activity) regardless of which tab is active.

---

## Section 1: Tab System

### Tab constants

```
tabChat  = 0
tabFiles = 1
tabGit   = 2
```

Add `activeTab int` to the root `model` struct.

### Header

The header renders tab labels top-right:

```
◆ ocode                          1:chat  2:files  3:git
```

Active tab highlighted (bold/inverted). Inactive tabs are dimmed. If chat has unread activity while on another tab, show a badge: `1:chat●`.

### Keybindings

- `ctrl+1` → switch to Chat tab
- `ctrl+2` → switch to Files tab
- `ctrl+3` → switch to Git tab

### Event routing

**Global messages** (handled by root `model.Update()` regardless of active tab):
- `streamMsgEvent`, `permissionAskMsg`, `authFinishedMsg`, `shellFinishedMsg`, `statusMsg`, `leaderTimeoutMsg` — all chat/agent events are always processed by the root model and chat sub-state, never dropped
- `ctrl+1/2/3` tab switches
- `ctrl+p`, `ctrl+x`, `ctrl+o`, `ctrl+y`, leader sequences — all existing globals unchanged

**Tab-local messages** (forwarded to active sub-model only):
- Keyboard input not matching a global binding
- Sub-model-specific messages (file tree loaded, git status refreshed, etc.)

### Chat liveness

The chat model continues processing agent events in the background on all tabs. Unread indicators (badge on `1:chat●`) appear when new assistant messages, tool calls, or permission prompts arrive while on Files or Git tab. Permission prompts (`permissionAskMsg`) auto-switch back to `tabChat` so the user can respond — they cannot be deferred.

### Modal overlays

Existing modals (`showPicker`, `showConnect`, `showPalette`, `showFullToolOutput`) are global and render on top of whichever tab is active — no change to their logic. They take priority in `renderContent()` before tab routing, exactly as today.

### `tab` key conflict resolution

- **Chat tab:** `tab` retains its existing meaning (cycle agent)
- **Files tab:** `tab` has no existing meaning — can be assigned freely (currently unused)
- **Git tab:** `tab` cycles focus between the 3 panels (sections → files → diff)

Root `Update()` checks `activeTab` before dispatching `tab` keypress.

### Status bar

Status bar hint updates per-tab to show relevant keybindings.

---

## Section 2: File Explorer (`filesModel`)

### Struct

Standalone `filesModel` with `Init`, `Update`, `View`. Owned by root `model` as `m.files`.

### Layout

Two-pane horizontal split:

- **Left pane** (~35% width): navigable file tree
- **Right pane** (~65% width): read-only file preview (scrollable viewport)

### File Tree

- Nodes lazy-loaded: directory children read from disk only on expand
- State: visible node list, cursor index, scroll offset
- Navigation: `j`/`k` or arrows to move; `enter`/`space` to expand/collapse dirs
- Selecting a file updates the right-pane preview

### File Preview

- Read file content into a viewport on cursor change
- **Binary file detection:** if the file contains null bytes in the first 512 bytes, show `[binary file]` instead of content
- **Size cap:** files over 1MB show a truncated preview with a `[truncated — 1MB limit]` notice
- Plain text only in v1, no syntax highlighting

### Opening in External Editor

`e` or `enter` on a file node calls `tea.ExecProcess` to suspend the TUI and open the file in the configured editor. The TUI resumes when the editor exits.

**During editor suspension:** agent streaming is paused (bubbletea limitation with `ExecProcess`). A status message "Editor open — agent paused" is shown before suspension. On resume, the agent stream reconnects and any buffered events are replayed.

**Editor resolution order (config overrides env):**

1. `editor` field in `ocodeconfig.json`
2. `$VISUAL` env var
3. `$EDITOR` env var
4. Fallback: `vi`

### Search

`/` opens fuzzy file search, reusing existing `fuzzy.go`. Selecting a result navigates the tree to that file and loads its preview.

### Keybindings (Files tab)

| Key | Action |
|-----|--------|
| `j` / `↓` | move down |
| `k` / `↑` | move up |
| `enter` / `space` | expand dir / open file in editor |
| `e` | open selected file in editor |
| `E` | choose editor, save choice, then open selected file |
| `/` | fuzzy search |

---

## Section 3: Git Browser (`gitModel`)

### Struct

Standalone `gitModel` with `Init`, `Update`, `View`. Owned by root `model` as `m.git`.

### Layout

Three-panel horizontal split:

```
[ Sections ] [ Files / List ] [ Diff / Detail ]
   ~20%           ~30%              ~50%
```

`tab` cycles focus: sections → files → diff → sections. Focused panel has a highlighted border.

### Auto-refresh

After any mutating git operation (stage, unstage, discard, commit, stash apply/drop, branch checkout), the git model reruns the relevant `git` commands and rebuilds its state. No manual refresh needed.

### Sections Panel (left)

Fixed list:

1. Changes
2. Log
3. Stash
4. Branches

`j`/`k` navigates, `enter` activates.

---

### Changes Section

**Middle pane:** file list grouped:
- `● staged (n)` — files in index
- `○ unstaged (n)` — modified but not staged
- `? untracked (n)` — new files

**Right pane:** `git diff --cached <file>` (staged) or `git diff <file>` (unstaged), in a scrollable colored viewport.

**Operations (middle pane focused):**

| Key | Action |
|-----|--------|
| `s` | stage: `git add <file>` |
| `u` | unstage: `git restore --staged <file>` |
| `d` | discard: `git restore <file>` (confirmation prompt first) |
| `c` | open commit input (see below) |

**Inline commit:** small textarea at tab bottom. `enter` commits with `git commit -m <message>`. `esc` cancels. For multi-line messages, `e` opens `$EDITOR` (or configured editor) for the commit message body.

---

### Log Section

**Middle pane:** commit list — `<hash>  <subject>  <author>  <age>`.

**Right pane:** files changed in selected commit (`git show --name-status <hash>`). `enter` on a file in the right pane shows full diff for that file.

---

### Stash Section

**Middle pane:** `git stash list`.

**Right pane:** `git stash show -p <ref>`.

| Key | Action |
|-----|--------|
| `a` | apply stash: `git stash apply <ref>` |
| `D` | drop stash: `git stash drop <ref>` (confirmation prompt) |

---

### Branches Section

**Middle pane:** `git branch -a`, current branch highlighted.

**Right pane:** recent commits on selected branch (`git log --oneline -20 <branch>`).

| Key | Action |
|-----|--------|
| `enter` | checkout: `git checkout <branch>` |

---

### Error handling

Git command errors (non-zero exit) surface as a transient status message at the bottom of the git tab. The panel state is not mutated on error.

### Keybindings (Git tab)

| Key | Action |
|-----|--------|
| `tab` | cycle panel focus |
| `j` / `↓` | move down in focused panel |
| `k` / `↑` | move up in focused panel |
| `s` | stage file (Changes) |
| `u` | unstage file (Changes) |
| `d` | discard with confirmation (Changes) |
| `c` | open inline commit input (Changes) |
| `e` | open editor for commit message body (Changes, commit input open) |
| `a` | apply stash (Stash) |
| `D` | drop stash with confirmation (Stash) |
| `enter` | checkout branch (Branches) / show file diff (Log) |

---

## Config

Add `Editor string` field to `ocodeconfig.json`:

```json
{
  "editor": "nvim"
}
```

If absent, falls back to `$VISUAL` → `$EDITOR` → `vi`.

---

## File Structure

| File | Change |
|------|--------|
| `internal/tui/files_model.go` | new — `filesModel` struct, Init/Update/View |
| `internal/tui/git_model.go` | new — `gitModel` struct, Init/Update/View |
| `internal/tui/model.go` | add `activeTab`, `files`, `git`; update routing, header, status bar |
| `internal/config/ocodeconfig.go` | add `Editor string` field |

No new external dependencies. Reuses: `viewport`, `textarea`, `fuzzy.go`, `tea.ExecProcess`, `exec.Command`.

---

## Out of Scope (v1)

- Syntax highlighting in file preview
- Interactive rebase, cherry-pick, merge conflict resolution
- Remote push/pull/fetch operations
- Multi-file staging (hunk-level staging)
