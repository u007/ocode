# Web/Desktop Agents Tab — Design

Date: 2026-08-06

## Problem

TUI has a dedicated top-level "agents" tab (`internal/tui/tabs.go` labels
list, `internal/tui/detail_view.go`) showing every subagent run for the
current session as cards, with click-through to a full transcript drill-in
view (messages, tool calls, thinking, nested sub-agent runs).

Web/desktop (`web/` React app, embedded unchanged in `cmd/ocode-desktop` via
Wails) has no equivalent. The only agent-run UI is `AgentPreview.tsx`, a
small rail above the chat input (`max-h-52`, scrollable) that renders the
same run tree inline but is cramped for anything beyond a glance.

This closes that gap without any backend changes: `useAgentRuns(sessionId)`
already streams the full run tree (including nested children) over SSE.

## Scope

Current session only, matching the TUI (which is also per-session). No new
HTTP/SSE endpoints. No project-wide/cross-session run history in this pass.

## Design

### Shared rendering

Extract the existing recursive tree renderer out of `AgentPreview.tsx` into
`web/src/components/Agents/RunNode.tsx` (plus its helpers: `statusStyles`,
`elapsed`, `childSummary`, `messageLine`). Purely a move — visuals unchanged.
Both the rail and the new tab render runs through this one component so
nested sub-agent runs, tool calls, and thinking blocks render identically
everywhere.

### New tab

- `web/src/components/Layout/TopTabs.tsx`: add `{ id: "agents", label:
  "Agents", icon: Bot }` to `mainTabs`, alongside the existing
  chat/files/changes/git/status/logs/cron/assets tabs.
- `web/src/components/Agents/AgentsPanel.tsx`: full-height tab content.
  Two states:
  - **List** (default): top-level runs for the current session, each row
    using `RunNode`'s existing collapsed-row rendering, no height cap.
  - **Detail**: one selected run's full tree (via `RunNode`, which already
    renders nested children recursively — no separate drill-down stack is
    needed, unlike TUI's push-based navigation) plus a header (name, model,
    status, duration) and a back button to return to the list.
  - Empty state ("no agent runs yet") follows the same pattern as other
    empty tab states (e.g. Changes tab).

### Rail → tab entry point

`AgentPreview.tsx` keeps its current inline expand/collapse (clicking the
chevron/row toggles children open, same as today). Additionally, clicking a
run's **name** now calls `openAgentDetail(runId)`, which switches
`activeTab` to `"agents"` and selects that run in `AgentsPanel`'s detail
state — one consistent way to reach the full view from anywhere a run is
shown.

### State

`selectedAgentRunId` and the `openAgentDetail` setter live at the same
App-level state layer as `activeTab`/`editorTabs` (threaded down as props,
matching existing patterns — no new global store). `AgentsPanel` renders
list vs. detail based on whether `selectedAgentRunId` is set. Switching
sessions resets `selectedAgentRunId` to `null` (mirrors the rail's existing
reset-on-session-change behavior in `useAgentRuns`).

### Desktop

No separate work — `cmd/ocode-desktop` (Wails) embeds the `web/` build
unchanged, so this ships to desktop automatically once merged in `web/`.

## Testing

`web/` has no test infrastructure today (no vitest, no test script, no
`.test.tsx` files). This feature adds a minimal Vitest + React Testing
Library setup (`vitest`, `@testing-library/react`, `@testing-library/jest-dom`,
`jsdom` as devDependencies; `vitest.config.ts`; a `test` script in
`package.json`) — the first test infra in the repo — scoped to covering:
- Agents tab renders the run list for the active session.
- Clicking a run opens its detail view, including nested sub-agent runs.
- Back button returns to the list.
- Clicking a run name in the chat rail switches to the Agents tab with that
  run selected.

## Out of scope

- Cross-session / project-wide agent run history.
- New backend/SSE endpoints (existing `useAgentRuns` stream is sufficient).
- Changing the rail's existing chevron/row inline-toggle behavior.
