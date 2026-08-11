# Move Logs and Status tabs into session sub-tabs

## Goal
Make Logs and Status tabs session-scoped instead of global top-level tabs in the web/desktop SPA. This keeps session-relevant views (Chat, Agents, Changes, Logs, Status) together under the session context.

**Scoping assumption**: LogPanel and StatusPanel don't need a `sessionId` prop — LogPanel streams project-wide logs, StatusPanel reads from `useChatState()` which is already session-aware. "Belong to the session" means per-session *placement* (rendered within the sessions view, controlled by `activeSubTab`), not per-session data sources.

## User Expectation Checklist

- [x] Logs and Status are no longer top-level tabs in the tab bar
- [x] Logs and Status appear as session sub-tabs alongside Chat, Agents, Changes
- [x] Clicking Logs/Status sub-tab shows the correct panel within the sessions view
- [x] StatusBar "status" click navigates to sessions view + Status sub-tab of the active session
- [x] StatusPanel close button returns to Chat sub-tab
- [x] LogPanel uses hidden-render (not conditional mounting) to preserve buffered log history and SSE stream
- [x] No duplicate polling: LogPanel SSE stream stays alive via forceMount + CSS hidden
- [x] Persisted tab restore handles the new sub-tab values ("logs", "status")
- [x] Empty sessions state: StatusBar click creates a session tab + shows Status
- [x] Git, Cron, Assets remain top-level (unchanged)
- [x] TypeScript compiles cleanly
- [x] Existing tests pass (48/48)
- [x] Vite build succeeds

## Files Modified

### 1. `web/src/stores/projectStore.tsx`
- `SessionSubTabId` expanded to `"chat" | "agents" | "changes" | "logs" | "status"`
- Persisted tab restore whitelist updated to accept `"logs"` and `"status"`

### 2. `web/src/components/Layout/TopTabs.tsx`
- Removed Logs from the top-level tab bar (Git stays)
- Cleaned up unused `ScrollText` import

### 3. `web/src/components/Layout/SessionSubTabs.tsx`
- Added Logs and Status buttons to the session sub-tab strip

### 4. `web/src/App.tsx`
- Removed `"logs"` from `activeView` union type (added `"git"` back)
- Restored top-level `<TabsContent value="git">` with GitPanel
- Removed top-level `<TabsContent value="logs">`
- Added LogPanel within sessions view using **hidden-render** (forceMount + CSS hidden) to preserve log buffer and SSE stream; `active` prop controls scroll behavior
- Added StatusPanel within sessions view using conditional mounting (no state to preserve)
- StatusBar click: `setActiveView("sessions")` + dispatch `SET_TAB_SUB_TAB → "status"` (or create session first if none open)
- StatusPanel close: dispatches `SET_TAB_SUB_TAB → "chat"`

## Implementation Notes

### LogPanel: hidden-render, not conditional mounting
LogPanel uses local `useState` for buffered logs and a persistent SSE stream. Conditional mounting would lose the buffer and restart the SSE connection on every sub-tab switch. Using hidden-render (`absolute inset-0 hidden`) keeps it alive, matching the pattern used by Chat/Agents/Changes. The `active` prop controls scroll positioning (jump to bottom on first open, restore position on re-open).

### StatusPanel: conditional mounting
StatusPanel has no persistent local state worth preserving — it fetches fresh data on mount. Conditional mounting avoids unnecessary API polling when the sub-tab is inactive.

## Verification

1. `tsc --noEmit` — clean
2. `vitest run` — 48/48 pass
3. `vite build` — succeeds
4. Manual: open session → sub-tabs show Chat, Agents, Changes, Logs, Status
5. Manual: click Logs sub-tab → shows log stream within sessions view
6. Manual: click Status sub-tab → shows status panel within sessions view
7. Manual: StatusBar status click from any view → navigates to sessions + Status sub-tab
8. Manual: StatusPanel close → returns to Chat sub-tab
9. Manual: no sessions open → StatusBar click creates session + shows Status
10. Manual: reload page → sub-tab selection persists correctly

## Terminal security and configuration addendum

The Terminal session sub-tab is served by a single server workdir, not by the
currently selected saved-project tab. The terminal config endpoint returns that
`work_dir`, and the web UI hides/disables terminal controls when the selected
project does not match it. Unauthenticated terminal access is permitted only
for loopback-bound servers; a non-loopback server must configure credentials.

Each WebSocket is limited to a 32 KiB inbound frame and the server permits at
most eight concurrent pty sessions. `terminal_scrollback_lines` is persisted in
`ocodeconfig.json`, defaults to 9999, is clamped to 100–100000, and is passed to
xterm.js as its scrollback setting. The browser decodes pty output in streaming
UTF-8 mode so characters split across WebSocket frames remain intact.
