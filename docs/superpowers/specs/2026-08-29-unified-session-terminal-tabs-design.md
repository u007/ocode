# Unified Sessions + Terminal Tab Bar

- **Status:** draft — needs user review
- **Date:** 2026-08-29
- **Scope:** web/desktop UI (`web/src`)
- **Related:** `web/src/components/Layout/TopTabs.tsx`, `web/src/components/Layout/OpenSessionBar.tsx`, `web/src/components/Terminal/TerminalTabs.tsx`, `web/src/components/Terminal/terminalPersistence.ts`, `web/src/stores/projectStore.tsx`, `web/src/App.tsx`

## 1. Goal

Merge the top-level "Sessions" and "Terminal" tabs into one. In the merged view:

- One row of open tabs mixing chat sessions and terminals, each prefixed with an emoji (💬 chat, ⌨️ terminal) so the type is visible at a glance.
- Two separate icon-only `+` buttons — one creates a new chat session, one creates a new terminal — replacing the single `+` each view has today.
- Tabs are drag-reorderable, chat and terminal tabs freely interleaved, order persisted per project.
- The existing "Processes" pseudo-tab stays pinned at the end of the row (not draggable), same as today's terminal-only behavior.
- `TopTabs.tsx` shows one merged tab instead of two, badge count = sessions + terminals combined.

## 2. Non-goals

- No change to chat session behavior, message streaming, or terminal/pty behavior itself.
- No change to sub-tabs within a chat session (Chat/Agents/Changes/Logs/Status stay as-is, they just apply only when a chat tab is active).
- No change to cross-window/cross-tab sync mechanics (`storage` events) beyond extending them to the new unified order key.
- No merge of the underlying chat and terminal *content* areas beyond switching which one is visible.

## 3. Architecture Overview

Today, terminal tab state (`terminals[]`, `activeId`, open/close/rename/restore) is private to `TerminalTabs.tsx`, one instance per open project, always mounted so ptys survive tab/project switches. Chat session tab state lives in `projectStore` (`tabsByProject`, `activeTabId`), rendered by `OpenSessionBar.tsx`. These are two separate ownership models with separate headers; `App.tsx` picks which one is visible via `activeView: "sessions" | "terminal"`.

To build one merged header, terminal tab state must become readable from outside `TerminalTabs.tsx`. Changes:

- **New `terminalStore` module** (`web/src/stores/terminalStore.tsx`), same context/reducer shape as `projectStore`, keyed by project path. Owns `terminals[]`, `activeId`, and the open/close/rename/restore logic currently inline in `TerminalTabs.tsx`. `terminalPersistence.ts` (localStorage read/write) is reused unchanged as its persistence backend.
- **`TerminalTabs.tsx` shrinks to content-only**: renders `ProcessesPanel` / `TerminalPanel` for the active id, reading `terminals`/`activeId` from `terminalStore` instead of owning them. Still one instance per open project, still always-mounted for pty survival — only where the state lives changes, not the mount lifecycle.
- **New `UnifiedTabBar` component** (`web/src/components/Layout/UnifiedTabBar.tsx`), replaces `OpenSessionBar.tsx`'s row and the header row currently inside `TerminalTabs.tsx`. Reads chat tabs from `projectStore` and terminal tabs from `terminalStore` for the active project, merges them per the order list (below), and renders the draggable row, the two `+` buttons, the pinned Processes tab, and the "All sessions" button (unchanged, opens the existing session picker).
- **`TopTabs.tsx`**: remove the `terminal` entry from `mainTabs`; the remaining "Sessions" entry's badge becomes `sessionsCount + terminalCount`. The tab id stays `"sessions"` — `activeView`'s type just drops `"terminal"`.
- **`App.tsx`**: the terminal content block (the `projectPaths.map` loop rendering one `TerminalTabs` per open project, kept mounted for pty survival) keeps its current DOM position — a sibling block outside `TabsContent value="sessions"`, per the existing comment explaining why it's deliberately not nested inside a `forceMount` `TabsContent`. Only its visibility condition changes, from `activeView === "terminal"` to `activeView === "sessions" && focusedKind === "terminal"` (new state, see §4). Inside `TabsContent value="sessions"`, `UnifiedTabBar` always renders when that tab is active (it's the shared header for both kinds); the chat-only content below it (`SessionSubTabs` + chat panels) gains a `focusedKind === "chat"` visibility gate.

**Risk area**: terminal ptys must keep surviving tab/project switches exactly as now (always-mounted, `display:none` when inactive) — moving state ownership must not touch that mount lifecycle, only who reads/writes `terminals`/`activeId`.

**Validated for free**: `StatusBar` and `CoworkSidebar` in `App.tsx` (lines ~774, ~793) already gate on `activeSessionTab = tabs.find((t) => t.id === activeTabId)`, which naturally evaluates to `undefined` whenever a terminal is focused (since `activeTabId` stays chat-only per §4 and a terminal id is never in `tabs`). Both need zero code changes — they already hide correctly when the merged view is showing a terminal instead of a chat.

## 4. Data Model

- **Each store keeps owning only its own ids** — no shared/overloaded id field. `projectStore.activeTabId` continues to mean exactly what it means today: the active (or last-active) chat session for the project, unchanged semantics, still what `useChat`, `SessionTabSync`, rename-via-API, and `REKEY_SESSION` key off. `terminalStore.activeId` continues to mean the active terminal id or the `"processes"` sentinel, unchanged from today's `TerminalTabs.tsx` behavior.
- **New `focusedKind: "chat" | "terminal"`**: plain `App.tsx` state (`useState`, not a store), mirroring how `activeView` is already owned by `App.tsx` and handed to `TopTabs` as a controlled prop. Says which of the two systems' content region is currently visible; passed to `UnifiedTabBar` as a controlled prop (`focusedKind`, `onFocusChange`). Resets to `"chat"` on project switch (v1 — no per-project memory of last-focused kind; can be revisited later without touching the rest of this design).
- Kind of an individual *tab pill* in the merged row (for rendering the emoji prefix and routing clicks) is derived by lookup, same as before: id present in this project's chat tabs → chat; in its terminal list, or equal to `"processes"` → terminal.
- **Unified order list**: new small localStorage-backed helper, `web/src/components/Layout/tabOrderPersistence.ts`, mirroring `terminalPersistence.ts`'s shape. Stores, per project path, an ordered array of composite keys (`chat:<sessionId>`, `term:<terminalId>`) — `processes` is never part of this list, it's always rendered last and undraggable. On mount, `UnifiedTabBar` reconciles this order against the live chat + terminal id sets (drops stale ids, appends new ones at the end in creation order) the same way `TerminalTabs.tsx` already reconciles restored terminals against `bumpSeqPast`.
- Reordering only touches this order list — it never mutates `projectStore` or `terminalStore` tab arrays, avoiding cross-store coupling.

## 5. Component Behavior

- **Row rendering**: `UnifiedTabBar` sorts the merged {chat tabs} ∪ {terminal tabs} by the order list, renders each as a draggable pill (dnd-kit, already a dependency via `TerminalTabs.tsx`) with an emoji prefix (💬 / ⌨️), title, close button, and existing per-kind extras (chat: pending/processing badge from `OpenSessionBar`; terminal: none beyond title). Rename-on-double-click stays, routed to the correct store by kind.
- **Clicking a pill**: chat pill → `openSessionTab(id)` (existing, updates `projectStore.activeTabId`) + `setFocusedKind("chat")`. Terminal pill → `terminalStore`'s `setActiveId(id)` + `setFocusedKind("terminal")`.
- **`+` buttons**: two icon-only buttons at the end of the draggable region — 💬+ calls the existing `openNewSessionTab` (same reuse-empty-tab logic as today) + `setFocusedKind("chat")`; ⌨️+ calls the existing `openTerminal` (now on `terminalStore`) + `setFocusedKind("terminal")`. Each newly created id is appended to the order list and made active.
- **Processes tab**: pinned after the `+` buttons, same as `TerminalTabs.tsx` today — clicking sets `terminalStore.activeId` to `"processes"` + `setFocusedKind("terminal")`.
- **Closing a tab**: routed to the correct store by kind (chat → `closeSessionTab` + existing chat cleanup; terminal → terminal store's close, which unmounts its `TerminalPanel` and kills the pty as today). Removed id is dropped from the order list.
- **Drag reorder**: `dnd-kit`'s `SortableContext` over the combined id list (excluding `processes`); `onDragEnd` writes the new order to the order list only.
- **Keyboard shortcuts (`App.tsx`'s `useKeyboard`)**: `onNewTerminal` (Cmd/Ctrl+T) — when `activeView === "sessions"`, always opens a new terminal for the active project and sets `focusedKind = "terminal"` (today it only did this if already on the terminal top-level tab; now that terminal is reachable within the merged tab, this always makes sense there); the existing else-branch (open new chat session on other top-level tabs) is unchanged. `onCloseSession` (Cmd/Ctrl+W) — when `activeView === "sessions"`, branch on `focusedKind`: `"terminal"` → close the active terminal (today's `activeView === "terminal"` branch, moved); `"chat"` → close the active chat tab (today's existing branch, condition changed from `activeView === "sessions"` to `activeView === "sessions" && focusedKind === "chat"`).

## 6. Testing

- Adapt `OpenSessionBar`'s existing tab-bar tests and `TerminalTabs.test.tsx` to the new `terminalStore` + `UnifiedTabBar` split (state assertions move from component-local state to store state).
- New test: dragging a terminal tab before/after a chat tab persists that order and survives reload (order list read back correctly).
- New test: 💬+ creates a chat tab and activates it without disturbing terminal tabs/ptys; ⌨️+ creates a terminal tab and activates it without disturbing chat tabs.
- New test: closing the active tab of one kind falls back to a sensible neighbor (existing per-kind fallback logic, exercised through the merged bar).
- Existing pty-survival guarantee (terminal stays mounted/alive across tab and project switches) must still hold — covered by not touching `TerminalPanel`'s mount lifecycle, verified by existing terminal persistence tests plus a manual check per `AGENTS.md` UI-testing convention.
