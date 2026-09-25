# Desktop quit guard + sticky-port fallback fix — design

Date: 2026-09-25
Status: implemented (approved 2026-09-25)

## Problem

1. **Silent loss of unsaved editor drafts.** Editor tabs mirror unsaved edits
   into `localStorage` (`web/src/components/Files/editorTabsPersistence.ts`,
   key `ocode.editor.draft.<tabId>`). On a quota failure `saveEditorDraft` only
   logged the error and returned; `ShouldQuit` (`cmd/ocode-desktop/main.go`)
   always returned `true`, and `WindowClosing` quit unconditionally. A failed
   draft write was therefore invisible and quitting lost the edits.

2. **Sticky-port fallback silently moves the localStorage origin.** The
   webview's persisted UI state is origin-scoped to `http://127.0.0.1:PORT`,
   and `desktop-port` exists to keep that origin stable. But `server.Listen()`
   walks forward up to 20 ports on `EADDRINUSE`, and `StartServer` unconditionally
   re-saved the actually-bound port — so a conflict produced `savedPort+1`,
   permanently orphaning the previous origin's drafts/tabs.

## Design

### Native quit guard (Go)

- `internal/desktop/quit_guard.go`: a mutex-guarded `QuitGuard` with
  `Block(reason)`, `Clear()`, `Blocked() (bool, string)` and
  `HandleRawMessage(msg) bool`.
- `application.Options.RawMessageHandler` feeds the guard. Messages that do not
  start with `wails:` reach it; the SPA sends them through the already-working
  minimal bridge (`window._wails.invoke`), the same channel as
  `wails:runtime:ready`. Protocol:
  - `ocode:quit-guard:blocked[:reason]`
  - `ocode:quit-guard:clear`
- `ShouldQuit` returns `false` while blocked (cancels Cmd+Q, tray/menu Quit, and
  any programmatic `app.Quit()`).
- `window.RegisterHook(events.Common.WindowClosing, …)` calls `event.Cancel()`
  while blocked. Verified against Wails beta.12: hooks run **before** listeners
  and a cancelled event skips them entirely, so the internal listener that
  destroys the window never runs. A plain `OnWindowEvent` listener is too late
  (the window is already destroyed).
- `confirmQuit` grows a blocked branch: "Keep editing" (cancel/default) and
  "Quit anyway" (clears the guard, then quits). The rapid double-⌘Q bypass is
  disabled while blocked.
- `notifyQuitBlocked` dispatches `ocode:quit-blocked` via `ExecJS` so the web UI
  re-surfaces the reason.

### Web reporter

- `editorTabsPersistence.saveEditorDraft` returns `boolean`.
- The guard tracks **two** states, because "a write failed" is not the only way
  to lose work:
  - `pending` — the tab holds edits with no draft written yet (the debounced
    write is still in flight). Armed on every keystroke, silent (no toast).
  - `failed` — the localStorage write itself failed. Raises the existing sticky
    `ActionErrorToast`.
  Either state blocks quit.
- `web/src/lib/editorDraftGuard.ts` reports `blocked`/`clear` transitions to
  native through `invokeWails` (`web/src/lib/wails.ts`), only on transitions.
  `reconcileDraftGuard` drops entries for tabs no longer dirty, so
  close/discard/reload/file-delete cannot leave a stale block.
- **Bounded debounce.** The draft write was a plain trailing 500 ms debounce,
  which continuous typing pushes out forever — so an entire tail of edits could
  sit in memory with the guard none the wiser. `useEditorTabs` now clamps the
  delay into an absolute deadline: `max(50ms, min(500ms, 1000ms - elapsed))`
  since the batch started, so a write lands ≤1 s after the first unpersisted
  keystroke, and ≥50 ms after the last one.
- **No stale flush.** `handleEditorChange` mirrors the new content into
  `editorTabsRef` synchronously (the effect that syncs it may not have run when
  a short-delay flush fires; writing a stale draft would drop the newest
  keystrokes).
- `installQuitBlockedListener` (mounted in `App.tsx`) re-raises the reason when
  native refuses a quit.

### Sticky-port fix

`StartServer` saves the bound port only when it equals the saved port (or there
was none): `savedPort == 0 || boundPort == savedPort`. A drift (via the
walk-forward or the all-20-taken fallback) leaves `desktop-port` untouched, so
the origin returns on the next launch; only the drifted run's UI state is
temporary.

## Behaviour matrix

| Quit path | Guard clear | Guard blocked |
| --- | --- | --- |
| Cmd+Q (single) | confirm dialog | "Keep editing" / "Quit anyway" |
| Cmd+Q (double, <1.5 s) | immediate quit | confirm dialog (bypass disabled) |
| Tray / menu Quit | confirm dialog | same blocked dialog |
| Red-X window close | window closes → quit | close cancelled, window stays, toast re-raised |
| Programmatic `app.Quit()` | quits | cancelled |

## Residual risks

- Unpersisted edits are now bounded to ≤1 s instead of being deferrable
  indefinitely, and the guard is armed the instant a keystroke lands — so a quit
  inside the window is blocked rather than silently dropping the edit. The one
  remaining loss path is a process that dies without any quit handshake (crash,
  `kill -9`): the last ≤1 s of edits are still in memory only.
- A drifted run still writes its UI state to the temporary origin; that state is
  lost on the next launch (the previous origin's state is preserved).

## Tests

- `internal/desktop/quit_guard_test.go` — block/clear, raw-message handling,
  reason with colons.
- `internal/desktop/boot_test.go` — `TestStartServerFallbackDoesNotOverwriteSavedPort`.
- `web/src/lib/editorDraftGuard.test.ts` — reporter transitions, pending vs
  failed, reconcile, listener, no-bridge no-op.
- `web/src/hooks/useEditorTabs.test.ts` — a failing draft write blocks quit and a
  save clears it; the guard arms while edits are in memory; continuous typing
  cannot defer the write (and the final write carries the newest content).
