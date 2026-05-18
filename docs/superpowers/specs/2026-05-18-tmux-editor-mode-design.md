# Tmux Editor Mode Design

## Goal

Make external editing the primary full-power workflow for the Files tab while keeping inline editing as a quick-edit path. Users can choose a default editor and choose how files open. Explicit tmux modes must fail fast at startup when tmux is not available.

## User Experience

- `i` remains inline quick edit in the right preview pane.
- `e` and `enter` open the selected file in the configured external editor.
- `E` keeps the per-file editor picker in the Files tab.
- `/editor` opens an editor picker and saves the selected editor globally.
- `/editor <command>` saves the given editor command globally.
- `/editor-mode` opens an editor mode picker and saves the selected mode globally.
- `/editor-mode <mode>` saves the given mode globally.

## Editor Modes

- `external`: current full-screen external editor flow via Bubble Tea process execution.
- `tmux-split`: opens the editor in a tmux split beside `ocode`.
- `tmux-window`: opens the editor in a new tmux window.

Default mode is `external`. Tmux modes only activate after the user explicitly selects one.

## Tmux Split Behavior

For `tmux-split`, opening a file should create a right-side split by default. If `ocode` width is below 120 columns, use a bottom split. The editor command receives the selected file path as its final argument.

When the editor exits, the tmux pane closes and `ocode` refreshes the current preview.

## Tmux Window Behavior

For `tmux-window`, opening a file creates a new tmux window running the configured editor. The window closes when the editor exits. `ocode` refreshes preview the next time the Files tab receives input, and `R` remains the explicit manual refresh.

## Startup Validation

If the saved editor mode is `tmux-split` or `tmux-window`, startup must validate before the TUI starts:

- `$TMUX` is set.
- `tmux` binary exists.
- `tmux display-message -p '#S'` succeeds.
- Configured editor binary exists.

If any validation fails, exit before launching the TUI with a clear notice. No fallback is allowed for explicit tmux modes.

Example notice:

```text
tmux editor mode requires running inside tmux.
Fix: start ocode inside tmux, or set editor_mode to external in ocodeconfig.json.
```

## Config

Persist settings in `ocodeconfig.json` alongside the existing `editor` field:

```json
{
  "editor": "nvim",
  "editor_mode": "tmux-split"
}
```

Existing `editor` behavior remains intact. `editor_mode` is a new string field with valid values `external`, `tmux-split`, and `tmux-window`.

## Error Handling

- Invalid `/editor-mode` input shows valid modes and does not change config.
- Missing editor during `/editor` selection shows an error and does not save.
- Missing tmux dependencies in explicit tmux mode exits at startup with a notice.
- Runtime tmux command failure shows a Files tab status message and does not silently open another editor mode.

## Files Tab Hints

Preview mode should show concise hints:

```text
i inline edit | e external editor | E choose editor | /editor set default
```

Edit mode should show:

```text
ctrl+s save | esc cancel
```

If editor mode is tmux-based, the hint can name it:

```text
e tmux split: nvim | i inline edit | E choose editor
```

## Testing

- Config load/save tests for `editor_mode`.
- Command tests for `/editor`, `/editor <command>`, `/editor-mode`, and `/editor-mode <mode>`.
- Startup validation tests for tmux modes with missing `$TMUX`, missing binary, failing tmux command, and missing editor.
- Files tab tests verifying `e` dispatches the correct opener for external, tmux split, and tmux window modes.
- Hint tests for preview mode and edit mode.

## Non-Goals

- Do not embed Vim/Neovim inside Bubble Tea.
- Do not make inline edit behave like VS Code.
- Do not add automatic fallback from explicit tmux modes.
- Do not change default behavior for users who have not selected a tmux mode.
