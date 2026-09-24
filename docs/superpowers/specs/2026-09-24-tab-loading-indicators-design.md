---
type: Design
title: Tab Loading Indicators — Design Spec
description: 'Design spec: accessible blocking initial-load overlays and non-blocking refresh spinners for web tab shells, keyed by (host, project, tab) with generation/abort stale-response rejection.'
tags:
  - design
  - spec
  - web
  - frontend
  - loading
  - tabs
  - superpowers
timestamp: 2026-09-24T08:15:41Z
---
# Tab Loading Indicators — Design Spec

**Status:** Approved (2026-09-24); revised to incorporate advisor review corrections (2026-09-24)
**Scope:** ocode web frontend (`web/src`), shared by desktop via the embedded React SPA
**Non-goals:** no separate desktop frontend; no global API interception/context; no large Suspense refactor; no source changes in this document (spec only); no unrelated refactors

> **Revision note.** This revision adds per-request generation + cancellation for A→B→A late responses, replaces the incomplete `initial|refresh|idle` callback with an explicit request-event contract, removes the redundant "emit idle for old key" cleanup, recommends a shared `useKeyedLoad` module, adds a ~300ms delayed-refresh rule to avoid poll flicker, pins key identity to a stable host id, and replaces the vague scope line with explicit wired/deferred tab lists plus an expanded test plan.

---

## 1. Problem

The web UI (and the desktop shell, which embeds the same React SPA bundle) shows no visible loading feedback for tab content. When a tab's data is fetched asynchronously, the user sees either stale content or an apparently-idle panel, and cannot tell:

- whether a tab's **first load** for the current project is still in flight,
- whether a **background refresh** is running behind visible content,
- whether a spinner (where panels happen to have one) refers to the **currently selected project** or to a previous project.

Because panels use `forceMount` and callbacks can land late, loading flags also go stale across project switches. Keying by project identity alone is **not sufficient**: in an A→B→A round trip the key matches again on return, so a late response issued during the *first* visit to A would still be accepted. Both project identity **and** a per-request generation are required to reject it.

## 2. Goals

1. **Block initial loads visibly.** While a tab's first load for the current project is unresolved, the tab's content is blocked by an accessible centered loading overlay (`aria-busy`, spinner, "Loading" text). The overlay clears on success, on a handled error, or on an empty result.
2. **Keep background refreshes non-blocking.** Current content stays visible; a small spinner appears beside the relevant tab trigger/label, including the top-nav overflow menu — but only after the refresh has actually been running for ~300ms, so ordinary 10-second polls do not blink. Existing panel-level feedback (inline notices, per-panel spinners, error banners) remains.
3. **Make loading state project-scoped and race-free.** State is keyed by `(stable host id, project path, tab)` and is reset immediately on project switch. Late callbacks/responses from the old project — **and late responses from an earlier visit to the same project** — are ignored. A visible spinner always means "loading for the currently selected project."

## 3. Non-goals / explicit exclusions

- No global fetch/XHR/SSE interception to infer loading (fragile, races with cancellation, and cannot distinguish first load from refresh). Loading is emitted explicitly via the shared hook (§5.5); no React context plumbing is introduced unless proven necessary.
- No repo-wide Suspense/React `use()` refactor.
- No new loading UI for surfaces that already own specialized loading feedback: **editor (`FileEditor`), preview viewers (`PdfViewer`/`MarkdownViewer`/office/etc. via `PreviewSurface`/`PreviewTabPage`), browser (`BrowserPanel`), terminal (`TerminalPanel`)**. Those keep their existing loading states; this design only covers tab-shell and panel chrome.
- No changes to server APIs or event contracts; this is purely presentational state management in the client.
- **The top-nav's independent Git badge poll (`TopTabs.tsx` `fetchCounts`/`git_status` listener) must not drive the tab loading indicator** (§5.5, §7.12).
- Desktop requires no additional work beyond rebuilding/reloading the shared bundle.

## 4. Existing findings (current codebase)

| Area | File / symbol | Relevant detail |
| --- | --- | --- |
| Top-nav tabs | `web/src/components/Layout/TopTabs.tsx` `mainTabs` (`:24-31`) | Main tabs: `sessions, files, git, cron, assets, settings`. Hosts the overflow menu. |
| Top-nav Git badge poll | `TopTabs.tsx:81-112` | Own `setInterval(fetchCounts, 10000)` + `eventBus.on("git_status")` → badge counts only. **Independent of `GitPanel.load`; must not emit loading events.** |
| Overflow menu | `TopTabs.tsx:247-284` | Radix `Select` (`SelectContent`) rendered in a **portal** — indicators here are easy to forget and must be queried via `document.body`. |
| Session sub-tabs | `SessionSubTabs.tsx` `subTabs` (`:6-12`) | Sub-tabs: `chat, agents, changes, logs, status, preview`. |
| Main tab shell | `web/src/components/App.tsx` | `TabsContent` `forceMount` for `files:1323, git:1421, cron:1424, assets:1427, settings:1430, sessions:1434`; natural home for lifted loading state. |
| GitPanel | `Git/GitPanel.tsx` | `load` `useCallback` `:244`; `REFRESH_INTERVAL = 10000` `:35`; `refreshing:197`; `workspace:154`. 10s background poll → refresh path. |
| FileTree | `Files/FileTree.tsx` | Root `loading:800`; `refresh` `useCallback:1003`; root fetch already uses `AbortController` (`controller.signal.aborted:991`); `refreshKey:806`. |
| CronPanel | `Cron/CronPanel.tsx` | `loading:18`; `loadJobs`/`loadTargets`; interval `:76`. |
| AssetsPanel | `Assets/AssetsPanel.tsx` | `loading:107`; **`loadGeneration = useRef(0)` `:113`** — existing per-request generation precedent; `loadFiles:139`. |
| ChangesPanel (sub-tab) | `Changes/ChangesPanel.tsx` | `loading:28`; `refresh:32`. |
| Reusable spinner semantics | `web/src/components/Browser/LoadingSpinner.tsx` | `Loader2` + `animate-spin` + `motion-reduce` — the visual/semantic reference for all new indicators. |
| Mount behavior | Panels rendered with `forceMount` | Mounted across tab switches, so local flags and subscriptions survive; callbacks/state need project-aware keys, a request generation, and explicit abort/cleanup. |

## 5. Design

### 5.1 Key identity (`originKey`)

Every load is stamped with a single string key:

```ts
originKey = `${hostId}\u0000${projectPath}\u0000${tabKey}`
```

- **`hostId`** — the **stable host identifier** for the surface: `projectState.activeProject?.host` for main tabs, `resolveSessionHost(projectState, sessionId)` for session sub-tabs, `""` for local. It is the same identifier the app already uses to route API calls — **never a display label** — so it is stable across renders.
- **`projectPath`** — the canonical project path as stored (`activeProject.path` / the session tab's `projectPath`), not a basename or label.
- **`tabKey`** — a main tab id (e.g. `"git"`), or `` `${sessionId}:${subTabId}` `` for a session sub-tab (e.g. `"ses_x:changes"`) so two sessions in one project never share a flag.

`\u0000` separators make the tuple unambiguous. **The same project path on two different hosts yields two different `hostId`s and therefore two different keys — they never collide.** The store is keyed by `tabKey`; each event carries its full `originKey` so stale keys can be detected (§5.4).

### 5.2 Request event contract (`LoadRequestEvent`)

Panels no longer emit a phase (`initial|refresh|idle`). They emit an explicit **request event** describing what happened to one request:

```ts
type LoadStatus = "start" | "success" | "empty" | "error";

interface LoadRequestEvent {
  originKey: string;     // full (host, project, tab) key the request was started for
  generation: number;    // per-panel request generation that issued this event
  status: LoadStatus;
  message?: string;      // human-readable error text when status === "error"
  retry?: () => void;    // re-runs the failed request when status === "error"
}
```

- `start` — a request began for `originKey` under `generation`.
- `success` / `empty` — it completed with content / with a confirmed-empty result.
- `error` — it failed; `message` describes it, `retry` (optional) re-runs it.

**App derives `initial` vs `refresh` from the event plus its own store context** (§5.3). Panels never decide which kind of load it is; they only report what happened. `message`/`retry` are meaningful only for `error` and are consumed only on the initial path (§5.3); refresh errors surface through the panel's existing inline error UI.

### 5.3 Store phase model and derivation

Loading state lives in `App.tsx` as an explicit, key-scoped store. It is a `Map<TabKey, TabLoadingEntry>` owned by the shared hook module (§5.5):

```ts
type TabPhase = "idle" | "initial" | "refresh" | "error";

interface TabLoadingEntry {
  phase: TabPhase;                                   // initial = blocking; refresh = non-blocking
  generation: number;                                // generation of the accepted in-flight request
  resolved: boolean;                                 // a success/empty has landed for this key since the map was last cleared
  error?: string;                                    // set when phase === "error"
  retry?: () => void;                                // set when phase === "error"
  refreshTimer?: ReturnType<typeof setTimeout>;      // 300ms delayed-indicator arm — internal, not rendered
}
```

Event → phase derivation (applied only after the staleness checks in §5.4):

| Event (current, non-stale) | Condition | Resulting phase | UI |
| --- | --- | --- | --- |
| `start` | `!entry.resolved` (no prior success/empty for this key) | `initial` | Blocking `TabLoadingOverlay` with spinner + `aria-busy`. |
| `start` | `entry.resolved` | arm `refreshTimer` (300ms); phase stays `idle` until it fires, then → `refresh` | No indicator for the first ~300ms; then spinner beside the tab. |
| `success` | — | `idle`, `resolved = true` | Content shown; `refreshTimer` disarmed; error cleared. |
| `empty` | — | `idle`, `resolved = true` | Empty state shown; `refreshTimer` disarmed. |
| `error` | was `initial` (`!entry.resolved`) | `error` | **Error/retry overlay, no spinner.** `message`/`retry` rendered. |
| `error` | was a refresh (`entry.resolved`) | `idle` | **Content retained, `refreshTimer` disarmed, spinner cleared → idle/non-loading.** The panel's existing inline error UI shows the failure. |

Notes:

- `initial` vs `refresh` is derived from `entry.resolved`, not from the event — satisfying "App derives initial vs refresh from the event/context."
- Because the store **clears on project switch** (§5.4), `resolved` resets, so the first load of a newly selected project's tab is always `initial` (blocking). Refreshes occur only after a first success within a project (polls, git operations, manual refresh).
- `refreshTimer` is an internal pending arm, **not** a render phase: while armed the surface renders `idle` (content, no spinner). If any terminal event (`success`/`empty`/`error`) arrives before it fires, it is disarmed and the spinner never appears.

### 5.4 Stale rejection: key + generation + abort

Three independent guards reject late work:

1. **Key guard (old project).** On event apply, if `event.originKey !== expectedOriginKey(tabKey)` for the current `(host, project)` the event is **dropped**. On project switch the entire map is **cleared immediately** (finding: no "emit idle for old key"), so no old-key entry or indicator survives the switch frame; any late old-key event is ignored by the originKey mismatch. *Panels do not emit `idle` for the previous key — cleanup is the map clear plus this key check.*
2. **Generation guard (A→B→A same key).** Each panel runs loads through `useKeyedLoad` (§5.5), which holds a monotonically increasing `generation`. The generation is **bumped on every new run, on every `originKey` change, and on unmount**. A terminal event is applied only if `entry` exists **and** `event.generation === entry.generation`; otherwise it is dropped. Returning to A issues a fresh generation, so a response from the *first* visit to A — whose `originKey` matches again — is rejected because its generation is stale. The same generation check runs **inside the panel before mutating panel data/state and before emitting**, so a stale response can corrupt neither the panel's content nor the store.
3. **Abort (cancellation).** `useKeyedLoad` owns one `AbortController` per in-flight request; it is aborted on re-run, on `originKey` change, and on unmount, and its `signal` is passed to the fetch. Abort is the fast path; the generation guard is the correctness backstop for already-resolved promises and non-abortable work.

This applies to **`GitPanel.load`** and **`FileTree.refresh`** (plus the initial root load) explicitly, and to every other wired panel through the same hook.

### 5.5 Shared hook module — `useKeyedLoad`

One shared module, `web/src/hooks/useKeyedLoad.ts`, centralizes all four cross-cutting concerns so **no panel and no App component reimplements them**:

- **`useKeyedLoad` (panel-side hook)** — returns a `run(fn)` helper. Each `run` aborts the previous controller, increments `generation`, emits `{ originKey, generation, status: "start" }`, invokes `fn({ signal, generation })`, and on completion **checks generation + originKey before applying panel state and before emitting** `success|empty|error`. It bumps generation and aborts on `originKey` change and on unmount.
- **`useTabLoadingStore` (App-side store, same module)** — applies `LoadRequestEvent`s per §5.3/§5.4: key guard, generation guard, `initial`/`refresh` derivation, and the **~300ms delayed-refresh indication** (`REFRESH_INDICATOR_DELAY_MS = 300`), clearing promptly on completion.
- Exports the `LoadRequestEvent`/`LoadStatus`/`TabPhase` types and `REFRESH_INDICATOR_DELAY_MS`.

**Delayed refresh (flicker avoidance):** a refresh `start` on an already-resolved key arms a 300ms timer; the indicator renders only if the request is still running when it fires, and clears immediately on any terminal event. A fast request (the common 10s Git/Cron poll) completes in well under 300ms and **never shows a spinner**, so polling does not blink constantly. A slow refresh shows the spinner for exactly as long as it is actually blocked past the threshold.

**App wiring stays minimal** — App only: (a) computes each surface's `originKey` (§5.1), (b) holds the store, (c) renders `TabLoadingOverlay`/`TabLoadingIndicator` from `phase`, (d) passes `loadingKey` + `onLoadingEvent` to wired panels, and (e) clears the store on project switch. No per-tab loading logic, no timers, and no interception live in App. (Finding: avoid global API interception/context unless proven necessary — this design does not need them.)

### 5.6 Blocking initial overlay — `TabLoadingOverlay`

New shared component `web/src/components/common/TabLoadingOverlay.tsx`:

- Props: `active: boolean`, `label?: string` (default `"Loading"`), `error?: string`, `onRetry?: () => void`.
- Renders a centered overlay covering the tab's content area: `role="status"`, `aria-busy="true"`, `aria-live="polite"`, a `Loader2` spinner with `animate-spin` and `motion-reduce:animate-none` (matching `LoadingSpinner.tsx`), and visible "Loading" text (spinner alone is insufficient for accessibility and motion-reduced users).
- Rendered when `phase === "initial"` (`active`). When `phase === "error"` it renders the error state (message + Retry via `onRetry`, wired to the event's `retry`) with **no spinner** — the loading itself cleared; only the handled failure remains.
- The parent content area also receives `aria-busy` while the overlay is active.

Applied by `App.tsx` around each blockable tab's content. Unblockable surfaces (editor, viewers, browser, terminal) are excluded per §3.

### 5.7 Non-blocking indicator — `TabLoadingIndicator`

New shared component `web/src/components/common/TabLoadingIndicator.tsx`:

- Props: `active: boolean` (renders iff `phase === "refresh"`), `label?: string` (e.g. `"Loading Git"`).
- Small inline spinner (same `Loader2`/`animate-spin`/`motion-reduce` recipe) adjacent to a tab trigger's label.
- Used in: `TopTabs.tsx` (each top-nav trigger whose phase is `refresh`), the top-nav **overflow Select menu** (the row label for an overflowing tab in `refresh` phase — a portal-rendered `SelectItem`), and `SessionSubTabs.tsx` (sub-tab label in `refresh` phase).
- Triggers keep `aria-busy` (or `aria-describedby` pointing at the spinner's live region) while active.
- Existing panel-level feedback (GitPanel notices, error banners, per-panel skeletons) is untouched.

### 5.8 Panel integration

Wired panels receive an explicit, optional pair of props instead of owning display-only flags:

```ts
interface TabLoadingProps {
  loadingKey?: string;                                  // current originKey from App (§5.1)
  onLoadingEvent?: (event: LoadRequestEvent) => void;   // emits §5.2 events
}
```

- `GitPanel.load` runs through `useKeyedLoad.run`, classifying `success`/`empty`/`error` (the 10s `background` poll takes the refresh path via §5.3). It keeps its local `refreshing` flag for its own refresh button; the tab indicator is event-driven.
- `FileTree.refresh` and the initial root load run through `useKeyedLoad.run` (generation + abort). Per-node child-expansion loads keep their existing `AbortController` and are **not** separately event-wired (they are part of the files tab's single load).
- `CronPanel`, `AssetsPanel`, `ChangesPanel` map their fetch cycles through the same hook. (`AssetsPanel` already keeps a `loadGeneration` ref — the hook generalizes that pattern.)
- On `loadingKey` change or unmount, a panel's hook **bumps generation and aborts**; it emits **no event for the old key** (no `idle`-for-old-key cleanup — the store map is cleared and old-key events are ignored per §5.4).
- Panels classify an empty result as `empty` (e.g. zero root nodes / zero files / zero changes) so the overlay clears to the empty state.
- Panels that already show a first-load skeleton/empty state should prefer the shared overlay for consistency, but may keep richer specialized loading UI where one exists (overlay and panel UI must not double-render the spinner).
- The **TopTabs Git badge poll, `SyncStatusWidget`, `PortMapsWidget`, and terminal-count listeners do not emit `LoadRequestEvent`s** and therefore never drive the indicator.

### 5.9 Why this shape (and what was rejected)

- **Shared `useKeyedLoad` module + lifted store:** centralizes generation, abort, stale rejection, and delayed refresh in one place; testable, key-aware by construction, colocated with the tab shell. ✔ chosen.
- **Key-only staleness check (no generation):** rejected — A→B→A re-matches the key, so a first-visit response would be accepted. Generation + abort are required (§5.4).
- **Global API interception:** rejected — cannot distinguish initial vs. refresh, cannot attribute responses to `(host, project, tab)`, races with cancellation, and would still need keying. Out of scope per §3.
- **Suspense refactor:** rejected — large blast radius across panels and `forceMount` lifecycles; the blocking overlay delivers the user-visible benefit without it.

## 6. Scope

**Wired — main top tabs (4):**

| `tabId` | Panel | Async work |
| --- | --- | --- |
| `files` | `FileTree` | initial root tree load + `refresh` |
| `git` | `GitPanel` | `load` + 10s background poll |
| `cron` | `CronPanel` | `loadJobs`/`loadTargets` + interval refresh |
| `assets` | `AssetsPanel` | `loadFiles` |

**Wired — session sub-tabs (1):**

| `subTabId` | Panel | Async work |
| --- | --- | --- |
| `changes` | `ChangesPanel` | session changes refresh |

**Explicitly deferred / not wired (retain existing feedback; may adopt `useKeyedLoad` later without a design change):**

- Main tabs: `sessions` (container — its content is the already-loaded session list; its sub-tabs are handled individually), `settings` (`SettingsPanel` = synchronous forms, no async initial-load gate).
- Session sub-tabs: `chat` (`ChatPanel` already owns transcript loading + load-failure retry UX — `ChatPanel.loadFailure`), `agents` (`AgentsPanel`), `logs` (`LogPanel`), `status` (`StatusPanel`), `preview` (specialized viewer loading).

**Retained specialized loading UI (excluded surfaces):** editor (`FileEditor`), preview viewers (`PreviewSurface`/`PreviewTabPage` + `PdfViewer`/`MarkdownViewer`/office), browser (`BrowserPanel`), terminal (`TerminalPanel`).

**In scope (supporting):** the tab shell (`App.tsx`) state, `TabLoadingOverlay`, `TabLoadingIndicator`, the shared `useKeyedLoad` module, the top-nav overflow-menu indicator, and project-switch correctness for **remote and local projects, desktop and web** (shared bundle ⇒ one implementation).

**Out of scope:** server-side changes; loading-inference middleware/interceptors; the four excluded surfaces above; any refactor beyond the enumerated wiring.

## 7. Test plan

Automated tests (vitest + testing-library, `web/src`):

1. **Initial blocker:** first load for a key renders `TabLoadingOverlay` with `aria-busy`, spinner, and "Loading" text; content is blocked; overlay clears on success.
2. **Non-blocking refresh:** with resolved content present, a refresh keeps content visible and renders `TabLoadingIndicator` beside the trigger/label (no overlay) — after the 300ms delay (see 11).
3. **Project switch / stale old-key event:** switching project clears the store immediately; a late event emitted with the old `originKey` is ignored (no spinner, no old-project panel content under the new key). No `idle`-for-old-key event is required or emitted.
4. **A→B→A late response rejected:** start a load for A (`gen 1`), switch to B (map cleared, panel bumps generation + aborts), return to A (`gen 2`), then deliver A's `gen 1` terminal event → dropped (no phase change, no indicator), and the panel did not mutate its data from `gen 1`. Mutation: removing the generation check must fail this test.
5. **Same path, different host:** two surfaces with identical `projectPath` but different `hostId` never share loading state (a `refresh`/`initial` under host1 leaves host2's entry untouched).
6. **Unmount mid-load:** unmounting a wired panel mid-request aborts the `AbortController`; no state mutation and no `LoadRequestEvent` is produced after unmount.
7. **Force-mounted hidden tab:** `git` is `forceMount`ed but unselected and performs its initial load; selecting it renders the overlay if still in flight, content after `success`, no double-load, and no stuck overlay after completion.
8. **Refresh error preserves content:** a refresh that fails keeps content visible, returns to `idle` (no overlay, no spinner), and surfaces the failure via the panel's existing inline error UI.
9. **Error and empty completion:** an initial error shows the error/retry overlay with **no spinner**; an empty result clears the overlay to the empty state (no permanent spinner).
10. **Top-nav overflow indicator:** a refresh-phase tab shows the indicator inside the Radix `Select` overflow menu — assert against `document.body` (portal), not the container.
11. **Delayed refresh / no poll flicker:** a refresh completing in <300ms renders **no** indicator; one still running ≥300ms renders it; completion clears it promptly. The ordinary 10s Git poll never blinks.
12. **Independent Git badge poll does not drive the indicator:** drive `TopTabs`' `fetchCounts`/`git_status` badge path and assert **no** loading phase change / no indicator.
13. **Desktop/shared-bundle build:** web bundle builds/typechecks cleanly; desktop embed path compiles (shared `web/dist` — assert build/typecheck, no separate desktop test surface).
14. **Accessibility:** `motion-reduce` disables spin animation; overlay/indicator expose busy semantics (`aria-busy`, `role="status"`).

Each state-transition test should be mutation-verified: reversing the key-check, the generation-check, the `resolved`-derivation, or the initial→completion rule must fail the corresponding test.

## 8. Acceptance criteria

- **Initial load blocks current-project tab content:** selecting a tab whose first load for the current project is pending shows the centered accessible overlay; it never spins forever after success, handled error, or empty result.
- **Background refresh is non-blocking with a spinner beside the tab:** existing content never blanks; the correct trigger (including the overflow-menu row) shows a small spinner, and only after ~300ms of actual work — fast polls do not blink.
- **Errors use existing/retry UX:** initial errors transition to the error/retry overlay with no spinner; refresh errors retain content and return to idle/non-loading, surfacing through the panel's existing error UI.
- **Stale responses are rejected:** after any project switch, zero indicators or panel content from the previous project are visible; after an A→B→A round trip, a first-visit response is rejected by generation even though the key matches again.
- **Key identity holds:** the same path on two hosts never collides.
- **Unrelated surfaces unchanged:** editor/viewer/browser/terminal loading UI is untouched; the design introduces no refactor beyond the enumerated wiring.
- **Shared bundle:** web typecheck/build and the listed test suites pass; desktop and web share the embedded SPA and desktop needs only a rebuild.

## 9. Open questions

None — design approved 2026-09-24 and revised per advisor review the same day as specified above.
