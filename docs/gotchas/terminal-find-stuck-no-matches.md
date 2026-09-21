---
type: Gotcha
title: Terminal find bar stuck on "No matches"
description: SearchAddon find() throws because allowProposedApi was unset on the xterm Terminal.
tags:
  - xterm
  - terminal
  - search
  - allowProposedApi
  - addon
timestamp: 2026-09-21T03:47:16Z
---

# Terminal find bar stuck on "No matches"

## Symptom

The web terminal find bar (`⌘/Ctrl+F` inside a terminal, or "Find…" in the terminal context menu) always reports **"No matches"** for text that is plainly visible in the terminal buffer. The terminal itself renders normally; only find is broken. Searching in either case (`novita` / `NOVITA`) fails, because nothing is ever actually searched.

## Root cause

`SearchAddon` highlights matches by calling `Terminal.registerDecoration()`. xterm 6 gates that API behind the `allowProposedApi` terminal option, and `TerminalPanel` constructed its `Terminal` without it. As a result `SearchAddon.findNext()` threw inside its internal `_highlightAllMatches()` step:

```
Error: You must set the allowProposedApi option to true to use proposed API
```

The throw happens **before** the addon selects the match or fires `onDidChangeResults`, so `TerminalPanel`'s `setFindResult` never runs, `resultCount` stays `0`, and the counter renders "No matches" for every query. Case sensitivity is a red herring — the default search is case-insensitive (`novita` would have matched `NOVITA`).

## Fix

Pass `allowProposedApi: true` when constructing the terminal. The only proposed API in play is `registerDecoration`, which is exactly what draws the match/active-match highlights and the overview-ruler marks — the feature the find bar advertises.

The production xterm options now live in `web/src/components/Terminal/terminalOptions.ts` (`buildTerminalOptions({ scrollbackLines, fontFamily, fontSize, savedBuffer })`), and `TerminalPanel.tsx` constructs `new Terminal(buildTerminalOptions(...))`.

## Why tests missed it

Every `TerminalPanel.*.test.tsx` file `vi.mock()`s `@xterm/addon-search` with a stub whose `findNext` is a `vi.fn()`. Those tests only pin the find bar's wiring (open/close/next/prev/query), so a real search could never actually fail in them.

## Regression test

`web/src/components/Terminal/terminalOptions.test.ts` builds a **real** xterm 6 `Terminal` with the real production options and loads the **real** `SearchAddon`; it writes `#NOVITA_API_KEY="sk_test"` and asserts `findNext("novita", { decorations })` returns `true` and `onDidChangeResults` fires `{ resultIndex: 0, resultCount: 1 }`. A second case uses a 3-occurrence buffer to pin the navigation contract — `resultCount: 3` with `findNext` advancing `0→1→2→0` and `findPrevious` wrapping `0→2`, and `getSelection()` confirming the active match is selected. Mutation-verified: removing `allowProposedApi: true` fails both option assertions and both behavioural tests, reproducing the original `registerDecoration` throw. The tests stub `window.matchMedia` and `devicePixelRatio`, which xterm's `CoreBrowserService` needs to `open()` under jsdom.

## Rule

Any xterm addon that renders decorations (search today, others later) requires `allowProposedApi: true` on the `Terminal`. When adding such an addon, set it at construction time in `buildTerminalOptions()` — and cover it with a real-addon test, not a mocked one.

## Source files

- `web/src/components/Terminal/terminalOptions.ts` — `buildTerminalOptions()` (sets `allowProposedApi`)
- `web/src/components/Terminal/TerminalPanel.tsx` — SearchAddon wiring, find bar state
- `web/src/components/Terminal/TerminalFindBar.tsx` — find UI ("No matches" counter)
- `web/src/components/Terminal/terminalOptions.test.ts` — real-addon regression test
