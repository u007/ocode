# Web/Desktop Session Tab Hierarchy — Design

- **Date:** 2026-08-09
- **Status:** Approved (design) — pending implementation plan
- **Goal:** Restructure the web/desktop right-side content navigation so
  Sessions is the primary tab group, and Chat/Agents/Changes/Status/Logs
  become sub-tabs that belong to each open session tab individually, instead
  of one global tab shared across all sessions.

---

## 1. Current state (baseline)

- `TopTabs.tsx` (`web/src/components/Layout/TopTabs.tsx:20-30`) renders a
  flat `mainTabs` array: Chat, Agents, Files, Changes, Git, Status, Logs,
  Cron, Assets. Selection is one global `useState` (`activeTab` from
  `useEditorTabs`, `App.tsx:103-118`) — switching which sub-tab is visible
  applies to every open session simultaneously.
- `OpenSessionBar.tsx` (mounted `App.tsx:308`, directly below `TopTabs`) is
  the session-tab strip, backed by `projectStore.tsx`'s `tabsByProject` /
  `activeTabByProject` (`projectStore.tsx:19-20`). It is orthogonal to
  `TopTabs` today — switching sessions never changes which sub-tab is shown.
- Only `ChatPanel` is kept alive per session (stacked, absolute-positioned,
  toggled via CSS — `App.tsx:328-353`). Agents/Changes/Status/Logs render a
  single instance reading the *currently active* session id.
- `SessionSidebar.tsx` exists but is unmounted dead code (confirmed via
  repo-wide grep and commit `b25ab73`).

## 2. Decisions (confirmed with user)

| Topic | Decision |
|---|---|
| Sub-tab scope | **Per-session, independent.** Each open session tab remembers its own last-active sub-tab (e.g. Session A on Logs, Session B on Chat, simultaneously). |
| Background panel state | **Keep alive per session**, same pattern as existing Chat behavior — Agents/Changes/Status/Logs panels stay mounted (hidden) per open session tab rather than remounting/refetching on switch-back. |
| Tab scope split | **Session-scoped:** Chat, Agents, Changes, Status, Logs. **Project-scoped:** Files, Git, Cron, Assets — these are directory/project-wide concepts, not tied to one chat session. |
| Layout | Top-level row becomes `[Files] [Git] [Cron] [Assets] [Sessions]`. Selecting "Sessions" reveals the open-session-tab strip beneath it, and a sub-tab strip (Chat/Agents/Changes/Status/Logs) beneath that, scoped to whichever session tab is active. |
| Dead code | `SessionSidebar.tsx` is deleted as part of this work (unused since `b25ab73`). |
| Editor tabs | **Nest under the Files top-level view.** `useEditorTabs`'s `activeTab`/editor-tab state currently doubles as the global `Tabs` `value` (`App.tsx:103-118`, `handleOpenFile` in `useEditorTabs.ts:43-64`). It is decoupled from the top-level view selector: opening a file switches the top-level view to `'files'` and separately tracks which editor tab (or the file tree) is showing within that view — mirroring the Sessions > session-tabs pattern one level shallower (no further sub-tab split needed under an editor tab). |

## 3. Architecture overview

```
App.tsx
 └─ activeView: 'files' | 'git' | 'cron' | 'assets' | 'sessions'   (was activeTab, global sub-tab)
     ├─ TopTabs.tsx        → [Files, Git, Cron, Assets, Sessions]
     ├─ 'files' view:
     │   ├─ EditorTabBar.tsx      (new) → open file-editor tabs + "file tree" entry
     │   └─ FileTree.tsx (when no editor tab active) | FileEditor.tsx (per open editor tab)
     ├─ GitPanel / CronPanel / AssetsPanel                          (unchanged, project-scoped)
     └─ 'sessions' view:
         ├─ OpenSessionBar.tsx        (unchanged structurally)
         ├─ SessionSubTabs.tsx        (new) → [Chat, Agents, Changes, Status, Logs]
         │     reads/writes Tab.activeSubTab for the active session id
         └─ stacked panels, keyed `${sessionId}:${subTab}`, hidden via CSS:
               ChatPanel | AgentsPanel | ChangesPanel | StatusPanel | LogPanel
```

## 4. State model changes

- `Tab` interface (`projectStore.tsx:5-9`) gains `activeSubTab: SubTabId`,
  defaulting to `'chat'`.
- New reducer action `SET_TAB_SUB_TAB(sessionId, subTab)` alongside the
  existing `SET_ACTIVE_TAB` / `ADD_TAB` family (`projectStore.tsx:27-40`).
- Persistence: `activeSubTab` piggybacks on the existing `tabsByProject`
  serialization under `ocode.ui.tabs.v1`. No storage version bump — on load,
  tabs missing `activeSubTab` default to `'chat'`.
- `App.tsx`'s `activeTab`/`setActiveTab` is repurposed as the top-level view
  selector (`'files' | 'git' | 'cron' | 'assets' | 'sessions'`); it no longer
  controls which of Chat/Agents/Changes/Status/Logs is visible.
- `useEditorTabs` (`hooks/useEditorTabs.ts`) drops its `activeTab`/`setActiveTab`
  return values (that was the same state as the old global `Tabs` value) and
  gains `activeEditorTabId: string | null` (`null` = file tree shown). Editor
  tab open/close/switch logic is otherwise unchanged. `handleOpenFile` no
  longer needs to double as "switch the whole app to this view" — that
  becomes the caller's job (`App.tsx` also calls `setActiveView('files')`
  when a file is opened from anywhere, e.g. from a chat message reference).

## 5. Component changes

- `TopTabs.tsx`: `mainTabs` shrinks to the 5 project-scoped entries plus a
  `Sessions` entry. It stops rendering file-editor tabs inline
  (`TopTabs.tsx:113-156` moves to the new `EditorTabBar.tsx`).
- New `EditorTabBar.tsx` (`Layout/`): renders the open file-editor tabs (the
  logic at `TopTabs.tsx:113-156` today) plus a "file tree" entry to return to
  `FileTree`. Mounted beneath `TopTabs` inside the `'files'` view, mirroring
  `OpenSessionBar`'s position under the `'sessions'` view.
- New `SessionSubTabs.tsx` (`Layout/`): renders the 5 session-scoped sub-tabs
  for the currently-active session tab; reads/writes `activeSubTab` via
  `SET_TAB_SUB_TAB`. Mounted directly beneath `OpenSessionBar` inside the
  `'sessions'` view.
- `OpenSessionBar.tsx`: unchanged internally, just repositioned to sit above
  `SessionSubTabs` instead of being a peer of the old global `TopTabs`.
- `AgentsPanel`, `ChangesPanel`, `StatusPanel`, `LogPanel`: extend the
  existing `ChatPanel` stacking pattern (`App.tsx:328-353`) — one mounted
  instance per `${sessionId}:${subTab}` combination among currently open
  session tabs, visibility toggled via the same absolute/hidden CSS
  technique, not conditional unmount. (`StatusPanel.tsx`, the "Status" tab
  content, is distinct from `StatusBar.tsx`, the persistent bottom status
  bar at `App.tsx:385-391` — the bottom bar is unrelated to this change and
  stays global.)
- `CoworkSidebar` (`App.tsx:395-403`) is currently shown only when
  `activeTab === "chat"`. Since `activeTab` is repurposed as the top-level
  view selector, this condition must change to: top-level view is
  `'sessions'` **and** the active session's `activeSubTab === 'chat'`.
- Delete `SessionSidebar.tsx` (dead code, unused since `b25ab73`).

## 6. Data flow

No change to how session data is fetched or streamed. `chatStore`'s
`sessions` slice (keyed by session id) and the equivalent per-session state
backing Agents/Changes/Logs already update via WebSocket regardless of
mount state. Keeping panels mounted-but-hidden is purely for UI continuity
(scroll position, in-progress local component state) — not a data-freshness
requirement.

## 7. Out of scope

- Any change to how Files/Git/Cron/Assets panels themselves work — they
  remain single-instance, project-scoped, unaffected by session switching.
- Mobile/narrow-viewport layout adjustments for the new two-row session
  header (follow existing responsive patterns already used by `TopTabs`/
  `OpenSessionBar`).
