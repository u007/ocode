---
type: Concept
title: User-Message Jump (Alt+↑ / Alt+↓)
description: Alt+↑/Alt+↓ jump between the user's own messages in a chat transcript (TUI + web), with msg N/M readout; distinct from composer input history and the find bar.
tags:
  - chat
  - keyboard
  - tui
  - web
  - navigation
  - transcript
  - TUI-parity
timestamp: 2026-09-29T14:36:17Z
---
# User-Message Jump (Alt+↑ / Alt+↓)

Jump between **your own messages** in a chat transcript, with a `msg N/M` position readout. Shipped on both surfaces:

| Surface | Binding | Readout |
|---|---|---|
| TUI | `Alt+↑` / `Alt+↓` (aliases `ctrl+shift+up` / `ctrl+shift+down`) | `msg N/M` appended to the status bar's `leftStatus` |
| Web / desktop | `Alt+↑` / `Alt+↓` (window keydown listener in `ChatPanel.tsx`) | Floating pill (`data-testid="user-jump-indicator"`, `role="status"`, `aria-live="polite"`) above the scroll-to-bottom button |

The desktop app is a Wails webview of `web/dist`, so web and desktop are one React implementation — there is no separate desktop code path.

## Three similar-looking features, three different things

| | Walks what you… | Trigger | Count basis |
|---|---|---|---|
| **Input history** (`web-chat-input-history.md`) | **typed** (composer draft recall) | plain `↑`/`↓` in the composer | local per-tab list appended at submit |
| **Find bar** (`Ctrl+F`) | **matched** (keyword hits) | `Enter` / arrows inside the find input | server match list over the full transcript |
| **User-message jump** (this page) | **sent** (your prompts in the transcript) | `Alt+↑` / `Alt+↓` | server list of countable user messages |

Plain arrows are never touched by the jump: the web window listener ignores the keys when the event target is an `INPUT`/`TEXTAREA`/`SELECT`/contentEditable element (which also keeps the find bar's own arrow navigation intact), and the TUI binds only the alt/ctrl-shift combos in the composer key switch.

## Shared counting rule

A countable user message is:

- **TUI**: `isVisibleUserMessage` (`internal/tui/model.go`) — `role == roleUser && !transient && !isCommandHistoryMessage`. Reusing the existing predicate keeps the jump count, the session title generation, and the terminal window title in agreement.
- **Server**: `isCountableUserMessage(role, content)` (`internal/server/handler_session_user_messages.go`) — `role == "user" && !strings.HasPrefix(strings.TrimSpace(content), "/")`. It mirrors the TUI predicate; `transient` needs no filter because transient messages are render-only and never persisted.

Two deliberate asymmetries:

- **Slash-command echoes are excluded.** `/theme` is `roleUser` but is chrome, not a prompt; counting it would make `msg 3/17` disagree with what the user perceives as their messages.
- **Empty/whitespace-only content IS counted.** There is no emptiness check (the TUI predicate has none). The composer already skips empty submits, so in practice this rarely arises, but the rule is "role says user", not "content is non-empty".

## Cursor semantics: seed, clamp, no wrap

- Cursor `-1` = no walk in progress.
- **The first press in either direction seeds at the NEWEST user message** — the nearest entry point from the bottom, and it keeps the two keys symmetric so neither is a silent no-op on first press.
- Subsequent presses step ±1 with **clamping at both ends and NO wrap-around**: the find bar wraps (you are hunting a term and want to cycle); walking a document does not — wrapping from the oldest prompt back to the newest would silently teleport you across the transcript.
- TUI: the walk is **cleared by any other keypress** in `handleChatKeys`. The indicator disappears when the cursor resets.
- Web: the readout and cursor **reset when the tab switches session** — the cursor is a position in the previous session's index list and would be meaningless (or out of range) in the new one.

The web helpers `nextUserJumpCursor` (same seed/clamp semantics) and `userJumpLabel` (`msg N/M`) live in `web/src/lib/userMessageNav.ts`, mirroring the TUI logic.

## Why the count is server-side

`GET /api/sessions/{id}/user-messages` (`internal/server/handler_session_user_messages.go`, route in `internal/server/server.go`) returns `{total, indices, truncated, scanned}` — same shape and limit clamping as the find-bar search endpoint (`handler_session_search.go`), reusing `session.PaginatedLoad` so it costs a scan, not a new parse.

The web client only holds a **tail window** (store cap `MAX_SLICE_MESSAGES`), so a client-side count would disagree with the TUI on the same session — the TUI loads the full transcript, the browser does not. `indices` are positions in the same post-load array the paged session response slices, so they double as pagination targets (off-window jumps).

### Lazy fetch

The index list is fetched **on the first keypress, not on mount**: a session that is read but never jumped in must not cost a request per open. If the request fails, the web client degrades to a local fallback (`isCountableUserMessage`/`loadedUserMessageIndices` over the loaded window) instead of the feature going dead — the readout then reflects the window it can actually see.

## TUI scrolling: reused core and its ordering constraint

Jumping reuses `flashAndScrollToMessage` (`internal/tui/chat_search.go:256`), which was **extracted out of `jumpToChatMatch`** precisely so the second caller (the user jump, `internal/tui/user_jump.go:85`) could share it. The load-bearing ordering inside it:

1. `ensureTranscriptMessageVisible`
2. read `transcriptMsgStartLine[msgIdx]` for the target line
3. `rerenderTranscriptAndMaybeScroll()`
4. `SetYOffset(target)` **LAST** — because `renderTranscript` → `shouldAutoScrollTranscript` → `GotoBottom` would otherwise stomp the explicit offset. Anything that scrolls after step 4 undoes the jump.

**Known limitation:** the target line is read **before** the re-render, so with a never-rendered (all-zero) `transcriptMsgStartLine` table the jump falls back to line 0. This is reachable only in a fresh session or a unit test — production frames have already populated the table.

The indicator is appended to `leftStatus` in `renderStatus()` **after** `permissionMode`, on purpose: a documented invariant says `statusPermColStart`/`statusPermColEnd` bound only the permissionMode segment for click hit-testing, and `leftStatus` is truncated from the right — so a segment there survives on narrow terminals, whereas a hint `suffix` in `rightContent` would be cut off.

Flash expiry reuses the existing `chatSearchFlashTick`/`chatSearchFlashExpiredMsg`, so there is no second expiry message type.

## Web scrolling: find-bar mechanism, follow-tail side effect

Scrolling reuses the find bar's mechanism: `virtualizer.scrollToIndex(pos, {align:"center", behavior:"smooth"})`. When the target is outside the loaded window, the off-window branch calls `olderPrefixFetch` + `api.getSession` + `PREPEND_MESSAGES`, guarded by a ref so overlapping fetches cannot double-prepend.

Side effect worth knowing: the follow-tail listener treats a bare `ArrowUp` as scroll intent, so a jump also **un-pins auto-scroll**. That is desirable — otherwise streaming would yank the view back to the bottom right after a deliberate jump.

Note on verification: jsdom cannot assert scroll positions, so the scroll pixels are not verified by the tests (stated in the `ChatPanel.userJump.test.tsx` header); the tests cover cursor math, fetch wiring, indicator rendering, and reset-on-session-switch.

## Tests

- `internal/tui/user_jump_test.go` — includes `TestUserJumpKeyDispatch`, pinning that bubbletea reports the combo as the literal string `"alt+up"` (`tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt}.String()`) — which is why the key switch matches on those strings and the `ctrl+shift+up`/`down` aliases exist.
- `internal/server/handler_session_user_messages_test.go` — endpoint shape, clamping, counting rule.
- `web/src/lib/userMessageNav.test.ts` — pure cursor/label/fallback helpers.
- `web/src/components/Chat/ChatPanel.userJump.test.tsx` — wiring, indicator, lazy fetch, session-switch reset (several mutation-verified).

## Related

- [Web Chat Composer Input History](web-chat-input-history.md) — the plain `↑`/`↓` walk over what you *typed*
- [Web UI Global Keyboard Shortcuts](web-keyboard-shortcuts.md) — where the find bar's `Ctrl+F` lives and why the jump is not in the global hook
