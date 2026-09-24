---
type: Gotcha
title: Web UI Mobile Layout Breakage (≤767px)"
description: '"Gotcha: web UI mobile layout broke because sidebar CSS reserved space on phones, default-open sidebars never closed, a CSS grid collapsed the tab strip, and the floating bottom bar overflowed the viewport edge. Five root causes, eight responsive fixes, and regression tests. Rule 7 now documents per-session side-pane scoping."'
tags:
  - gotcha
  - mobile
  - layout
  - css
  - web-ui
  - sidebar
  - responsive
  - side-pane
  - preview
timestamp: 2026-09-24T04:41:08Z
resource: https://github.com/aimsai2/ocode/blob/main/web/src/lib/sidePaneVisibility.ts
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

1. **`ProjectSidebar` mobile drawer pattern.** Gained an `isMobile` prop. On mobile: renders a fixed left off-canvas drawer (`fixed inset-y-0 left-0 z-50 w-72 max-w-[85vw]`) with `translate-x-0`/`-translate-x-full` toggle + `z-40` scrim, always mounted. Desktop 40px collapsed rail is skipped on mobile. Selecting a project auto-dismisses the drawers. The ~145-line expanded markup is shared via a hoisted `renderExpandedInner()` function to avoid duplication.

2. **Lazy-init from viewport width.** `sidebarOpen`/`coworkOpen` initialize from `window.innerWidth >= 768` (not `true`). Resize handle gated with `!isMobile`. `width` prop only when `!isMobile`. Cowork wrapper class becomes `isMobile ? "" : "w-72 flex-shrink-0 …"`.

3. **`TopTabs` mobile menu button.** Gained optional `onMenuToggle` rendering a `PanelLeft` button with `aria-label="Open projects sidebar"` (`md:hidden`) — the only way to reopen the project drawer on mobile. `App` passes it only when `isMobile`.

4. **`UnifiedTabBar` responsive layout.** `flex flex-col gap-1 sm:grid sm:grid-cols-[minmax(0,1fr)_auto]`; pills `w-full sm:w-52`; actions `justify-start sm:justify-end flex-wrap`.

5. **`SpeechToolbar` wrapping bar on phones.** On mobile, `inset-x-2` + `flex-wrap` gives a full-width bar that wraps long content. No base `left-1/2` or translate — just `justify-center` and `gap-1.5`. At `sm` and above, the original centered single-line pill is reconstructed via `sm:inset-x-auto sm:left-1/2 sm:max-w-[calc(100vw-1rem)] sm:-translate-x-1/2 sm:flex-nowrap sm:justify-start sm:gap-2 sm:px-3`.

6. **`ProjectSidebar` narrow-width row wrap (expanded row).** A separate narrow-width contract on the expanded `SortableProjectRow` (`web/src/components/Layout/ProjectSidebar.tsx`), independent of the mobile drawer. The row container is a wrapping flex line — `w-full justify-start flex-wrap gap-x-2 gap-y-1 px-2 h-auto py-2 text-sm` (was `gap-2`). The label block (`name` + `path` + `RemoteProjectStatus`) is `flex-1 min-w-[5rem] text-left` (was `min-w-0 flex-1`), holding a 5rem floor so the name never collapses. The trailing badge cluster (Bell "need attention", session count, streaming, stalled, pending, terminal beep) is `flex flex-wrap items-center gap-1 shrink-0 max-w-full` (added `flex-wrap max-w-full`). When the sidebar is too narrow for both a readable name and the badges, the badge cluster drops to a second line **under** the row instead of staying right and squeezing the name to nothing; it wraps as a unit (`shrink-0`) and, at the 160px minimum width, wraps internally rather than overflowing. Verified in headless Chromium at 160/200/240/280/320/400/500px — no horizontal overflow; 1 badge stays right down to 200px, 4–5 badges wrap at ≤240px. `cn()`/twMerge strips `buttonVariants`' `justify-center`, so the wrapped line is left-aligned. The collapsed icon rail (~line 1000) uses absolute overlays and is unaffected.

7. **Right-hand side pane is desktop-only; browser + preview move to tabs on mobile.** `shouldRenderSidePane` (`web/src/lib/sidePaneVisibility.ts`) gained an `isMobile` gate and returns `false` at ≤767px, so the right-hand Browser/Preview pane beside the chat never renders on phones (was a silent no-op before — activations disappeared and there was no UI at all). `App.tsx` passes `useIsMobile()` and also hides the pane's 🌐 "Toggle browser panel" button on mobile. Browser + preview remain reachable as **tabs** on mobile: the `UnifiedTabBar` browser pills and "New browser tab" button, and the session sub-tab `SessionSubTabs` → `PreviewTabPage`. Activation routing is no longer a silent no-op: when an AI `preview_open` tool fires (via `usePreviewActivation`) or the file tree dispatches `ocode:open-preview`, the App effect switches the active session sub-tab to `"preview"` and passes the one-shot `request`/`nonce`/`onConsumeActivation` into `PreviewTabPage`, which selects the file and acknowledges the activation. Desktop keeps the activation in the side pane (`PreviewHost`). This is a behavioral routing rule rather than a CSS fix — the 767px gate mirrors the breakpoint used by Rules 1–5.

   **Per-session pane state (not cross-session).** The pane's open/collapsed state is preserved **per session**, not shared across sessions: it lives in localStorage `ocode.ui.sidebarPreview.v2` keyed by side stateKey (`side:chat:<sessionId>` for the chat surface, `side:term:<terminalId>` for the terminal surface). Switching to a **different chat** shows that chat's own pane state (typically closed) — opening the pane in one chat never opens it in another. Switching between a session's **own sub-tabs** (chat ↔ terminal) keeps each surface type's state separate. Returning to the same session restores exactly what it had. The single source of truth for the stateKey convention and rekey lifecycle is `gotchas/side-pane-statekey-convention.md`.

8. **Files-tab editor header Save button is the touch save path.** `FileEditor` (`:862-878`) renders a lucide `<Save />` button (`aria-label="Save file"`) when an `onSave` prop is provided and the buffer is `dirty`; disabled when clean, omitted entirely for read-only surfaces (no `onSave`). This is the only save mechanism on touch devices — there is no keyboard for `⌘S`/`Ctrl+S`. App.tsx passes `dirty={et.isDirty}` and `onSave={() => void saveEditorTab(et.id).catch((e) => reportActionError(e, "Save file"))}`; save failures surface via `ActionErrorToast`. Header layout contract: the path is `truncate min-w-0` (never forces the action cluster off-screen), the action cluster is `flex shrink-0` (never squished below its content width), the **Save button is first** in the cluster so it survives the narrowest panes, and the "Settings" text label is `hidden sm:inline` (icon-only below `sm`). Live-verified with Playwright at 390px and 360px (Save visible with the file tree both expanded and collapsed) and 320px (visible with the tree collapsed; at ≤320px with the tree expanded the editor pane is only ~60px wide, so the tree must be collapsed). `FileEditor.save.test.tsx` and `App.editorTabScope.test.tsx` ("wires a Save handler and the dirty flag into each editor pane") cover this.

## Regression Tests

All verified to **FAIL against the pre-fix code** by temporary mutation:

| File | Describe block | Cases |
|------|---------------|-------|
| `web/src/components/Layout/ProjectSidebar.test.tsx` | "ProjectSidebar mobile drawer" | 4 cases |
| `web/src/components/Layout/ProjectSidebar.test.tsx` | "ProjectSidebar project indicators" — "wraps the badge cluster below a narrow row instead of squeezing the label" | pins the narrow-width CSS contract (row `flex-wrap`, label block `min-w-[5rem]`, cluster `shrink-0`) — jsdom has no layout engine, so it asserts the class contract; mutation-verified by reverting the two classes |
| `web/src/components/Layout/UnifiedTabBar.test.tsx` | "stacks full-width session rows on phones…" | phones row stacking |
| `web/src/components/Layout/TopTabs.test.tsx` | (new file) | 2 cases — menu button renders on mobile, hidden on desktop |
| `web/src/components/Speech/SpeechToolbar.layout.test.tsx` | (new file) | asserts `flex-wrap`, `inset-x-2`, `sm:flex-nowrap`, `sm:left-1/2`, and no base `left-1/2` — mutation-verified to fail when `flex-wrap` is removed |
| `web/src/lib/sidePaneVisibility.test.ts` | "never renders on mobile…" | asserts `shouldRenderSidePane` returns false at ≤767px across the mobile gate |
| `web/src/App.previewActivation.test.tsx` | "App side pane on mobile" describe | mobile activation routes to the preview sub-tab instead of the side pane |
| `web/src/components/Preview/PreviewTabPage.activation.test.tsx` | (new file) | 4 cases — one-shot `request`/`nonce`/`onConsumeActivation` consumed, file selected, tab switched on mobile |
| `web/src/components/Files/FileEditor.save.test.tsx` | "FileEditor header Save button" | renders disabled when not dirty; enables and calls `onSave` when dirty; omitted when `onSave` undefined (read-only surfaces) |
| `web/src/App.editorTabScope.test.tsx` | "wires a Save handler and the dirty flag into each editor pane" | pins `dirty` + `onSave` props passed to each `FileEditor` |

## Verification

Tested with Playwright at 390×844 (iPhone 14 viewport) against the running server, plus DOM geometry measurements confirming `main` column width is non-zero and session pills no longer overflow the actions column. SpeechToolbar measured right edge ≤ 390px with no horizontal scroll on mobile; desktop box 799px centered single row (unchanged). Full web suite 213 files / 1843 tests pass; `tsgo --noEmit` and `vite build` clean.

## Why This Keeps Happening

Mobile overlay/drawer patterns are fragile across refactors. When an inline sidebar component is refactored (e.g. `SessionSidebar` → `ProjectSidebar`), the mobile handling is often not carried forward because it lived in the *parent* wiring (App.tsx media queries) rather than being co-located in the component. Treat mobile as a first-class layout concern in every sidebar refactor — not an afterthought bolted on later.

The same pattern applies to floating fixed-position UI elements like toolbars and bottom bars: `flex-nowrap` + centered translation works fine on wide screens but creates overflow on narrow viewports. Always add a mobile-first responsive override that allows wrapping or switches to a full-width layout below `sm`.

Feature panes that assume a wide viewport follow the same discipline problem: any component gated to a side-pane position must check `isMobile` and route to a tab-based equivalent rather than silently no-oping. The 767px breakpoint is the shared contract across Rules 1–8.

Interactive controls that survive only in a side pane (a chat's 🌐 toggle, the file tree's Open button) must have a tab-based equivalent for mobile or they become invisible. The side pane's open/collapsed state is session-scoped (`side:chat:<id>` in `ocode.ui.sidebarPreview.v2`), not global per project — the pane a user left open in one chat is not expected to appear in another.
