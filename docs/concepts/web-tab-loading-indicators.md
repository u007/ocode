---
type: Concept
title: Web tab loading indicators
description: 'Concept: keyed tab loading indicators in the web UI — identity, store, guards, wiring, and the independent Git badge poll.'
tags:
  - web
  - frontend
  - tabs
  - loading-indicators
  - react
timestamp: 2026-09-24T12:13:15Z
---
# Web tab loading indicators

**Status:** implemented (2026-09-24). Companion docs: `superpowers/specs/2026-09-24-tab-loading-indicators-design-design.md` (design) and `superpowers/plans/2026-09-24-tab-loading-indicators.md` (plan).

## What it is

The data-backed main tabs (Files, Git, Cron, Assets) and the session Changes sub-tab expose honest loading states driven by their own loaders:

- **Initial load** (first load per key for the current project) → blocking overlay over the panel with spinner + `aria-busy`.
- **Refresh** (later loads, polls) → non-blocking spinner on the tab trigger, only after the request has run for ~300ms so ordinary 10s polls never blink.
- **Initial error** → blocking overlay with the message and a **Retry** button.
- **Refresh error** → content is retained (no overlay, no clearing of already-loaded rows); the store returns to `idle`.

## Key identity: `${host}\0${projectPath}\0${tabKey}`

`tabLoadKey(host, projectPath, tabKey)` in `web/src/hooks/useKeyedLoad.ts:228-234` joins the three segments with a literal NUL (`\u0000`), which cannot occur in host ids or paths, so composite keys never collide. The NUL separator is also why the same path on two remote hosts is two distinct keys.

- Main tabs: `tabLoadKey(activeProjectHost, activeProjectPath, "files" | "git" | "cron" | "assets")` — built once in `web/src/App.tsx:278-281`.
- Session Changes: tab segment is `` `${sessionId}:changes` `` — `web/src/App.tsx:1590-1594` and `web/src/components/Layout/SessionSubTabs.tsx:45-48`.

Keying by project identity alone is insufficient: in an A→B→A round trip the key matches again, so a per-request **generation** (below) is also required to reject a late response issued during the *first* visit to A.

## Module-level keyed store + `useKeyedLoad`

All state lives at module scope in `web/src/hooks/useKeyedLoad.ts` (not React context): `entries: Map<string, Entry>` keyed by origin key, `ownerRecords` per hook instance (a `Symbol` owner claimed/released around key ownership, `claimTabLoadingKey`/`releaseTabLoadingKey`), and a listener set with an immutable `storeSnapshot`.

- Panels call `useKeyedLoad(loadingKey, onLoadingEvent)` (`useKeyedLoad.ts:267-350`) to get a `run(load, options)` runner. `run` emits `start` → `success`/`empty`/`error` request events; a status snapshot per key is `{phase: idle|initial|refresh|error, error?, retry?}`.
- Subscribers read through `useTabLoadingStore()` (`useKeyedLoad.ts:251-260`), a `useSyncExternalStore` over the module store. This is what avoids prop drilling and keeps `App.tsx` diffs small: the store subscription is the only channel between panels and triggers/overlays.
- Store entry transitions (`emitOwnedEvent`): `start` before first success → `initial`; `start` after a success → arms the 300ms timer then `refresh`; `success`/`empty` → `idle` (marks `resolved`); `error` before first success → `error` + message + `retry`; `error` after a success → back to `idle` with content untouched (refresh errors never surface as an overlay).

## Generation + AbortController guards

Every `run` increments `generationRef` and aborts the previous `AbortController` before creating a new one (`useKeyedLoad.ts:300-318`). After `await load(...)`, `isCurrent()` requires all three to hold: same generation, same key as at run start, and signal not aborted. Otherwise the result is returned as `{status: "stale"}` (or `"aborted"` for `AbortError`) and **never emitted to the store nor applied by the caller**. Terminal store events are also dropped when `event.generation !== entry.generation` (`useKeyedLoad.ts:159`). Unmount/key-change invalidates via `invalidate()` in the claim effect (`useKeyedLoad.ts:287-298`).

## 300ms refresh gate

`REFRESH_INDICATOR_DELAY_MS = 300` (`useKeyedLoad.ts:4`). On a refresh `start`, the store stays `idle` and arms a timeout; only if the generation is still current and the entry still `resolved` when it fires does the phase become `refresh` (`useKeyedLoad.ts:180-188`). A new run or a terminal event clears the timer, so fast polls and quick user-triggered reloads never show a spinner.

## Initial blocking / error Retry

`TabLoadingOverlay` (`web/src/components/common/TabLoadingOverlay.tsx`) is `absolute inset-0`, `role="status"` + `aria-live="polite"` + `aria-busy`, with a spinner + label while `active`, or an error icon + message + **Retry** button when `error` is set (Retry is the registered `retry`, defaulting to re-running the same `load`). It renders only when there is something to say. `TabLoadingIndicator` (`web/src/components/common/TabLoadingIndicator.tsx`) is the compact trigger affordance: 14px spinner while active, 6px red dot when `error`, sr-only label either way.

## Refresh errors retain content

For an already-resolved entry the `error` transition keeps `phase: "idle"` and does not install an overlay (`useKeyedLoad.ts:205-222`), and panels keep their previously loaded rows — e.g. `GitPanel.load` (`web/src/components/Git/GitPanel.tsx:257-305`) only clears/sets its inline `error` banner relative to `background` and never wipes existing file lists on a failed background refresh. Existing panel-level feedback (inline notices/banners) remains the error channel for refreshes.

## Scoped store clear on project switch

`App.tsx:269-278` tracks `{host, path}` in a ref (`previousLoadingProject`) and, when the active project's host or path changes, calls `clearTabLoadingForProject(previous.host, previous.path)` (`useKeyedLoad.ts:360-373`) — which drops only the **previous** project's key prefix (`host\0path\0…`), invalidates its owner records, and clears any armed refresh timers for those keys.

This is deliberately scoped rather than a full wipe: child effects for the newly active project claim their fresh keys **before** this parent effect runs, so clearing the whole map would cancel those just-started requests (`App.tsx:272-274`). `clearTabLoadingStore()` (`useKeyedLoad.ts:351-357`) still exists and clears everything (entries, timers, owner records) for an explicit full reset — production callers should use the scoped helper, and `__resetTabLoadingStoreForTests` is the test-only wrapper around the full clear. Either way, the guarantee holds: a visible spinner always means "loading for the currently selected project"; stale flags from the old project cannot linger.

## App-owned overlay/trigger wiring

`App.tsx` owns all composition; panels only receive `loadingKey` + `onLoadingEvent`:

- Keys and `busy` derivations: `App.tsx:278-289`; event funnel `handleTabLoadingEvent` → `emitTabLoadEvent` (`App.tsx:265-267`).
- Each `TabsContent` carries `aria-busy` and mounts a `TabLoadingOverlay` sibling: Files `App.tsx:1359-1375`, Git `1472-1482`, Cron `1490-1497`, Assets `1505-1508`, session Changes `1603-1613`.
- Wired panels: `FileTree` (`web/src/components/Files/FileTree.tsx:807-809`), `GitPanel:164`, `CronPanel:31`, `AssetsPanel:107`, `ChangesPanel:29` — each through `useKeyedLoad`.
- Trigger indicators: `web/src/components/Layout/TopTabs.tsx:204/219` (main triggers) and `267/277` (the overflow Select, whose items render in a portal so the indicator must live inside the item content); `web/src/components/Layout/SessionSubTabs.tsx:45-48` on the Changes item only. Reads go through `useTabLoadingStore()` in `App.tsx:264` passed down as `loadingStates`.

Tabs are force-mounted, so hidden panels can load invisibly; their trigger indicator still shows. Deliberately **not** wired: Settings and the main Sessions triggers, all other session sub-tabs (chat/terminal/browser), editor tabs, preview viewers, browser and terminal panels — those own specialized loading UI.

## TopTabs' Git badge poll is deliberately independent

`web/src/components/Layout/TopTabs.tsx:77-116` runs its own `fetchCounts()` on a 10s interval plus a `git_status` bus listener to maintain the changed-files badge. It emits **counts only** and is intentionally disconnected from the loading-phase store: badge activity must never drive the tab loading indicator, and indicator phases must never be inferred from the badge poll. The two channels share the `TopTabs` component but nothing else.

## Tests

- `web/src/hooks/useKeyedLoad.test.ts` — key isolation (host/project), generation staleness, 300ms gate, error/retry transitions, `clearTabLoadingStore`/`clearTabLoadingForProject`.
- `web/src/components/common/TabLoadingOverlay.test.tsx` — blocking + Retry rendering, store-driven behavior.
- Panel suites: `FileTree.loading.test.tsx`, `GitPanel.loading.test.tsx`, `CronPanel.loading.test.tsx`, `AssetsPanel.loading.test.tsx`, `ChangesPanel.loading.test.tsx` (incl. `${sessionId}:changes` keys and per-project isolation).
- Trigger suites: `TopTabs.test.tsx`, `SessionSubTabs.loading.test.tsx`.
