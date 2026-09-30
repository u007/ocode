---
type: Gotcha
title: Cowork Sidebar LSP Status Overflow (horizontal scrollbar from an unshrinkable cell)
description: 'Gotcha: Cowork sidebar grew a horizontal scrollbar only on chats with a failed language server — flex-shrink-0 status cell with unbounded stderr detail inside an overflow-y-auto pane (axis coupling computes overflow-x to auto). Fix, measured proof, and CSS contract regression test.'
tags:
  - gotcha
  - web-ui
  - layout
  - css
  - flexbox
  - overflow
  - sidebar
  - cowork-sidebar
  - lsp
  - truncate
timestamp: 2026-09-29T08:40:49Z
---
## Symptom

The right-hand **Cowork** sidebar (`CoworkSidebar`, a 288px `w-72` pane) grew a horizontal scrollbar — but only on *some* chats. It was not reproducible in every session, which made it look content- or chat-specific and sent investigation down the wrong path twice.

## Root Cause

In the sidebar's LSP section, each language-server row renders a status cell. For a server with `state: "failed"`, the row's `label` is assigned the server's `detail` field — a **raw, unbounded exec/stderr string**, e.g.:

```
failed to start gopls: exec: "gopls": executable file not found in $PATH (looked in /usr/local/bin, /opt/homebrew/bin, /Users/james/go/bin)
```

Two CSS facts then compounded:

1. The status cell was `<span className="... flex-shrink-0">`. `flex-shrink-0` removes the flex item's ability to shrink below its content width, and the cell had **no truncation** — so a long failure detail made the whole row wider than the 288px pane.
2. The sidebar's scroll container is `flex-1 min-h-0 overflow-y-auto overscroll-contain` with **no `overflow-x` guard**. Per CSS, when one axis is not `visible` the other computes to `auto` — so `overflow-x` computed to `auto` and the pane rendered a visible horizontal scrollbar.

**The general trap:** `overflow-y-auto` alone is enough to make a horizontal scrollbar appear. You never need `overflow-x-auto` anywhere for this to happen — the axis coupling does it for you.

### Why only certain chats overflowed

The LSP section only lists servers when the session's TUI status snapshot has `lsp_servers`, and the overflow specifically needed a **FAILED server with a long `detail`**. Chats with healthy language servers (or none) never overflowed — hence the "chat-specific" illusion.

## Measured Proof

Real browser, the app's own compiled Tailwind CSS, DOM replica of the row inside a 288px pane:

| Variant | scroller `scrollWidth` / `clientWidth` | Horizontal scrollbar |
|---|---|---|
| before (`flex-shrink-0`) | 1051 / 287 | **yes** |
| after (`min-w-0 truncate`) | 287 / 287 | no |
| after, expanded (`whitespace-pre-wrap break-words`) | 287 / 287 | no — grows **vertically** |

Second, an end-to-end check on the **real `CoworkSidebar` component with real data**: this project genuinely has failed language servers (e.g. `language server "bash-language-server" not found in PATH (install it for .sh support)`), so a chat was opened, the LSP section expanded, and the sidebar's own scroll container measured against a server built from the pre-fix bundle and one built from the post-fix bundle:

| Bundle | sidebar scroller `scrollWidth` / `clientWidth` | Horizontal scrollbar |
|---|---|---|
| pre-fix | 726 / 287 | yes |
| post-fix | 287 / 287 | no |

The first table is the class-level A/B on a DOM **replica** of the row; this second table is the **real component with real failed-server data**, old bundle vs new bundle.

## Fix (shipped)

- The status cell **truncates**: `min-w-0 truncate`.
- When the server failed with a detail, the cell becomes a `<button>` that toggles the full message in place — collapsed: `truncate`; expanded: `whitespace-pre-wrap break-words` — with `aria-expanded` and a `title` hint matching the sidebar's existing session-title expand affordance.
- Expansion state is **per server**, keyed by the same `root-cmd` string as the row key.
- The old separate line that re-printed the same `detail` below the row was **removed** — the toggle now owns the full text, so printing the same error twice is redundant, and having one copy truncated while the other is not is worse than either copy alone.
- The non-expandable (short) labels became `min-w-0 truncate` too, so a future long label cannot silently reintroduce the overflow.

**Deliberately NOT done:** no `overflow-x-hidden` on the sidebar or its scroll container. That would mask the bug and clip legitimate content. The fix belongs on the offending element.

## Regression Test

`web/src/components/Layout/CoworkSidebar.lsp.test.tsx` (3 tests). jsdom has no layout engine, so it pins the **CSS contract**: `truncate` present, `flex-shrink-0` absent, expanded classes swap, `detail` rendered exactly once. Mutation-verified: reverting the status cell to `flex-shrink-0`, dropping the expand toggle, and restoring the duplicated detail line each make the suite fail with real assertion errors.

## Rules

1. **Never put `flex-shrink-0` on a cell whose content is externally sourced** (tool output, error details, server messages). Pair unshrinkable cells with `min-w-0 truncate` on the content span, or don't mark them shrink-0 at all.
2. **`overflow-y-auto` implies a horizontal scrollbar under overflow.** Audit any `overflow-y-*` container for unbounded children — you do not need `overflow-x-auto` for horizontal scrolling to appear.
3. **Fix the offending element, don't clip the container.** `overflow-x-hidden` on a pane is a mask, not a fix; it converts an overflow bug into silent content loss.
4. **Conditional rows are latent overflow bugs.** A row that only renders with certain data (failed LSP server, long error detail) will pass every session where that data is absent — write the CSS contract test, not just the happy-path render test.
5. **When a long string must survive, offer an expand affordance instead of a duplicate line.** One element owns the full text (`truncate` ↔ `whitespace-pre-wrap break-words`), `aria-expanded` announced, state keyed per row.

## See Also

`gotchas/web-ui-mobile-layout-breakage.md` — horizontal/viewport overflow causes in the web UI (narrow-viewport class). This page is the *other* class: a flex `min-width:auto` / unshrinkable-child overflow that occurs at any viewport width.
