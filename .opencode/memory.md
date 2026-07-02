## Current focus
- Multi-project desktop UI (SupaCode-like) implementation complete
- Backend: project list CRUD API, project-scoped session listing
- Frontend: ProjectSidebar, SessionTabs, ProjectStore
- **SSE streaming fix for headless/serve mode** (no TUI RC bridge)

## Stable preferences
- N/A

## Project decisions
- Project list stored at `~/.local/share/opencode/projects.json` by `internal/projects/projects.go`
- New API endpoints:
  - `GET /api/projects` — list saved project roots
  - `POST /api/projects` — add a project root
  - `DELETE /api/projects/{path...}` — remove a project root
  - `GET /api/projects/sessions?path=<path>` — list sessions for a specific project
- `session.ListForDir(wd)` added to list sessions scoped to a working directory path
- `session.GetStorageDirForPath(wd)` added for path-based storage dir resolution
- Frontend project store: `web/src/stores/projectStore.tsx` (ProjectProvider + useProjectState)
- Components: `ProjectSidebar`, `SessionTabs` in `web/src/components/Layout/`
- `App.tsx` redesigned: left ProjectSidebar, session tabs above content, TopTabs for secondary nav
- Backward compatible: `/session/:id` route still works independently

## SSE Streaming Fix (headless/serve mode)
- **Problem**: `HandleChat`/`HandleSendMessage` called `agent.Step()` synchronously with no streaming callbacks in headless mode (no TUI RC bridge). The SSE mirror endpoint returned empty `[]` when no bridge was active. The browser received no live tokens and no final message list.
- **Fix**: Added `headlessSubs` subscriber map to `Handler` with `subscribeHeadless()`, `unsubscribeHeadless()`, `broadcastEvent()` methods. Modified `HandleChat`/`HandleSendMessage` to set `OnDelta`/`OnMessage` callbacks that broadcast events to subscribers when no RC bridge. Modified `HandleSessionMessages` (SSE mirror) to load session history from disk and subscribe to the headless event bus when no bridge is active.
- **Key details**:
  - OnDelta kind `"reasoning"` is mapped to SSE event `"thinking"` (matching TUI RC bridge pattern)
  - OnDelta kind `"text"` passes through as `"text"`
  - Events broadcast: `user_message`, `thinking`, `text`, `tool_start`, `tool_result`, `messages`, `turn_done`
  - After `Step()` completes, a `"messages"` snapshot with the full message list is broadcast, plus `"turn_done"`
  - Subscriber channels are buffered (256), sends are non-blocking (drops on backpressure)
  - Files changed: `internal/server/handler.go`, `internal/server/handler_sse.go`, `internal/server/handler_sse_test.go`
