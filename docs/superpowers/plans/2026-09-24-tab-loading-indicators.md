---
type: Plan
title: 'Plan: Tab Loading Indicators'
description: Implementation plan for keyed tab loading indicators (Files/Git/Cron/Assets + session Changes).
tags:
  - plan
  - web
  - loading-indicators
  - tabs
timestamp: 2026-09-24T14:32:44Z
---
Implementation-ready, high-level plan. No code snippets or diffs; the file map below was supplied and is trusted (line numbers as of 2026-09-24 — re-locate by symbol if concurrent WIP shifts them).

## Goal and scope

Give the data-backed main tabs (Files, Git, Cron, Assets) and the session Changes sub-tab honest loading states driven by their OWN loaders: a blocking overlay for initial loads, a delayed non-blocking indicator for refreshes, a retry overlay on initial errors, and content-retaining behavior on refresh errors — plus matching spinners on the tab triggers (main bar, overflow portal, Changes sub-tab).

In scope:
- New keyed loading hook and two shared UI components.
- Wiring: `FileTree` (loadRoot/refresh), `GitPanel` (load), `CronPanel` (load/poll), `AssetsPanel` (loadFiles), session `ChangesPanel` (refresh).
- Trigger indicators in `TopTabs.tsx` (main triggers + overflow Select portal) and `SessionSubTabs.tsx` (Changes item only).
- Minimal `App.tsx` edits (key/context threading and `aria-busy` wrappers only).

Out of scope (no new loading UI): Settings tab (App.tsx:1430), the main Sessions trigger (App.tsx:1434), all other session sub-tabs (App.tsx:1451-1551: chat/terminal/browser), editor tabs, file viewers, browser panel, terminal panel, server/API changes, visual redesign.

Hard constraints: stable `(host id, project path, tab/session key)` identity; per-request generation + AbortController; 300ms delayed refresh indicators; initial load blocks, refresh never blocks; initial error → retry overlay, refresh error → retain content; TopTabs' independent Git badge poll (`TopTabs.tsx:81-120`) is NOT an indicator driver; the working tree has unrelated concurrent WIP, so `App.tsx` changes stay minimal and isolated.

## Architecture and current files

Data flow: panel loader runs through `useKeyedLoad` → hook maintains a module-level store keyed by composite key → `TabLoadingIndicator` subscribes for triggers, `TabLoadingOverlay` subscribes for the owning panel. Store subscription (via `useSyncExternalStore`) avoids prop drilling, which is what keeps `App.tsx` diffs tiny. The TopTabs Git badge poll stays fully orthogonal (counts only, never phases).

Existing files:
- `web/src/App.tsx` — main Tabs at 1220; tab bodies: Files 1323, Git 1421, Cron 1424, Assets 1427, Settings 1430 (untouched), Sessions 1434; session sub-tab render 1451-1551 (only the Changes branch is wired). Tabs are force-mounted/hidden, so hidden panels can load invisibly; their trigger indicator still shows.
- `web/src/components/Layout/TopTabs.tsx` — main triggers 198-243 get `TabLoadingIndicator`; overflow Select 247-284 renders items in a portal, so the indicator must live in the Select item content too; independent Git badge poll 81-120 is left unchanged and disconnected from phases.
- `web/src/components/Layout/SessionSubTabs.tsx` — subtab defs 6-13, render 76-120; indicator on the Changes item only.
- `web/src/components/Files/FileTree.tsx` — `loadRoot` 977-997 = initial; `refresh` 1003-1019 = refresh.
- `web/src/components/Git/GitPanel.tsx` — `load` 244-295 serves first load and later reloads; first run per key is initial, later runs refresh.
- `web/src/components/Cron/CronPanel.tsx` — `load`/`poll` 14-80; only the first load is initial, every poll tick is refresh (polls must never block).
- `web/src/components/Assets/AssetsPanel.tsx` — `loadFiles` 139-162; first run initial, later refresh.
- `web/src/components/Changes/ChangesPanel.tsx` — `refresh` 25-51; first refresh per session key is initial.

New files:
- `web/src/hooks/useKeyedLoad.ts` — keyed state machine, generation counter, AbortController, 300ms gate, error classification, retry registration, store + subscription.
- `web/src/components/common/TabLoadingOverlay.tsx` — full-panel blocking overlay (initial) and initial-error variant with Retry.
- `web/src/components/common/TabLoadingIndicator.tsx` — compact non-blocking spinner/error dot for tab triggers and refresh corners.

Tests: `web/src/hooks/useKeyedLoad.test.ts`, `web/src/components/common/TabLoadingOverlay.test.tsx`, `web/src/components/common/TabLoadingIndicator.test.tsx`, plus additions to existing FileTree/GitPanel/CronPanel/AssetsPanel/ChangesPanel and TopTabs/SessionSubTabs/App test files.

## Ordered tasks

1. [x] [Step 1 — Build `web/src/hooks/useKeyedLoad.ts`: composite key `(host id, project path, tab/session key)`, per-key generation + AbortController, 300ms gate for BOTH initial and refresh phases, phase store with subscription, initial-error vs refresh-error split, retry registration, unmount cleanup] → verify: `web/src/hooks/useKeyedLoad.test.ts` passes, covering key isolation (A/B, same path under two hosts), late-settle generation drop, abort-on-switch/unmount, delayed vs immediate transitions, empty success clears state, and error split.
2. [x] [Step 2 — Build `TabLoadingOverlay` (blocking initial state; error state with Retry wired to the registered retry; content retained underneath for refresh errors) and `TabLoadingIndicator` (idle/refresh/error-dot), both with `role="status"`/`aria-live`, visually-hidden loading text, and `aria-busy` output] → verify: `web/src/components/common/TabLoadingOverlay.test.tsx` + `TabLoadingIndicator.test.tsx` pass, including an accessibility describe asserting roles, live-region text, and a focusable Retry.
3. [x] [Step 3 — Wire Files: `FileTree.loadRoot` (977-997) as initial, `refresh` (1003-1019) as refresh, overlay mounted in the Files panel root] → verify: existing FileTree test file gains cases where first load blocks past 300ms, an in-time load shows nothing, refresh shows only the delayed indicator, a failed first load shows Retry (retry refetches), and a failed refresh keeps the tree rendered.
4. [x] [Step 4 — Wire Git: `GitPanel.load` (244-295) as first-run-initial/later-refresh, overlay in panel root, indicator driven ONLY by this hook — assert `TopTabs.tsx:81-120` badge poll neither reads nor writes loading state] → verify: GitPanel test file cases for initial-block/refresh-delay/error-retains-content pass, plus one test pinning badge-poll disconnection (poll fires, store unchanged).
5. [x] [Step 5 — Wire Cron and Assets: `CronPanel.load/poll` (14-80, first load initial, every poll tick refresh) and `AssetsPanel.loadFiles` (139-162)] → verify: CronPanel test proves repeated polls never re-block (indicator only when a tick exceeds 300ms) and AssetsPanel test proves initial-block → refresh-delay → empty-success-clears.
6. [x] [Step 6 — Wire session Changes: `ChangesPanel.refresh` (25-51) with the SESSION id as the tab/session key component, first refresh per session = initial] → verify: ChangesPanel test covers first-load block, subsequent refresh non-blocking, refresh error retaining rendered rows, and two sessions loading concurrently keeping separate states.
7. [x] [Step 7 — Surface indicators: `TopTabs.tsx` main triggers 198-243 and overflow Select items 247-284 subscribe for Files/Git/Cron/Assets keys; `SessionSubTabs.tsx` Changes item 76-120 subscribes for its session key; `App.tsx` edits limited to threading existing host/project/tab key inputs and `aria-busy` wrappers at 1323/1421/1424/1427 and the Changes branch of 1451-1551 — no new App state, no edits to 1430/1434 or other sub-tabs] → verify: TopTabs/SessionSubTabs/App tests show the spinner on the right trigger in normal AND overflow-portal rendering, `aria-busy` on the active panel, git diff of `App.tsx` touches only the named lines.
8. [x] [Step 8 — Scenario + gate pass: A→B→A switching, same path on two hosts, unmount mid-load, force-mounted hidden tab, error/empty states, overflow portal, accessibility assertions across all suites] → verify: `tsgo --noEmit`, `vite build`, and scoped then full `vitest run` are green (comparing failures against a pristine baseline — the tree has unrelated concurrent WIP).

## State/event contract

- Key: `(host id, project path, tab/session key)` composed into one store key; distinct hosts with identical paths never share a record. Examples: `files`/`git`/`cron`/`assets` tab keys, and the session id for Changes.
- Record fields (prose): phase, kind (initial | refresh), generation, start time, error message (when failed), registered retry action.
- Phases: `idle` → (begin, timer armed) → `initial-blocking` or `refresh-indicator` after 300ms → `idle` on success | `initial-error` (overlay + Retry) or `refresh-error` (indicator becomes an error dot, CONTENT RETAINED) on failure.
- Delay decision: the 300ms gate applies to BOTH kinds, so fast loads and fast poll ticks flash nothing; initial merely upgrades to a blocking overlay when it trips the gate.
- Generation rule: `begin` increments the key's generation and aborts the prior AbortController; success/failure/timeout callbacks act only if their generation is still current (late A-response after switching to B is dropped; A→B→A restarts a fresh generation).
- Notifications: subscribers are notified only on phase transitions; panels own their overlay, triggers subscribe by key — no event passes through `App.tsx`.
- Lifecycle: unmount aborts in-flight work, clears the timer, deletes the key record; a hidden force-mounted panel keeps loading, its own trigger shows the indicator, and no overlay can leak onto the visible tab.
- TopTabs Git badge poll (`TopTabs.tsx:81-120`): independent, counts only, never a phase source or sink.

## Test matrix

- A→B→A tab switching — hooks + TopTabs tests: no ghost overlay, at most one in-flight generation per key, correct trigger spinner each direction.
- Same path / different host — hooks test: two records, independent phases.
- Unmount mid-load — hooks + panel tests: abort called, timer cleared, no late store mutation.
- Force-mounted hidden tab — App/TopTabs test: hidden panel loads without blocking the visible tab; hidden tab's own trigger still spins.
- Initial error + Retry — Overlay test + each panel: Retry re-runs the loader and can reach idle.
- Refresh error — panel tests: content retained, error dot only.
- Empty success — panel tests: indicator cleared, empty state shown.
- Overflow portal — TopTabs test: indicator rendered inside the portal'd Select item for the loading tab.
- Accessibility — common tests + one per surface: `role="status"`, live-region text, `aria-busy`, focusable Retry.
- Git badge poll disconnection — TopTabs test.
- Cron poll non-blocking — CronPanel test.
- Concurrent sessions, Changes keying — ChangesPanel test.

## Validation commands

```bash
cd web
pnpm exec tsgo --noEmit
pnpm exec vite build
pnpm exec vitest run src/hooks src/components/common
pnpm exec vitest run src/components/Files src/components/Git src/components/Cron src/components/Assets src/components/Changes src/components/Layout
pnpm exec vitest run   # full suite; compare FAIL set against a pristine git worktree baseline
git diff -- src/App.tsx   # must contain only the minimal key/aria-busy hunks
```

## Risks/rollback

- Concurrent WIP: the tree has unrelated in-flight edits; `App.tsx` is contested — keep hunks minimal, re-read files immediately before editing, re-locate line numbers by symbol if they drift.
- Scope leakage: editor/viewers/browser/terminal and Settings/Sessions triggers must stay untouched; rollback = revert the wiring tasks independently — hook and common components are new files and can be dropped wholesale.
- TopTabs badge-poll coupling: the classic mistake is reusing 81-120 for Git's indicator; the disconnection test is the guard — if it fails, drop any badge-poll read rather than adjusting the contract.
- Timer/abort leaks on force-mounted panels: covered by unmount tests; leaks degrade to a stuck spinner, never wrong data (generation gate protects state).
- Portal rendering: Radix Select renders detached from the tab list; if the indicator is placed outside the item content it silently never appears — the overflow-portal test is the guard.
- Behavior change risk for polls: Cron polls becoming non-blocking is intentional; if regressions appear, revert only task 5 wiring.

## Acceptance criteria

- Files, Git, Cron, Assets, and session Changes show a blocking overlay only for initial loads exceeding 300ms, and only a non-blocking delayed indicator for refreshes.
- Initial failures show a working Retry overlay; refresh failures keep existing content visible.
- Tab triggers (main bar, overflow portal, Changes sub-tab) reflect their own panel's state with correct `aria-busy`/live-region semantics.
- Keys are host+path+tab/session-scoped: A→B→A, cross-host same-path, unmount, and hidden force-mounted tabs all behave per the contract.
- TopTabs' Git badge poll is untouched and disconnected.
- Settings/Sessions triggers, other session sub-tabs, and editor/viewers/browser/terminal gain no new loading UI; `App.tsx` diff is minimal.
- `tsgo --noEmit`, `vite build`, and the test suites pass (failures only at pre-existing baseline).