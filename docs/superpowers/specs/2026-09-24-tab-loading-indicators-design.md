---
type: Design
title: Tab Loading Indicators — Design Spec
description: 'Design spec: accessible blocking initial-load overlays and non-blocking refresh spinners for web tab shells, project-scoped by (host, project, tab).'
tags:
  - design
  - spec
  - web
  - frontend
  - loading
  - tabs
  - superpowers
timestamp: 2026-09-24T07:22:29Z
---
# Tab Loading Indicators — Design Spec

**Status:** Approved (2026-09-24)
**Scope:** ocode web frontend (`web/src`), shared by desktop via the embedded React SPA
**Non-goals:** no separate desktop frontend implementation; no global API interception; no large Suspense refactor; no source changes in this document (spec only)

---

## 1. Problem

The web UI (and the desktop shell, which embeds the same React SPA bundle) shows no visible loading feedback for tab content. When a tab's data is fetched asynchronously, the user sees either stale content or an apparently-idle panel, and cannot tell:

- whether a tab's **first load** for the current project is still in flight,
- whether a **background refresh** is running behind visible content,
- whether a spinner (where panels happen to have one) refers to the **currently selected project** or to a previous project.

Because panels use `forceMount` and callbacks can land late, loading flags can also go stale across project switches — a spinner or cached panel content from project A can appear while project B is selected.

## 2. Goals

1. **Block initial loads visibly.** While a tab's first load for the current project is unresolved, the tab's content is blocked by an accessible centered loading overlay (`aria-busy`, spinner, "Loading" text). The overlay clears on success, on a handled error, or on an empty result.
2. **Keep background refreshes non-blocking.** Current content stays visible; a small spinner appears beside the relevant tab trigger/label, including the top-nav overflow menu. Existing panel-level feedback (inline notices, per-panel spinners, error banners) remains.
3. **Make loading state project-scoped.** State is keyed by `(host, project path, tab)` and is reset/rekeyed immediately on project switch. Late callbacks/responses from the old project are ignored; project-specific panel state must not be displayed under a new project. A visible spinner always means "loading for the currently selected project."

## 3. Non-goals / explicit exclusions

- No global fetch/XHR/SSE interception to infer loading (fragile, races with cancellation, and cannot distinguish first load from refresh).
- No repo-wide Suspense/React `use()` refactor.
- No new loading UI for surfaces that already own specialized loading feedback: **editor, preview viewers, browser, terminal**. Those keep their existing loading states; this design only covers tab-shell and panel chrome.
- No changes to server APIs or event contracts; this is purely presentational state management in the client.
- Desktop requires no additional work beyond rebuilding/reloading the shared bundle.

## 4. Existing findings (current codebase)

| Area | File / symbol | Relevant detail |
| --- | --- | --- |
| Top-nav tabs | `web/src/components/Layout/TopTabs.tsx` | Main tab triggers; hosts the overflow menu that must also show a refresh spinner. |
| Session sub-tabs | `web/src/components/Layout/SessionSubTabs.tsx` | Per-session sub-tab row (chat/terminal/preview/etc.). |
| Main tab shell | `web/src/components/App.tsx` | Mounts tab content and is the natural home for lifted loading state. |
| Panel local flags | `GitPanel` (`refreshing`, `workspace`), `FileTree` (`loading`), `ChangesPanel`, `CronPanel`, `AssetsPanel` | Panels already track loading internally but per-mount, not per-project. |
| Reusable spinner semantics | `web/src/components/Browser/LoadingSpinner.tsx` | `Loader2` + `animate-spin` + `motion-reduce` — the visual/semantic reference for all new indicators. |
| Mount behavior | Panels rendered with `forceMount` | Mounted across tab switches, so local flags and subscriptions survive; callbacks/state therefore need project-aware keys and explicit cleanup. |

## 5. Design

### 5.1 Loading state model

Loading state lives in `App.tsx` (lifted from panels) as an explicit, key-scoped store rather than scattered local flags:

```ts
type TabKey = string; // top-nav tab id, or `${sessionId}:${subTabId}` for session sub-tabs

interface TabLoadingState {
  phase: "idle" | "initial" | "refresh" | "error"; // initial = blocking; refresh = non-blocking
  host: string;                           // "" for local
  projectPath: string;
  startedAt: number;
  error?: string;                         // human-readable message when phase === "error"
}
```

Key: `${host}\u0000${projectPath}\u0000${tabKey}` — the `(host, project path, tab)` tuple required by the design. Session-scoped sub-tabs additionally embed the session id so two sessions in the same project do not share a flag.

Rules:

- **Initial phase** is entered when the tab's content for the current key has never resolved (or the key changed). The loading (spinner) completes — i.e. leaves `initial` — on success, on a handled error, or on a confirmed-empty result:
  - success → `idle` (content shown);
  - empty → `idle` (empty state shown);
  - handled error → `error` with `error` set: the spinner stops, and the overlay presents the error/retry state instead of loading. `error` is not a loading phase, so it satisfies "clear on handled error" while keeping the failure visible.
- **Refresh phase** is entered when a load starts while the same key already has resolved content. It returns to `idle` on completion of any kind; errors surface through the panel's existing error UI, not the overlay.
- A transition is applied **only if the event's key still equals the current key**. Callbacks carry their originating key; stale-key events are dropped.

### 5.2 Stale-callback and project-switch handling

- The active key is derived from the current `(host, project path)` plus the tab/sub-tab id. On project switch, the entire loading map is **reset immediately** (not awaited), so no indicator from the old project survives the switch frame.
- Panels emit through a callback shaped as `(tabKey, phase, originKey) => void`. `App` recomputes the current key from `(host, project path, tabKey)` and applies the transition only if it equals `originKey` — mismatches are dropped. This is the late-response rejection path.
- Because panels are `forceMount`, project-specific panel state (lists, counts, drafts) must either be keyed by project (re-rendered fresh per key) or cleared on key change. Rule: **no panel renders project-derived content under a key different from the key it was fetched for.** Panels keep their data in a `Map` keyed by project key, or reset on key change; the overlay covers any gap.
- The same key discipline applies to the refresh spinner: a spinner rendered for tab T means the *current* project's T is loading.

### 5.3 Blocking initial overlay — `TabLoadingOverlay`

New shared component `web/src/components/common/TabLoadingOverlay.tsx`:

- Props: `active: boolean`, `label?: string` (default `"Loading"`), `error?: string`, `onRetry?: () => void`.
- Renders a centered overlay covering the tab's content area: `role="status"`, `aria-busy="true"`, `aria-live="polite"`, a `Loader2` spinner with `animate-spin` and `motion-reduce:animate-none` (matching `LoadingSpinner.tsx` semantics), and visible "Loading" text (spinner alone is insufficient for accessibility and motion-reduced users).
- When `error` is set (phase `error` per §5.1), it renders the error state (message + Retry when `onRetry` provided) with **no spinner** — the loading itself has cleared; only the handled failure remains visible.
- The parent content area also receives `aria-busy` while the overlay is active so assistive tech treats the region as busy.

Applied by `App.tsx` around each blockable tab's content. Unblockable surfaces (editor, viewers, browser, terminal) are excluded per §3.

### 5.4 Non-blocking indicator — `TabLoadingIndicator`

New shared component `web/src/components/common/TabLoadingIndicator.tsx`:

- Props: `active: boolean`, `label?: string` (e.g. `"Loading Git"`).
- Small inline spinner (same `Loader2`/`animate-spin`/`motion-reduce` recipe) rendered adjacent to a tab trigger's label.
- Used in:
  - `TopTabs.tsx` — beside each top-nav tab trigger whose phase is `refresh`;
  - the top-nav **overflow menu** — beside the row label for an overflowing tab in `refresh` phase (overflow rows are the easy place for indicators to be forgotten);
  - `SessionSubTabs.tsx` — beside the sub-tab label in `refresh` phase.
- Triggers keep their `aria-busy` (or `aria-describedby` pointing at the spinner's live region) while active.
- Existing panel-level feedback (GitPanel notices, error banners, per-panel skeletons) is untouched.

### 5.5 Panel integration

Panels gain an explicit, optional callback instead of owning display-only flags:

```ts
interface TabLoadingProps {
  loadingKey?: string;                 // current project/session key from App
  onLoadingChange?: (tabKey: string, phase: "initial" | "refresh" | "idle", originKey: string) => void;
}
```

- `tabKey` identifies WHICH tab/sub-tab is loading; `originKey` is the full `(host, project path, tabKey)` key the emission came from, so `App` can reject stale events (§5.2).
- `GitPanel` maps `refreshing`/`workspace` transitions; `FileTree` maps `loading`; `ChangesPanel`, `CronPanel`, `AssetsPanel` map their fetch cycles.
- On mount and on `loadingKey` change, a panel resets its own fetch state (cancels/ignores in-flight results for the previous key) and emits `idle` for the old key — the cleanup that `forceMount` otherwise skips.
- First fetch for a key emits `initial`; subsequent fetches emit `refresh`.
- Panels that already show a first-load skeleton/empty state should prefer the shared overlay for consistency, but may keep richer specialized loading UI where one exists (the overlay and panel UI must not double-render the spinner).

### 5.6 Why this shape (and what was rejected)

- **Explicit callbacks + lifted state:** small, testable, key-aware by construction, and colocated with the tab shell that owns the triggers. ✔ chosen.
- **Global API interception:** rejected — cannot distinguish initial vs. refresh, cannot attribute responses to `(host, project, tab)`, races with cancellation, and would still need keying. Out of scope per §3.
- **Suspense refactor:** rejected — large blast radius across panels and `forceMount` lifecycles; the blocking overlay delivers the user-visible benefit without it.

## 6. Scope

**In scope**

- Main top-nav tabs (`TopTabs.tsx`) and per-session sub-tabs (`SessionSubTabs.tsx`) **where real async work exists**.
- The tab shell (`App.tsx`) state, `TabLoadingOverlay`, `TabLoadingIndicator`.
- The listed panels' `onLoadingChange` wiring and key-scoped cleanup: GitPanel, FileTree, ChangesPanel, CronPanel, AssetsPanel.
- Top-nav overflow-menu indicator.
- Project-switch correctness for **remote and local projects, desktop and web** (shared bundle ⇒ one implementation).

**Out of scope**

- Editor, preview viewers (PDF/office/markdown), browser, and terminal loading UI — retain existing specialized feedback.
- Server-side changes.
- Any loading inference middleware/interceptors.

## 7. Test plan

Automated tests (vitest + testing-library, `web/src`):

1. **Initial blocker:** first load for a key renders `TabLoadingOverlay` with `aria-busy`, spinner, and "Loading" text; content is blocked; overlay clears on success.
2. **Non-blocking refresh:** with resolved content present, a refresh keeps content visible and renders `TabLoadingIndicator` beside the trigger/label (no overlay).
3. **Project switch / stale callback:** switching project resets loading state immediately; a late `onLoadingChange` emitted with the old project key is ignored (no spinner, no old-project panel content rendered under the new key).
4. **Error and empty completion:** handled error stops the spinner and shows the error state (with Retry when provided); an empty result also clears the overlay (no permanent spinner).
5. **Top-nav overflow indicator:** a refresh-phase tab inside the overflow menu shows the indicator on its menu row.
6. **Desktop/shared-bundle build:** the web bundle builds cleanly with the new components and the desktop embed path compiles (shared `web/dist` embed — assert build/typecheck, no separate desktop test surface).
7. **Accessibility:** `motion-reduce` disables spin animation; overlay/indicator expose busy semantics (`aria-busy`, `role="status"`).

Each state-transition test should be mutation-verified: reversing the key-check or the initial→completion rule must fail the test.

## 8. Acceptance criteria

- Selecting a tab whose first load for the current project is pending shows the centered accessible overlay; it never spins forever after success, handled error, or empty result.
- Background refreshes never blank existing content; the correct trigger (including overflow-menu rows) shows a small spinner.
- After any project switch, zero loading indicators or panel content from the previous project are visible, and out-of-order responses cannot reintroduce them.
- Editor/viewer/browser/terminal loading UI is unchanged.
- Web typecheck/build and the listed test suites pass; desktop needs only a rebuild to pick the changes up.

## 9. Open questions

None — design approved 2026-09-24 as specified above.