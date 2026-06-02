---
name: ocode-tui
description: How the ocode Bubble Tea TUI is wired — file map, screen geometry, render pipeline, input/event flow, and recurring gotchas. Use this whenever working on the TUI (new view, new panel, new chrome row, new shortcut, new tab, scrolling/selection bugs, layout overflow).
when_to_use: When the user asks for TUI changes, new tabs/panels, header/chrome tweaks, mouse/scrollbar/selection fixes, or anything under internal/tui.
---

# ocode TUI Field Guide

A short, dense map of the ocode TUI so you don't re-discover it from scratch.

## 1. Single entry point

- `tui.Run(opts RunOptions)` in `internal/tui/tui.go` — redirects `log` to a debug panel, then calls `tea.NewProgram(newModel(opts))`.
- `newModel(opts ...RunOptions) model` in `internal/tui/model.go:984` — assembles the `model` struct: input, viewport, tabs, sidebar, files/git sub-models, agent, theme.
- `View() tea.View` in `internal/tui/model.go:7486` — builds `tea.NewView(m.renderContent())`, sets `AltScreen = true`, sets `MouseMode = MouseModeAllMotion` (required for hover-underline on the sidebar).
- `Update(msg tea.Msg) (tea.Model, tea.Cmd)` in `internal/tui/model.go:1252` — value receiver. Big `switch msg := msg.(type)` for window size, key, mouse, agent, debug, etc.

## 2. Screen layout (chat tab, top → bottom)

```
┌────────────────────────────────────────────┐ y = 0
│  (blank — top pad, appHeaderTopPad)        │ y = 0
│  ◆ ocode <title>  ·  opencode clone v…  ▌1:chat ▌2:files …  ✕ exit │ y = 1   (app header)
│  ╭──── transcript (bordered) ─────────╮  │ y = 2
│  │ …                                  │  │   viewport content rows
│  ╰────────────────────────────────────╯  │
│  (slash popup / queue / agent strip, if any)│
│  ╭──── input (bordered) ───────────────╮   │
│  │ …                                  │   │
│  ╰────────────────────────────────────╯   │
│  ⟳ LLM  │ ⚙ tool1, tool2 …                  │   activity row
│  LLM: ●●○ · Agent: build · Model: …         │   status row
└────────────────────────────────────────────┘
```

The **app header is 2 rows**: a leading blank pad + the title line. Always go through the `appHeaderHeight` constant for any Y math below the header — do NOT hard-code `1`.

| Constant / helper | Where | What it is |
| --- | --- | --- |
| `appHeaderTopPad` (string) | `model.go` | The leading `"\n"` blank row above the title. |
| `appHeaderLeftPad` (string) | `model.go` | A single leading space so the bold `◆` doesn't pin to column 0. |
| `appHeaderHintGap` (string) | `model.go` | The `"  "` between the title and the dim version hint. |
| `appHeaderHeight` (int) | `model.go` | Total header rows = **2** (top pad + title). Use this in every `trackTop / bodyTop / clickY` calculation. |
| `(m model).renderAppHeader(title, hint, tabBar, exitBtn, width)` | `model.go` | The only correct way to render the app header. Returns the full padded line. |
| `(m model).viewportContentTopY()` | `model.go` | First row of the transcript content (= `appHeaderHeight + 1` for the top border). |
| `(m model).agentStripTopY()` | `model.go` | First row of the agent strip. |
| `(m model).logContentTopY()` | `model.go` | First row of the log viewport (= `appHeaderHeight + 3` for header + search + kind bar). |

**Rule:** if you add a new chrome row above the viewport, update `appHeaderHeight` (or the chrome-height sum in `layoutLogViewport` / `bottomChromeHeight`) and the affected `…TopY` helpers in lockstep. Mismatch = mouse clicks land on the wrong row and scrollbar drags jump.

## 3. Per-tab render path

`m.renderContent()` in `model.go:7501` routes by `m.activeTab`:

- `tabFiles` → `m.files.View(w, h, styles, chatUnread, exitPending)` (`internal/tui/files_model.go:1043`)
- `tabGit`   → `m.git.View(w, h, styles, chatUnread, exitPending)` (`internal/tui/git_model.go`)
- `tabLog`   → `m.renderLogTab()` (`internal/tui/model.go:8746`)
- `tabChat` (default) → inline render: header → (transcript + sidebar | status chain) → overflow re-render safety net

Both `files.View` and `git.View` build their own header; **they also use `appHeaderTopPad`/`appHeaderLeftPad`/`appHeaderHintGap`** so the chrome is identical across tabs. If you tweak the constants, all four tabs change.

The chat `renderContent` has a **safety net** at `model.go:7652`: if the rendered output's height exceeds `m.height`, the viewport is shrunk and the layout is re-rendered. New chrome rows that push `bottomChromeHeight` up can trip this — verify with `TestActivityRowGrowthStaysWithinHeight` (uses a deliberately short 13-row terminal).

## 4. Theme + styles

- `internal/tui/theme.go` defines `Styles` (`Header`, `Border`, `Hint`, `Selected`, etc.) and the 20+ built-in themes (`tokyonight`, `dracula`, `gruvbox`, …).
- `ApplyThemeColors(name string) Styles` is the single builder; it also pushes styles into package-level singletons (`headerStyle`, `borderStyle`, `hintStyle`, `selectedStyle`, `statusStyle`, `successStyle`, `errorStyle`, `textStyle`, `thinkingStyle`, `thinkingHeaderStyle`, `sidebarTextStyle`, `dimStyle`, `toolBoxStyle`).
- Tests must call `m.styles = ApplyThemeColors("tokyonight")` (or any name) and the singletons are also wired because `ApplyThemeColors` calls the `set*` helpers. Tests that only set `m.styles.Header` without calling `ApplyThemeColors` get the package-default `headerStyle`, which usually works for the singleton-style render calls.

## 5. Mouse + selection pattern

`tea.View.MouseMode = tea.MouseModeAllMotion` (not `CellMotion`) — required for hover-underline on sidebar files. `CellMotion` only fires motion while a button is held; for plain hover you need `AllMotion` and you must process `MouseNone` motion (don't early-return on `Button != MouseLeft` first).

**Clickable + selectable in one surface = implement selection in-app.** The terminal's native click-drag selection is killed by enabling mouse capture (global per frame, can't be scoped). Every scrollable/content surface (transcript, log tab, files preview, git diff, sidebar, agent detail) follows the same recipe:

1. A `selectionState{dragging, startLine, startCol, endLine, endCol, active}` per surface.
2. **Press** inside the region → record start, `dragging:true`, return handled.
3. **Motion** while dragging → update end, set `active` only when anchor actually moved, re-render with `applySelectionHighlight(styledLines, rawLines, sl,sc,el,ec)`.
4. **Release** → if `active`, `extractSelectionText` + `clipboard.WriteAll` (log copy errors, never swallow); if **not active** (no drag distance), clear and **fall through to the click handler** so a plain click still toggles/opens.
5. Track both styled and `stripANSI` raw lines so highlight and extract share the same coordinate space. Selection coords are screen-row/col relative to the content's top-left (`contentTopY`); bordered box left chrome = 2 cols (border(1) + padding(1)).

See `internal/tui/selection.go`, `handleMouseAction`/`handleMouseMotion` in `model.go`, and the per-surface sel fields (`m.sel`, `m.logSel`, `m.filesSel`, `m.gitSel`, sidebar, detail) for working copies.

## 6. TUI output safety (alt-screen)

Any `fmt.Print*` / `fmt.Fprint*(os.Stdout|os.Stderr,…)` / `println` / raw `os.Stderr.Write` from a code path the running TUI invokes paints over the alt-screen frame and corrupts it (text overlap, "hairwire" at the bottom, status line off-screen). The rules are repeated in `AGENTS.md` and `CLAUDE.md`:

- Use `agent.emitDebug` / `agent.DebugAppendf` inside the `agent` package, or `log.Printf` elsewhere — `tui.Run()` calls `log.SetOutput(debugLogWriter{})` so `log` lands in the debug panel.
- For subprocesses, capture output (`cmd.Stdout = &buf`); never inherit the terminal with `cmd.Stdout = os.Stdout`.
- Clamp one-line status/activity rows with `.Width(w).MaxHeight(1)` so long content can't wrap and push the bottom chrome past the terminal height.

## 7. Test scaffolding

In `internal/tui`:

- `newTestTextarea()` and `derefTestModel(t, value)` live in `slash_popup_test.go:305-321` — the textarea must be focused; `derefTestModel` unwraps the `(model, *model)` mix returned by `Update`.
- Common test model: `model{ready, width, height, input: newTestTextarea(), viewport: viewport.New(...), styles: ApplyThemeColors("tokyonight"), activeTab, ...}`.
- For tests that exercise layout/mouse Y math, **always use `appHeaderHeight`** (or `appHeaderHeight + 1` for first content row inside a bordered panel) — never `lipgloss.Height(m.styles.Header.Render("◆ ocode"))`. The latter returns 1 (just the styled text) and is wrong by 1 row now.
- For tests with a very short terminal (e.g. 13 rows for `TestActivityRowGrowthStaysWithinHeight`), remember the new top pad consumes 1 of those rows. The overflow safety net kicks in if chrome is too tall.

## 8. Common change recipes

**Add a top chrome row (above the transcript):** update `appHeaderHeight` (or the `bottomChromeHeight` sum) and every `…TopY` helper in lockstep. Re-run `go test ./internal/tui/...` — `TestActivityRowGrowthStaysWithinHeight`, the transcript scrollbar track/thumb tests, and the click tests will catch any Y-drift.

**Add a new tab:**
1. New const in `internal/tui/tabs.go` (e.g. `tabFoo = 4`), bump `tabCount`.
2. Add to `labels` in `renderTabBar`.
3. Add a `case tabFoo: return m.foo.View(...)` in `renderContent`.
4. Update `handleGlobalTabKeys` (alt+[/] / ctrl+shift+[/) in `model.go:2143`.
5. Update the `lipgloss.Height(m.styles.Header.Render("◆ ocode  Foo"))` callsites — replace with `appHeaderHeight` (or add a `appHeaderHeight` alias for that tab) so a future header tweak still works.
6. Add a foo sub-model with its own `View`, plus any click/selection fields.

**Add a new mouse-handled region:** follow the §5 selection recipe. Add a `selectionState` field, a `…ForClick(mouse)` and `…ContentTopY()` helper, and wire the press/motion/release paths in `handleMouseAction` / `handleMouseMotion` (and the `scrollbarDrag*` switch).

**Change a theme color:** edit the `Colors` struct in `theme.go` and the relevant `builtinThemes` entry. `ApplyThemeColors` is the only consumer; everything flows from it.

## 9. Files to know

- `internal/tui/model.go` (~9.3k lines) — model struct, Update, View, renderContent, layout, mouse, scrollbar, all the chrome math.
- `internal/tui/theme.go` — themes + style singletons.
- `internal/tui/tabs.go` — tab constants + `renderTabBar`.
- `internal/tui/selection.go` — shared `selectionState`, `applySelectionHighlight`, `extractSelectionText`, `normaliseSelection`.
- `internal/tui/scrollbar.go` — `renderScrollbar`, `scrollbarThumbMetrics`, `scrollbarThumbOffset`, `scrollbarSetOffset`.
- `internal/tui/files_model.go` — files tab View + click handlers.
- `internal/tui/git_model.go` — git tab View + click handlers.
- `internal/tui/tool_render.go` — tool result box rendering.
- `internal/tui/connect.go`, `picker.go`, `question_prompt.go` — modal dialogs.
- `internal/tui/debuglog.go` — debug panel writer (target of `log.SetOutput`).

## 10. Quick grep recipes

- "Where is the header rendered?" → `grep -n "renderAppHeader\|appHeaderHeight" internal/tui/`
- "Where does this chrome row live?" → `grep -n "render<Row>\|<row>Height" internal/tui/model.go`
- "Which Y-offset uses header height?" → `grep -n "appHeaderHeight" internal/tui/`
- "Find the active tab dispatch" → `grep -n "activeTab ==" internal/tui/model.go`
- "Find mouse handlers" → `grep -n "handleMouseAction\|handleMouseMotion\|scrollbarDrag" internal/tui/model.go`
