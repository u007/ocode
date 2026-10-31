---
type: Concept
title: 'Web Git panel: stacked section heights'
description: How the Git tab's left file pane stacks its five sections and sizes the three list sections by content (with caps) instead of equal flex shares, plus the definite-height chain the percentage caps depend on.
tags: []
timestamp: 2026-10-30T16:19:43Z
---
# Web Git panel: stacked section heights

**Type:** Concept  
**Description:** How the Git tab's left file pane stacks its five sections and sizes the three list sections by content (with caps) instead of equal flex shares, plus the definite-height chain the percentage caps depend on.  

---

# Web Git panel: stacked section heights

## Overview

The Git tab's left pane (`web/src/components/Git/GitPanel.tsx`, `data-testid="git-file-pane"`) stacks five sections: **Staged changes**, **Unstaged changes**, **Commits**, **Worktrees** (`WorktreesSection.tsx`), and **Stashes**. The pane is the single scroll container so the stacked sections stay reachable when they do not all fit: `overflow-x-hidden overflow-y-auto overscroll-contain` (`GitPanel.tsx:1592`). `overflow-x-hidden` still clips during the width animation; below the `md` breakpoint (768px) the same element is an overlay drawer (`absolute inset-y-0 left-0 z-30 ... -translate-x-full` when closed) rather than the older fixed 45% height, so its height is `inset-y-0`.

## Content-sized list sections, not equal flex shares

The three list sections (Commits / Worktrees / Stashes) are **content-sized when expanded**, not equal flex shares. `listSectionClass(expanded, maxHeight)` (`GitPanel.tsx:100`) returns `flex-none min-h-0 <max-h>` when expanded and `shrink-0` when collapsed.

- **Why:** the previous `flex-1` (= `flex: 1 1 0%`) split the pane into equal thirds, so on a phone-pane a two-item worktree or stash list was squeezed into one visible row with an inner scrollbar while space below sat empty.
- **Caps:** Commits `max-h-[30%] md:max-h-[55%]` (`GitPanel.tsx:96`); Worktrees/Stashes `max-h-[45%] md:max-h-[55%]` (`GitPanel.tsx:97`). The commit log is the only list routinely long, so it gets the tighter cap; a phone gets the tightest caps because the drawer/pane is short.
- **Overflow:** a list longer than its cap still scrolls internally (`min-h-0 overflow-y-auto overscroll-contain`, e.g. `WorktreesSection.tsx:253`), and the pane scrolls when the expanded sections together exceed it.
- **Inner-list rule:** the inner list must **not** be `flex-1` (that made the root's intrinsic height collapse to the header); it mirrors `FileSection`'s pattern (`shrink-0 max-h-[34%]` root, plain `overflow-y-auto min-h-0` list).

## Measured live (Chromium 143 and WebKit 26 agree, 1440x900 and 390x844)

- Pane 737px tall; Commits section 405.3px = exactly the 55% cap, its 2449px list scrolling inside.
- An expanded Worktrees section took its content height (89px for one checkout) instead of a share of the pane.
- Parked/collapsed sections render header-only.

## Definite-height chain (the failure mode jsdom cannot catch)

Percentage `max-h` depends on a definite height chain: body `flex-1 min-h-0` → pane (fixed %/width or `inset-y-0` drawer) → flex items. If an ancestor loses its definite height the cap silently becomes `none` and the long commit list crowds the other sections out again. This was verified in real Blink and WebKit; jsdom has no layout engine and cannot catch it.

## Regression tests

- `web/src/components/Git/GitPanel.sections.test.tsx` — CSS-contract assertions: expanded sections are content-sized with their caps and no `flex-1`; collapsed sections are `shrink-0` with no cap; the pane is the `overflow-y-auto` scroll container; inner lists are `overflow-y-auto overscroll-contain`.
- `web/src/components/Git/GitPanel.test.tsx` — mobile drawer and collapse-persistence tests.