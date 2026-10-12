---
type: Gotcha
title: Two Terminal Emulators Render The Pulse Screen And Disagree
description: The Pulse dashboard card replays a terminal log through @xterm/headless; the terminal_screen tool replays it through charmbracelet/x/vt. A 22-case parity run found three disagreements. Two are x/vt bugs and one is a window-start rule that differs between the two paths. Records the decision to keep the card on xterm, the fixes that followed, and the one open gap.
tags:
  - gotcha
  - pulse
  - terminal
  - emulator
  - xterm
  - x-vt
  - parity
timestamp: 2026-10-12T12:00:00Z
---
## Symptom

A full-screen program (Claude Code) shows a different screen in the Pulse card than
`terminal_screen` returns for the same terminal, or a wide character at the end of a row
disappears from one of them.

## Mechanism

Both paths start from the same log bytes and the same pty size, and both are emulators.
They diverge in three places:

1. **Window start.** Both replay only the newest 256 KiB (`PULSE_TERMINAL_SCREEN_BYTES`,
   `pulseTerminalScreenBytes`), so the first bytes of the window may be the tail of an escape
   sequence. The two paths trim it differently:
   - The card (`restoreTerminalHistory`, `maxBytes` path) skips up to the first `\n`, and only
     in the first 64 KiB page. If that page has no newline, nothing is skipped and the
     sequence tail is painted as text. Claude Code output has very few newlines, so this is
     the likely path to a garbled card.
   - The tool (`pulseTerminalScreenText`) starts at the first ESC when the window is cut, and
     keeps everything when there is no ESC.
2. **`ESC[3J` (erase scrollback).** x/vt's `handlers.go`, `case 3`, calls `scr.Clear()`, which
   blanks the visible screen as well. xterm clears only scrollback. x/vt's comment says it
   matches xterm; it does not. Claude Code's real log has no `ESC[3J`, so this is latent for
   that log.
3. **Wide character at the last column.** x/vt drops a double-width character whose cells reach
   the last column, whether it overflows (`123456789中` at 10 columns) or fits exactly
   (`12345678中x`). xterm keeps it: an overflowing character wraps to the next row, and an exact
   fit stays on its row. In the exact-fit case x/vt also leaves a blank cell, so the text after
   it lands on the same row instead of the next.

## Parity run

22 fixtures, each run through its own side's real windowing code (the card through
`restoreTerminalHistory` with a mocked `fetch`, the tool through `pulseTerminalScreenText`),
comparing the visible rows only. Result: 18 agree. The 4 that do not:

| Fixture | Result | Correct side |
|---|---|---|
| `clear_scrollback` (`ESC[3J`) | x/vt blanks two visible rows | xterm |
| `wide_at_edge` (`123456789中` at 10 cols) | x/vt drops `中` | xterm |
| `wide_at_edge_exact_fit` (`12345678中x` at 10 cols; found after the run) | x/vt drops `中`, leaves a blank, and puts `x` on the wrong row | xterm |
| `window_mid_escape_no_newline` | card paints the tail `2;40HNEW` and the z-run; tool starts at the ESC | tool (window rule) |
| `window_mid_escape_with_newlines` | card keeps text after the first newline; tool starts at the ESC | tool (window rule) |

`real_term726_200x61` (the Claude Code log, 200×61) agrees on the visible screen, but its
two windows start at different bytes (the card starts after a newline, the tool at an ESC).

## Decision

Option B: keep both emulators. The card stays on `@xterm/headless`, the tool stays on x/vt,
and the two are brought into line where they can be.

Option B, in full (done, except the wide-character gap):
- Align the card's window start to the first ESC, matching the tool.
- Strip `ESC[3J` before the tool writes to x/vt.
- Add a parity test over the fixtures.
- Accept the wide-character gap in `terminal_screen` until x/vt changes.

Option A, rejected: consolidate on the server. The card would fetch the screen from a new
`/screen` endpoint that runs the same Go replay as the tool, and `@xterm/headless` would go.
Why not A:
- **It regresses the card.** The card currently handles a wide character at the last column
  correctly. Under A, it would inherit x/vt's drop. The latest x/vt on the module proxy is
  still the pinned pseudo-version (checked 2026-10-12), so there is no upstream fix to wait
  for.
- **It makes the card depend on the x/vt pin.** The user-facing surface would then rest on a
  pseudo-versioned module with no tags.
- **It moves work to the server.** Each poll of an expanded row would replay up to 256 KiB
  and run an emulator on the server. We did not measure this cost; it is a reason, not a
  finding.
- **It does not remove the bugs.** The tool's two x/vt problems would still need fixing or
  working around, so A only adds a new endpoint and a dependency change on top of them.

Why B: the card is already correct on the wide-character case, so the only change it needs is
the window start. The x/vt bugs stay confined to the tool, where the wide-character gap is a
known limit and the `ESC[3J` strip is the approved workaround. B needs no new endpoint.

## Status

Done (2026-10-12):

- **One window rule.** The card's `restoreTerminalHistory` (`maxBytes` path) and
  `pulseTerminalScreenText` both start the window at the first ESC in its first 64 KiB
  page. A page with no ESC is kept whole. The card no longer skips to a newline.
- **`ESC[3J` stripped** before `renderPulseScreen` writes to x/vt.
- **Trailing spaces trimmed on the card.** xterm keeps an explicit trailing space, which
  `terminal_screen` trims, so the card trims too.
- **One fixture, two tests.** `internal/server/testdata/pulse_screen_parity.json` holds the
  small emulator cases. `TestPulseScreenParity` (Go) and the Pulse parity test (web) both
  check it. Window-level cases are not in it; the window rule has its own tests on each side.

Open:

- **Wide character reaching the last column, in `terminal_screen` only.** Both shapes, the
  overflow case (`wide_at_edge`) and the exact fit (`wide_at_edge_exact_fit`), are marked
  `known_gap` in the fixture. The Go test skips them, and the web test still asserts xterm's
  correct result. No input filter can fix it; it needs an x/vt fix or a fork.
  Not measured: how often Claude Code output puts a wide character on the last column.

Tracked in `TODO.md`.

## How to verify

- Go: `go test ./internal/server/ -run 'TestPulseScreenParity|TestRenderPulseScreen|TestPulseTerminalScreen'`.
- Web: `pnpm exec vitest run src/components/Pulse/usePulseTerminals.parity.test.tsx src/components/Terminal/terminalHistory.test.ts`.

The 22-case harness that found these gaps lived outside the repo, because its real-log
fixture is session content. The shared fixture covers the small cases.
