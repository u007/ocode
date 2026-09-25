---
type: Gotcha
title: Desktop quit guard for unsaved drafts (pending + failed) + sticky-port fallback no longer re-saves
description: Desktop unsaved-draft quit guard (two-state pending/failed, bounded debounce) and sticky-port drift no longer re-saves — 2026-09-25 fixes, follow-up hardening.
tags:
  - desktop
  - wails
  - quit-guard
  - localstorage
  - debounce
  - port
  - editor-drafts
timestamp: 2026-09-25T02:23:28Z
---
# Desktop: unsaved-draft quit guard & sticky-port fallback no longer re-saves (2026-09-25)

Two related desktop-shell fixes, both about **not destroying state the webview owns**. The quit guard was hardened twice on 2026-09-25: the original fix blocked quit only when a localStorage draft write *failed*; the follow-up (documented here) broadened it to also block while a debounced write is merely *pending*, and bounded the debounce so "pending" can never last more than ~1s.

## 1. Unsaved editor drafts block quit — two states, `pending` and `failed`

**Problem:** the desktop app could quit (⌘Q, tray, red-X close) while the SPA held editor drafts that were not in localStorage — either because the write failed (quota/availability) or because the write was still *pending* in the debounce window. The original guard only armed on a **failed** write, but the draft write is **debounced**, so a quit before the write happened lost the edits with nobody noticing.

**How the guard works:**

- **Draft persistence**: `web/src/components/Files/editorTabsPersistence.ts` — drafts keyed `ocode.editor.draft.<tabId>`. `saveEditorDraft` returns `boolean` (false on quota/storage failure) instead of only logging, so callers can react.
- **Native state**: `internal/desktop/quit_guard.go` — `QuitGuard{Block(reason), Clear(), Blocked() (bool, string), HandleRawMessage(msg) bool}`.
- **Wire protocol** (SPA → native, via the minimal `window._wails.invoke` bridge — the same channel as `wails:runtime:ready`; non-`wails:` messages reach the handler): `ocode:quit-guard:blocked[:reason]` and `ocode:quit-guard:clear`.
- **Feed**: `cmd/ocode-desktop/main.go` — `application.Options.RawMessageHandler` routes messages into the guard.
- **Quit refusal**: `ShouldQuit` returns `false` while blocked (covers ⌘Q / tray / menu / programmatic quit). Red-X close is cancelled by a `window.RegisterHook(events.Common.WindowClosing, …)` that calls `event.Cancel()` while blocked.

### KEY WAILS FACT (v3 beta.12): use `RegisterHook`, not `OnWindowEvent`, to cancel close

`WebviewWindow.HandleWindowEvent` runs `RegisterHook` hooks **before** listeners and **returns early when the event is cancelled**, so Wails' internal `WindowClosing` listener that destroys the window never runs. An `OnWindowEvent(WindowClosing)` listener runs **too late** — the window is already destroyed by the time it fires. This is why the close-cancel must be a `RegisterHook` hook.

### The two guard states (`web/src/lib/editorDraftGuard.ts`)

| State | Meaning | Armed by | User-visible signal |
|---|---|---|---|
| `pending` | tab holds edits not written to localStorage yet (debounced write still in flight) | `noteDraftPersistPending` on **every keystroke** from `useEditorTabs.handleEditorChange` | **deliberately silent** — a toast would fire on every character |
| `failed` | the localStorage write itself failed (`saveEditorDraft` returned false) | `noteDraftPersistFailure` from `flushDraft` | sticky `ActionErrorToast` via `reportActionErrorMessage` |

- Either state blocks quit: `draftPersistenceBlocked()` is `failed.size > 0 || pending.size > 0`.
- A single `syncNative()` helper sends `ocode:quit-guard:blocked:<reason>` / `ocode:quit-guard:clear` **only on transitions** (it latches on `nativeBlocked`), so per-keystroke arming never spams the bridge.
- `noteDraftPersistFailure` moves a tab from `pending` → `failed`; `noteDraftPersistSuccess` clears both; `reconcileDraftGuard(liveDirtyTabIds)` drops entries for tabs that are no longer dirty or were closed and syncs native (fires `ocode:quit-guard:clear` when the sets empty).
- Message precedence: failure message (with file list) wins over pending; pending reads e.g. *"An unsaved edit hasn't been written to local storage yet: a.txt. ocode won't quit until it is — wait a moment, or use 'Quit anyway'."*

### UX while blocked

- `confirmQuit` blocked branch (`cmd/ocode-desktop/main.go`): state-neutral preamble — **"Some editor changes aren't in the local draft yet, so ocode won't quit. Wait a moment for them to be written, or save the file(s) to disk."** — then the web-supplied reason is appended (`message += "\n\n" + reason`). The preamble is state-neutral because Go cannot tell `pending` from `failed`; the web side owns the specific reason. Buttons: **"Keep editing"** (cancel/default) / **"Quit anyway"** (clears the guard, then quits) — the explicit escape hatch.
- Rapid double-⌘Q bypass is **disabled while blocked**.
- `notifyQuitBlocked` ExecJS-dispatches `ocode:quit-blocked` back into the web UI; `installQuitBlockedListener()` re-raises the current message as a toast.

### Bounded draft debounce (`web/src/hooks/useEditorTabs.ts`)

The guard is only meaningful if "pending" resolves promptly, so the debounce is **bounded** instead of a plain trailing timer:

```
delay = max(DRAFT_WRITE_MIN_DELAY_MS,            // 50ms
            min(DRAFT_WRITE_DEBOUNCE_MS,         // 500ms
                DRAFT_WRITE_MAX_WAIT_MS - elapsed))  // 1000ms - elapsed
```

- `elapsed` is measured from `draftPendingSince` — when the current unpersisted batch started (first keystroke after a write).
- Result: a write lands **at most ~1s after the first unpersisted keystroke**, and **at least 50ms after the last one**.
- The old plain trailing 500ms debounce (timer reset on every keystroke) was **deferrable indefinitely by continuous typing**, which left an entire unpersisted tail — the guard would have had nothing to report until typing stopped.

**Stale-ref hazard:** a short-delay (50ms) flush can fire *before* the effect that syncs `editorTabsRef` runs, and a stale read there would write an older draft, dropping the newest keystrokes. So `handleEditorChange` mirrors the new content into `editorTabsRef.current` **synchronously** (shared `applyEdit` mapper) in addition to `setEditorTabs`, before arming the guard and scheduling the timer.

A `beforeunload` listener flushes every pending draft synchronously on the way out (reload/quit inside the debounce window).

### Native/web wiring

- `web/src/App.tsx` mounts `installQuitBlockedListener()`.
- `useEditorTabs` wires flush (write success/failure → `noteDraftPersistSuccess`/`noteDraftPersistFailure`), save, reload, plus a reconcile effect.

### Tests (all mutation-verified — reverting the wiring makes them fail)

- `internal/desktop/quit_guard_test.go` — round-trip, raw-message handling, reason containing colons.
- `web/src/lib/editorDraftGuard.test.ts` — 4 pending-state tests: blocks silently (no toast), clears on success, coexists with a recovered failure (`failed` + `pending` both tracked), `reconcileDraftGuard` drops pending, distinct pending message. Plus the original failure-state suite.
- `web/src/hooks/useEditorTabs.test.ts`:
  - `describe("useEditorTabs draft-persistence guard")` — the write-failure case.
  - `describe("useEditorTabs in-memory draft guard")` — fake-timer tests: the guard arms while edits are in memory and clears once the write lands; continuous typing (an edit every 400ms for 2s) still produces a mid-stream write and the final write carries the newest content.

## 2. Sticky-port fallback no longer re-saves the drifted port

**How the sticky port works:** `internal/desktop/boot.go` `StartServer` binds `127.0.0.1:<savedPort>` (from `loadSavedPort()`, `~/.config/opencode/desktop-port`). `server.Listen()` (`internal/server/server.go`, `maxPortAttempts = 20`) walks forward up to 20 ports on `EADDRINUSE` and only errors when all 20 are taken (then `:0`) — so a taken saved port yields `savedPort+1`.

**The bug:** the bound port was **always** re-saved. A drifted run (`savedPort+1`) then persisted as the new sticky port, moving the webview's `http://127.0.0.1:PORT` **localStorage origin** — permanently orphaning the previous origin's UI state (terminal/editor/session tabs, unsaved editor drafts).

**The fix:** `saveBoundPort(addr)` now runs only when `savedPort == 0 || boundPort == savedPort` (boot.go). A drifted run uses the temporary origin for that run only; its UI state is written there and lost on next launch — deliberately, rather than migrating the sticky origin.

**Test:** `internal/desktop/boot_test.go` `TestStartServerFallbackDoesNotOverwriteSavedPort`.

## Remaining loss paths (known, accepted)

- A process that dies with **no quit handshake** (crash, `kill -9`) can still lose the last ≤1s of edits — the bounded debounce is the floor; nothing survives an abrupt kill.
- A **drifted run's** UI state is written to the temporary port origin and lost on next launch (sticky-port fix, by design).

## Where this is also documented

- Design doc (outside the bundle): `docs/superpowers/specs/2026-09-25-desktop-quit-guard-design.md`.
- Skill: `skills/ocode-desktop/SKILL.md` gotchas **5** (sticky port drift) and **16** (unsaved-draft quit guard), plus the embedded copy under `cmd/ocode-desktop/embedded-assets/skills/ocode-desktop/SKILL.md` — keep those in sync with this doc.
- `CHANGES.md` (2026-09-25 entries).
