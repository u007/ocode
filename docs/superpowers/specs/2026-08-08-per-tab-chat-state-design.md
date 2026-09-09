# Per-Tab Chat State — Design

Date: 2026-08-08

## Problem

Session tabs in the web/desktop UI (`OpenSessionBar`) look like real tabs but
aren't: there is a single `ChatPanel` instance and a single global
`chatStore` slice holding one session's messages/live-stream/scroll state at
a time.

`SessionTabSync.tsx` resets and refetches from the server on every
`activeTabId` change:

```
chatDispatch({ type: "RESET" });
chatDispatch({ type: "SET_SESSION", sessionId: tabId });
api.getSession(tabId, { limit: PAGE_SIZE })...
```

And the SSE mirror handler drops every event whose `session_id` doesn't
match the currently active tab (`SessionTabSync.tsx:152-155`). So switching
away from a chat tab — to another session tab, another project, or a
non-chat tab like Logs — and back always: loses scroll position, re-fetches
the transcript from disk, and silently misses anything that streamed while
the tab was in the background.

## Scope

Web/desktop React app (`web/src/`) only. No backend/API changes — the SSE
mirror already carries every open session's events over one connection
(`connectSessionMirror`); the fix is entirely in how the client consumes and
renders that stream.

Chat input drafts (`web/src/lib/tabDrafts.ts`) are already per-tab and are
untouched by this change. `ChatInput` and `AgentPreview` stay singular,
still driven by the active tab id, since only the transcript/scroll/stream
view needs to persist across tab switches.

## Design

### State: per-session slices in `chatStore`

`ChatState` splits into:

- **Global fields** (unchanged, one copy): `model`, `smallModel*`,
  `advisorModel`, `advisorEnabled`, `ocr*`, `tuiStatus`, `sessionContext`,
  `spendingUSD`, `tuiStatusReady`. These reflect the single backend TUI, not
  a specific tab.
- **Per-session fields**, moved into `sessions: Record<string, SessionSlice>`
  where `SessionSlice` holds: `messages`, `live`, `isStreaming`, `error`,
  `pendingPermission`, `pendingQuestion`, `hasMore`, `loadingMore`,
  `totalMessages`, and a new `initialized: boolean` (has this session's
  first page been fetched at least once).

Actions that used to implicitly target "the current session" (`ADD_MESSAGE`,
`SET_MESSAGES`, `LIVE_DELTA`, `LIVE_TOOL_START`, `LIVE_TOOL_RESULT`,
`LIVE_RESET`, `PERMISSION_REQUEST`, `PERMISSION_RESOLVED`,
`QUESTION_REQUEST`, `QUESTION_RESOLVED`, `PREPEND_MESSAGES`,
`SET_LOADING_MORE`, `MERGE_SNAPSHOT`, `SET_TOTAL`, `SET_STREAMING`,
`SET_ERROR`) all take an explicit `sessionId`, applied to that entry in the
map (creating it if absent). `RESET` becomes "drop one session's slice"
(takes a `sessionId`), used only on tab close.

`SET_SESSION`/the old "current session" concept goes away from the store
itself — "current" is purely which tab's slice a given `ChatPanel` instance
is told to render (see below), sourced from `projectStore`'s
`tabsByProject`/`activeTabByProject`, not duplicated in `chatStore`.

### Downstream consumers of the removed global `sessionId`

Several places outside `ChatPanel` read the now-removed top-level
`chatState.sessionId` as a stand-in for "the active tab" and need to switch
to reading `activeTabId` from `projectStore` instead:

- `App.tsx`'s `currentSessionId` (`useChatState().sessionId`), passed to
  `FileEditor` (`session={currentSessionId}`), `ChangesPanel`
  (`session={currentSessionId}`), and the `selectedAgentRunId` reset effect.
- `OpenSessionBar.tsx` and `SessionDialog.tsx`, both of which compare
  `chatState.sessionId === tabId` to detect "the tab being closed is the
  active one" — becomes `activeTabId === tabId`.

`useChat()` (`hooks/useChat.ts`) is the other major consumer: `sendMessage`,
`resolvePermission`, and `submitQuestionAnswers` all read/write
`state.sessionId` directly, and it's the source of the single global
`pendingPermission`/`pendingQuestion` that `App.tsx`'s `PermissionDialog`
renders. It changes to take the target `sessionId` (the active tab, already
threaded into `ChatInput` today as `sessionTabId`) as an explicit argument
rather than an implicit read of removed store state, and to read/dispatch
against that session's slice in `chatStore.sessions`.

### Temp tab → real session id rekeying

A new chat starts life as tab id `new-<timestamp>`; today `SessionTabSync`
and `handleSessionCreated` rename it to the real server session id via
`UPDATE_TAB_ID` once the first turn's `session_started` event (or response)
arrives. Since `ChatPanel` instances and `chatStore.sessions` entries are
now keyed by session/tab id, that rename must rekey the *existing* slice
(move `sessions["new-<ts>"]` to `sessions["<real-id>"]`, preserving whatever
messages/live content already streamed in) rather than starting a fresh
slice under the new key — otherwise the first turn's own live-streamed
content is dropped at the exact moment the session is created, reintroducing
the class of bug this design fixes. A new `REKEY_SESSION` action (old id,
new id) covers this in the reducer; `SessionTabSync`'s `UPDATE_TAB_ID`
handling calls it alongside the existing `projectDispatch`.

### Rendering: one `ChatPanel` per open tab, all projects

`App.tsx` currently mounts a single `<ChatPanel />` under the "chat"
`TabsContent`. It changes to mount one `<ChatPanel sessionId={tab.id} />`
per tab across **every** project in `projectStore.tabsByProject` (not just
the active project) — each wrapped `forceMount`ed and CSS-hidden
(`data-[state=inactive]:hidden`) when its tab isn't the active one. This is
the same pattern already used for `FileEditor` tabs in `App.tsx`
(`editorTabs.map(...)` with `forceMount`).

A `ChatPanel` instance is visible when both its project is the active
project *and* its tab is `activeTabId`. Because each instance owns its own
scroll container and DOM subtree, switching tabs (including across
projects) is a pure show/hide — scroll position, loaded messages, and
in-flight live-stream rendering are preserved automatically, no remount.

Each `ChatPanel` fetches its initial page once, the first time it mounts
(tracked via its slice's `initialized` flag) — same trigger as today, just
scoped to "first mount of this session" instead of "every time it becomes
active."

Tab count is bounded by what the user has manually opened (existing
`tabsByProject` persistence), so no additional cap is introduced.

### Data flow: SSE mirror routes to every open session, not just active

`SessionTabSync`'s single persistent `connectSessionMirror` subscription
currently gates all per-session event types (`messages`, `user_message`,
`thinking`, `text`, `tool_start`, `tool_result`, `turn_done`, `question`,
`question_resolved`, `permission`, `permission_resolved`, `error`) behind
`eventSessionId === activeSessionRef.current`.

That gate is removed. Instead, for events carrying a `session_id`, the
handler dispatches into that session's slice in `chatStore.sessions` as
long as the session has an open tab (looked up via `tabsByProject` across
all projects — events for sessions with no open tab are ignored, same as
today's effective behavior for closed sessions). This keeps every open tab's
transcript, live-stream, and streaming indicator current in the background,
matching the requirement that switching away mid-response doesn't lose
anything.

`activeTabId`-driven title/status sync (`UPDATE_TAB_TITLE`, the initial
`api.getSession(tabId, {limit:1})` title fetch) stays as-is, just no longer
paired with a `RESET`.

### Prompts and badges

`PermissionDialog`/question UI in `App.tsx` continue to read only the
*active* tab's slice (`pendingPermission`/`pendingQuestion` for
`activeTabId`'s session) — a background tab hitting a permission or
question prompt does not pop a dialog over whatever the user is looking at.

`OpenSessionBar` gains a small badge (dot) on any tab whose slice has a
truthy `pendingPermission` or `pendingQuestion` and isn't the active tab.
Switching to that tab reveals the already-resolved-state dialog immediately,
since the slice is already populated (no refetch needed to discover it).
Cross-project badges are out of scope for this pass — `OpenSessionBar` only
renders the active project's tabs today, so badges are only visible for
same-project background tabs; sessions in other projects still stream and
accumulate state in the background, just without a visible indicator until
the user switches projects.

## Testing

- Existing `web/src/components/Layout/*.test.tsx` / hook tests updated for
  the new `sessions` map shape.
- Manual verification: open two session tabs in the same project, send a
  message in tab A, switch to tab B before the response finishes, confirm
  tab A's response keeps streaming and is fully present (with correct
  scroll position) when switching back — no network refetch observed (check
  Network tab / a temporary log).
- Manual verification across projects: same scenario but tab B is in a
  different project.
- Manual verification: trigger a permission prompt in a background tab,
  confirm a badge appears instead of a dialog, and the dialog appears
  correctly on switching to that tab.
