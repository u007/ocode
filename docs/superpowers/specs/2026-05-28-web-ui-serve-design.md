# Web UI for `ocode serve` — Full TUI Port

**Date:** 2026-05-28
**Status:** Approved for implementation

## Goal

Port the ocode TUI to a browser-based interface served from `ocode serve` / `ocode web`, enabling headless and server usage with a rich UI matching the terminal experience.

## Constraints

- `go install` must still work without pre-building frontend assets (stub fallback)
- No runtime memory overhead when running ocode in TUI mode (embed data stays in `.rodata`)
- SSE streaming for real-time chat (not polling or full-response REST)
- Binary size increase acceptable (~5-8MB for React build)

## Architecture

```
Browser (React SPA)  ──REST + SSE──>  Go HTTP Server  ──>  Agent/Tool/Session layer
                                         │
                                         ├── /api/* routes (existing + new)
                                         └── /* static file serving (embedded SPA)
```

- React 18 + TypeScript + Tailwind CSS + Vite for build
- Built SPA assets embedded via `go:embed` in `internal/server/web.go`
- SSE (`GET /api/chat/stream`) for streaming chat responses
- REST for state queries (sessions, models, agents, git, files)
- SPA handles client-side routing; server serves `index.html` for all non-API routes

## Directory Structure

```
ocode/
├── web/                              # React SPA source
│   ├── package.json
│   ├── tsconfig.json
│   ├── vite.config.ts
│   ├── tailwind.config.ts
│   ├── index.html
│   ├── src/
│   │   ├── main.tsx
│   │   ├── App.tsx
│   │   ├── api/
│   │   │   ├── client.ts             # REST + SSE client
│   │   │   └── types.ts              # shared types
│   │   ├── components/
│   │   │   ├── Chat/
│   │   │   │   ├── ChatPanel.tsx
│   │   │   │   ├── ChatInput.tsx
│   │   │   │   ├── MessageBubble.tsx
│   │   │   │   └── ToolOutput.tsx
│   │   │   ├── Sidebar/
│   │   │   │   ├── SessionList.tsx
│   │   │   │   ├── ModelSelector.tsx
│   │   │   │   └── AgentTabs.tsx
│   │   │   ├── Git/
│   │   │   │   └── GitPanel.tsx
│   │   │   ├── Files/
│   │   │   │   └── FileTree.tsx
│   │   │   └── common/
│   │   │       ├── PermissionDialog.tsx
│   │   │       ├── CommandPalette.tsx
│   │   │       └── StatusBar.tsx
│   │   ├── hooks/
│   │   │   ├── useChat.ts
│   │   │   ├── useSSE.ts
│   │   │   └── useSessions.ts
│   │   └── stores/
│   │       └── chatStore.ts
│   └── dist/                          # Vite build output
│       ├── index.html                 # stub fallback (committed to git)
│        └── .gitkeep                  # committed; all other dist/* gitignored
│
├── internal/
│   └── server/
│       ├── server.go                  # add static file serving route
│       ├── handler.go                 # existing
│       ├── handler_sse.go             # new: SSE streaming handler
│       ├── handler_git.go             # new: git status endpoint
│       ├── handler_files.go           # new: file tree/content endpoint
│       ├── web.go                     # new: go:embed + SPA serving
│       └── server_test.go
```

## Component Map (TUI → React)

| TUI Component | React Component | API Endpoint |
|---|---|---|
| Chat messages | `<ChatPanel>` | `GET /api/chat/stream` (SSE) |
| Input editor | `<ChatInput>` | `POST /api/sessions/{id}/message` |
| Sidebar (sessions) | `<SessionList>` | `GET /api/sessions` |
| Model picker | `<ModelSelector>` | `GET /api/models` |
| Agent tabs | `<AgentTabs>` | `GET /api/agents` |
| Tool output blocks | `<ToolOutput>` | streamed via SSE events |
| Git tab | `<GitPanel>` | `GET /api/git/status` |
| File browser | `<FileTree>` | `GET /api/files/tree` |
| Permission dialog | `<PermissionDialog>` | `POST /api/permissions/approve` |
| Status bar | `<StatusBar>` | `GET /api/status` |
| Slash commands | `<CommandPalette>` | client-side filtering |

## SSE Event Protocol

**Endpoint:** `GET /api/chat/stream?session={id}&message={content}`

Events:
```
event: text
data: {"delta": "Here's the code"}

event: tool_start
data: {"tool": "bash", "command": "ls -la"}

event: tool_result
data: {"tool": "bash", "output": "total 48\ndrwxr-xr-x..."}

event: tool_error
data: {"tool": "bash", "error": "command not found"}

event: permission_required
data: {"tool": "bash", "command": "rm -rf /tmp/foo", "request_id": "abc"}

event: done
data: {"session_id": "abc123", "model": "claude-sonnet-4"}
```

## New API Endpoints

| Method | Path | Description |
|---|---|---|
| GET | `/api/chat/stream` | SSE streaming chat |
| GET | `/api/agents` | List available agents |
| GET | `/api/git/status` | Git branch, staged/unstaged changes |
| GET | `/api/files/tree?path=` | Directory tree listing |
| GET | `/api/files/content?path=` | File content |
| POST | `/api/permissions/approve` | Approve/deny tool permission |
| GET | `/api/status` | Server status (model, uptime) |

## Build Pipeline

```makefile
web-build:
	cd web && npm install && npm run build

web-dev:
	cd web && npm run dev  # Vite dev server on :5173, proxies to Go API on :4096

build: web-build
	go build -o ocode
```

**Dev mode:** Vite dev server proxies `/api/*` to the Go server. No frontend rebuild needed during development.

**`go install` fallback:** `web/dist/index.html` (stub) is committed to git. If `web/dist/` has only the stub, the server serves it with a message "Run `make web-build` for full UI."

## Implementation Phases

### Phase 1: Foundation + Chat (Days 1-3)
- Scaffold React app (Vite + TS + Tailwind)
- `go:embed` in `internal/server/web.go`
- SSE streaming endpoint (`/api/chat/stream`)
- `<ChatPanel>`, `<ChatInput>`, `<MessageBubble>`
- End-to-end chat flow
- Static SPA serving from Go server

### Phase 2: Tools + Sessions (Days 4-6)
- SSE events for tool start/result/error
- `<ToolOutput>` (bash, file read/write, search, diff)
- `<SessionList>` + `<ModelSelector>`
- `<AgentTabs>` + `/api/agents`
- Session create/switch/continue

### Phase 3: Sidebar + Status (Days 7-9)
- `<StatusBar>` (model, tokens, cost)
- `<CommandPalette>` (slash commands)
- `<PermissionDialog>` for ask-mode tools
- Keyboard shortcuts (Enter send, Ctrl+N new, etc.)

### Phase 4: Git + Files (Days 10-12)
- `/api/git/status` endpoint
- `<GitPanel>` (branch, diff, staged/unstaged)
- `/api/files/tree` + `/api/files/content`
- `<FileTree>` with content preview

### Phase 5: Polish (Days 13-15)
- Theme support (light/dark)
- Image paste/display
- Responsive layout
- Error handling + loading states
- `Makefile` integration + CI
