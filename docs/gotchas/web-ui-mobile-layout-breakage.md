---
type: Gotcha
title: Web UI Mobile Layout Breakage (≤767px)
description: 'Gotcha: web UI mobile layout broke because sidebar CSS reserved space on phones, default-open sidebars never closed, a CSS grid collapsed the tab strip, and the floating bottom bar overflowed the viewport edge. Five root causes, responsive fixes, and regression tests.'
tags:
  - gotcha
  - mobile
  - layout
  - css
  - web-ui
  - sidebar
  - responsive
timestamp: 2026-09-19T13:23:36Z
---
# Web UI Mobile Layout Breakage (≤767px)

**Type:** Gotcha  
**Description:** Gotcha: web UI mobile layout broke because sidebar CSS reserved space on phones, default-open sidebars never closed, a CSS grid collapsed the tab strip, and the floating bottom bar overflowed the viewport edge. Five root causes, responsive fixes, and regression tests.  
**Tags:** gotcha, mobile, layout, css, web-ui, sidebar, responsive  

---

## Root Cause

Five independent CSS/JS issues combined to break phone-width layouts (≤767px) in the web UI:

1. **`ProjectSidebar` had no mobile handling.** The expanded inline flex column always rendered as an inline column, reserving space in the flex row even on phones. The old `SessionSidebar` had a mobile overlay+backdrop (commit `ae6565f3`), but that was lost when it was refactored into the multi-project `ProjectSidebar`.

2. **`CoworkSidebar` wrapper reserved 288px on mobile.** `App.tsx` wrapped `CoworkSidebar` in `<div className="w-72 flex-shrink-0 …">`. `CoworkSidebar` already renders itself as `position:fixed` off-canvas on mobile, but the wrapper still reserved space in the flex row.

3. **Sidebars defaulted to `true` and never closed on mobile.** `sidebarOpen` and `coworkOpen` defaulted to `true`; the mobile media-query effect in `App.tsx` only closed the sidebar on the *rising* edge of the breakpoint (768px crossing from below). A direct load at ≤767px left both open — no transition fires.

4. **`UnifiedTabBar` grid collapsed.** Used `grid grid-cols-[minmax(0,1fr)_auto]` with fixed 208px (`w-52`) pills and a `shrink-0` actions column. The grid's first column collapsed to 0px, so pills overflowed and painted over the action buttons.

5. **`SpeechToolbar` bottom bar overflowed the viewport edge.** Rendered as a single non-wrapping centered pill (`fixed bottom-2 left-1/2 z-40 flex max-w-[calc(100vw-1rem)] -translate-x-1/2 items-center gap-2 …`). On a 390px viewport, the controls and TTS error text overflowed past the right edge (measured `right` beyond the viewport) because `flex-nowrap` plus the centered translation forced all content into a single line with no wrap path.

**Net effect:** Centre `main` column measured 0px wide with both rails open, 146px with only the project sidebar open; fixed-width 208px session pills painted over the action-button column; top tab strip ran off-screen; floating bottom bar clipped past the right viewport edge.

## Fix Rules

1. **`ProjectSidebar` mobile drawer pattern.** Gained an `isMobile` prop. On mobile: renders a fixed left off-canvas drawer (`fixed inset-y-0 left-0 z-50 w-72 max-w-[85vw]`) with `translate-x-0`/`-translate-x-full` toggle + `z-40` scrim, always mounted. Desktop 40px collapsed rail is skipped on mobile. Selecting a project auto-dismisses the drawer. The ~145-line expanded markup is shared via a hoisted `renderExpandedInner()` function to avoid duplication.

2. **Lazy-init from viewport width.** `sidebarOpen`/`coworkOpen` initialize from `window.innerWidth >= 768` (not `true`). Resize handle gated with `!isMobile`. `width` prop only when `!isMobile`. Cowork wrapper class becomes `isMobile ? "" : "w-72 flex-shrink-0 …"`.

3. **`TopTabs` mobile menu button.** Gained optional `onMenuToggle` rendering a `PanelLeft` button with `aria-label="Open projects sidebar"` (`md:hidden`) — the only way to reopen the project drawer on mobile. `App` passes it only when `isMobile`.

4. **`UnifiedTabBar` responsive layout.** `flex flex-col gap-1 sm:grid sm:grid-cols-[minmax(0,1fr)_auto]`; pills `w-full sm:w-52`; actions `justify-start sm:justify-end flex-wrap`.

5. **`SpeechToolbar` wrapping bar on phones.** On mobile, `inset-x-2` + `flex-wrap` gives a full-width bar that wraps long content. No base `left-1/2` or translate — just `justify-center` and `gap-1.5`. At `sm` and above, the original centered single-line pill is reconstructed via `sm:inset-x-auto sm:left-1/2 sm:max-w-[calc(100vw-1rem)] sm:-translate-x-1/2 sm:flex-nowrap sm:justify-start sm:gap-2 sm:px-3`.

## Regression Tests

All verified to **FAIL against the pre-fix code** by temporary mutation:

| File | Describe block | Cases |
|------|---------------|-------|
| `web/src/components/Layout/ProjectSidebar.test.tsx` | "ProjectSidebar mobile drawer" | 4 cases |
| `web/src/components/Layout/UnifiedTabBar.test.tsx` | "stacks full-width session rows on phones…" | phones row stacking |
| `web/src/components/Layout/TopTabs.test.tsx` | (new file) | 2 cases — menu button renders on mobile, hidden on desktop |
| `web/src/components/Speech/SpeechToolbar.layout.test.tsx` | (new file) | asserts `flex-wrap`, `inset-x-2`, `sm:flex-nowrap`, `sm:left-1/2`, and no base `left-1/2` — mutation-verified to fail when `flex-wrap` is removed |

## Verification

Tested with Playwright at 390×844 (iPhone 14 viewport) against the running server, plus DOM geometry measurements confirming `main` column width is non-zero and session pills no longer overflow the actions column. SpeechToolbar measured right edge ≤ 390px with no horizontal scroll on mobile; desktop box 799px centered single row (unchanged).

## Why This Keeps Happening

Mobile overlay/drawer patterns are fragile across refactors. When an inline sidebar component is refactored (e.g. `SessionSidebar` → `ProjectSidebar`), the mobile handling is often not carried forward because it lived in the *parent* wiring (App.tsx media queries) rather than being co-located in the component. Treat mobile as a first-class layout concern in every sidebar refactor — not an afterthought bolted on later.

The same pattern applies to floating fixed-position UI elements like toolbars and bottom bars: `flex-nowrap` + centered translation works fine on wide screens but creates overflow on narrow viewports. Always add a mobile-first responsive override that allows wrapping or switches to a full-width layout below `sm`.
