# ocode Project Instructions

## File Reading

When reading files, do not show the entire file content. Show only the relevant excerpts needed for the current task.

## TUI Output Safety (alt-screen)

The TUI runs in Bubble Tea's alt-screen. Any write to `os.Stdout`/`os.Stderr` from a
code path the running TUI invokes paints directly over the rendered frame and
corrupts it (text overlap / "hairwire" at the bottom of the chat, status line
pushed off-screen). This is a recurring bug class — when fixing rendering glitches,
suspect raw writes, not just layout.

Rules for any code reachable while the TUI is live (agent loop, tools, hooks,
session, plugins, auth flows, config reload):

- **Never** `fmt.Print*`, `fmt.Fprint*(os.Stdout|os.Stderr, …)`, `println`, or raw
  `os.Stderr.Write` for diagnostics. Route through the debug sink instead:
  `agent.emitDebug(kind, msg)` / `agent.DebugAppendf(...)` inside the `agent`
  package, or the standard library `log` package elsewhere — `tui.Run()` calls
  `log.SetOutput(debugLogWriter{})`, so `log.Printf` lands in the debug panel, never
  the terminal. `emitDebug` falls back to stderr only when no sink is set (headless
  `run`/`serve`/`acp`), where stderr is correct.
- When spawning subprocesses, **capture** output (`cmd.Stdout = &buf`) — never inherit
  the terminal with `cmd.Stdout = os.Stdout`. Surface captured output via `log`/the
  error, not the inherited fd.
- One-line status/activity rows must be clamped with `.Width(w).MaxHeight(1)` so long
  content can't wrap and grow the bottom chrome past the terminal height.

## TUI Mouse: clickable chrome vs selectable content

Terminal mouse capture is **global per frame** — `tea.View.MouseMode` is one flag for
the whole screen, not per-region. Enabling capture (`MouseModeCellMotion` /
`MouseModeAllMotion`) makes tabs/menus/buttons clickable but **blocks the terminal's
native click-drag text selection**. The two are mutually exclusive and cannot be scoped
to a screen region. So you cannot "turn mouse off over the content area and on over the
nav" — there is no such thing.

The correct pattern (and the only one that satisfies "nav is clickable AND content is
selectable"): **keep mouse capture ON and implement selection in-app.** Never disable
`MouseMode` to regain native selection — that kills every click target.

Every scrollable/content surface follows the same in-app selection recipe (see the
transcript, log tab, files preview, git diff, sidebar, and agent-detail drill-in for
working copies):

- A `selectionState{dragging, startLine, startCol, endLine, endCol, active}` field per
  surface.
- **Press** inside the content region → record start + `dragging:true` (return handled).
- **Motion** while dragging → update end, set `active` only once the anchor actually
  moved, re-render with `applySelectionHighlight(styledLines, rawLines, sl,sc,el,ec)`.
- **Release** → if `active`, `extractSelectionText(rawLines, …)` + `clipboard.WriteAll`
  (log copy errors, never swallow); if **not** `active` (no drag distance) clear and
  **fall through to the click handler** so a plain click still toggles/opens. This
  press-starts-drag / release-decides-click-vs-copy split is what lets one region be
  both clickable and selectable.
- Track the surface's styled + ANSI-stripped (`stripANSI`) visual lines so highlight and
  extract operate on the same coordinate space. Selection coords are **screen-row/col
  relative to the content's top-left** (`contentTopY`, left chrome = border(1)+padding(1)
  = 2 cols for bordered boxes).

Mouse-mode gotcha for **hover** effects (e.g. underline-on-hover): `MouseModeCellMotion`
only emits motion events **while a button is held** — it delivers no plain-hover events.
Hover requires `MouseModeAllMotion`, and the motion handler must process `MouseNone`
motion (don't early-return on `Button != MouseLeft` before the hover check). AllMotion
fires on every cursor move, so the hover handler must be cheap: read cached geometry/
hit-test maps populated during render, and only return a redraw when the hovered target
actually changes.

