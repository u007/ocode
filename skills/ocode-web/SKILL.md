---
name: ocode-web
description: How the ocode React SPA is wired — file map, component hierarchy, state management, streaming pipeline, routing, theming, build setup, and recurring gotchas. Use this whenever working on the web frontend (new component, new page, API integration, SSE/streaming, state changes, routing, theming).
when_to_use: When the user asks for web UI changes, new components/pages, API integration, streaming/fixie UI fixes, auth/token wiring, theming changes, Vite/build config, or anything under web/src.
---

# ocode Web UI Field Guide

A dense map of the ocode React SPA so you don't re-discover it from scratch.

## 1. Build & Dev

| Command | What it does |
|---------|-------------|
| `cd web && npm run dev` | Vite dev server on `:5173`, proxying `/api` → `:4096` |
| `cd web && npm run build` | `tsc && vite build` → `web/dist/` |
| `make web-build` | `cd web && npm install && npm run build` |
| `make web-dev` | `cd web && npm run dev` (background) |
| `make dev` | Go backend + Vite together |
| `make production` | Full Go binary with embedded `web/dist/` |

**Vite config** (`web/vite.config.ts`): bare-bones. `base: "./"` + runtime `<base>` injection in `index.html` (so assets resolve behind tailscale path prefixes). Proxy `/api` → `localhost:4096` in dev. `@/` alias → `src/`.

**Production embed chain**: `web/embed.go` uses `//go:embed all:dist` to embed the built SPA. `web.FS()` returns the embedded filesystem (nil when `dist/` missing). Both the headless server (`main.go` → `server.Run(args, web.FS())`) and the desktop app (`cmd/ocode-desktop/main.go` → `desktop.StartServer(web.FS(), workDir)`) inject the same `web.FS()`.

## 2. File Map

```
web/
├── index.html              # SPA entry, runtime <base> injection for reverse proxy
├── vite.config.ts          # Vite config: proxy /api → :4096, @/ alias, base: "./"
├── tsconfig.json           # TypeScript strict, @/ path alias
├── tailwind.config.ts      # Tailwind (minimal — CSS vars do the work)
├── postcss.config.js       # tailwindcss + autoprefixer
├── components.json         # shadcn/ui config (New York style, zinc base)
├── package.json            # React 18, react-router-dom 7, Monaco, Radix, lucide, cmdk
├── embed.go                # Go embed.FS: embeds web/dist/ into the Go binary
├── embed_test.go           # Verifies index.html is in the embedded FS
└── src/
    ├── main.tsx            # React root: BrowserRouter(basename) → App
    ├── App.tsx             # Routes, layout, global state wiring, keyboard shortcuts
    ├── index.css           # :root CSS vars (HSL for shadcn), Tailwind directives, dark-first
    ├── lib/
    │   ├── utils.ts        # cn() = clsx + tailwind-merge
    │   ├── monaco-setup.ts # Monaco worker bundling (offline-capable, no CDN)
    │   └── fileLinks.tsx   # rehype plugin for clickable file links
    ├── pages/
    │   └── SessionPage.tsx # /session/:id — main workspace with tabs + chat + streaming
    ├── api/
    │   ├── client.ts       # fetchJSON(), api object (50+ methods), SSE connect helpers
    │   └── types.ts        # TS types matching Go backend structs
    ├── stores/
    │   ├── chatStore.tsx   # Chat state: useReducer + Context (messages, live, TUI status)
    │   └── projectStore.tsx # Multi-project state: projects, tabs, sessions
    ├── hooks/
    │   ├── useChat.ts      # sendMessage, stop, resolvePermission, executeShell
    │   ├── useSessions.ts  # List/refresh sessions
    │   ├── useTheme.ts     # Fetch theme, hex→HSL conversion, CSS var injection
    │   ├── useKeyboard.ts  # Global shortcuts (⌘K palette, ⌘N new, Escape)
    │   └── useAgentRuns.ts # SSE subscription to agent-run tree
    └── components/
        ├── Layout/
        │   ├── TopTabs.tsx         # Header nav: Chat, Files, Git, Status, Logs, Assets
        │   ├── OpenSessionBar.tsx  # Compact row of open session tabs below TopTabs
        │   ├── SessionDialog.tsx   # Filterable session picker dialog
        │   ├── ProjectSidebar.tsx  # Multi-project sidebar (left edge)
        │   ├── CoworkSidebar.tsx   # Right sidebar: model, agent, context, files, LSP
        │   ├── ModelDialog.tsx     # Model selector with tabs (main/small/advisor)
        │   └── DirectoryBrowser.tsx# Folder picker for adding projects
        ├── Chat/
        │   ├── ChatPanel.tsx       # Message list with lazy loading, auto-scroll
        │   ├── ChatInput.tsx       # Textarea, slash commands, file attach, !shell
        │   ├── MessageBubble.tsx   # Renders messages with react-markdown, file links
        │   ├── TurnParts.tsx       # ThinkingBlock (collapsible) + ToolBlock (collapsible)
        │   ├── AgentPreview.tsx    # Nested agent-run tree viewer
        │   ├── PermissionDialog.tsx# Permission approval dialog
        │   ├── SlashCommandMenu.tsx# /-command autocomplete popup
        │   └── commands.ts         # Canonical COMMANDS array + dispatchCommand()
        ├── Files/
        │   ├── FileTree.tsx        # Project file browser
        │   └── FileEditor.tsx      # Monaco editor (tabbed, multi-language)
        ├── Git/
        │   └── GitPanel.tsx        # Git status, diff viewer
        ├── Logs/
        │   └── LogPanel.tsx        # Server logs with SSE stream, filtering
        ├── Assets/
        │   └── AssetsPanel.tsx     # Uploaded file manager
        ├── Status/
        │   └── StatusPanel.tsx     # TUI status drill-down (files, LSP, spending)
        ├── Agents/
        │   └── AgentsPanel.tsx     # Agent-run tree viewer (same data as TUI agents tab)
        ├── Changes/
        │   └── ChangesPanel.tsx    # Session file changes with diffs
        ├── Cron/
        │   └── CronPanel.tsx       # Scheduled jobs management
        ├── Terminal/
        │   ├── TerminalTabs.tsx    # Terminal instance manager (N tabs)
        │   └── TerminalPanel.tsx   # xterm.js + WebSocket terminal emulator
        ├── Settings/
        │   └── SettingsPanel.tsx   # App settings UI
        ├── common/
        │   ├── StatusBar.tsx       # Bottom bar: tokens, model, session, context, spending
        │   ├── CommandPalette.tsx  # ⌘K palette (cmdk-based)
        │   └── ErrorBoundary.tsx   # React error boundary with reload button
        └── ui/                    # shadcn/ui primitives (8+ components)
            ├── button.tsx, badge.tsx, input.tsx, dialog.tsx
            ├── tabs.tsx, command.tsx, select.tsx, separator.tsx
            ├── scroll-area.tsx, tooltip.tsx
```

## 3. Component Hierarchy

```
<BrowserRouter basename={_basePath}>
  <ProjectProvider>                    ← project store context
    <ChatProvider>                     ← chat store context
      <App>                            ← All routes + layout
        Routes:
        ├─ "/" → <App> (default layout)
        └─ "/session/:id" → <SessionPage> (full workspace)

        Both layouts share:
        ├── <ProjectSidebar>           (left, collapsible project list)
        ├── <TopTabs>                  (Chat | Files | Git | Status | Logs | Assets)
        ├── <OpenSessionBar>           (open session tabs below TopTabs)
        ├── Main content (tab-switched):
        │   ├── <ChatPanel>
        │   │   ├── <MessageBubble>    (react-markdown, ThinkingBlock, ToolBlock)
        │   │   └── <AgentPreview>     (nested agent-run tree, modal)
        │   ├── <FileTree> + <FileEditor>   (Files tab)
        │   ├── <GitPanel>             (Git tab)
        │   ├── <StatusPanel>          (Status tab)
        │   ├── <LogPanel>             (Logs tab)
        │   ├── <AssetsPanel>          (Assets tab)
        │   ├── <AgentsPanel>          (Agents tab)
        │   ├── <ChangesPanel>         (Changes tab)
        │   ├── <CronPanel>            (Cron tab)
        │   ├── <TerminalTabs>         (Terminal sub-tab)
        │   └── <SettingsPanel>        (Settings)
        ├── <ChatInput>                (textarea, slash menu, file attach)
        │   └── <SlashCommandMenu>
        ├── <StatusBar>                (bottom bar)
        ├── <CoworkSidebar>            (right, model/agent/context/files/LSP panel)
        │
        └── Dialogs:
            ├── <SessionDialog>        (filterable session picker)
            ├── <ModelDialog>          (model selector, 3 tabs)
            ├── <PermissionDialog>     (permission approval)
            ├── <CommandPalette>       (⌘K, cmdk-based)
            └── <AgentPreview>         (agent-run tree, also opens as dialog)
```

## 4. State Management

Two `useReducer` + `Context` stores — no Redux, no Zustand.

### chatStore.tsx
- **State**: `ChatState` — `messages[]`, `sessionId`, `model/smallModel/advisorModel`, `ocr*`, `isStreaming`, `live[]` buffer, `pendingPermission`, pagination cursor, `tuiStatus` snapshot, `spending`, `sessionContext`
- **Actions**: 24+ action types including `ADD_MESSAGE`, `SET_MESSAGES`, `LIVE_DELTA`, `LIVE_TOOL_START/RESULT`, `MERGE_SNAPSHOT` (from turn-done), `PREPEND_MESSAGES` (scroll-up lazy load), `SET_TUI_STATUS`, `PERMISSION_REQUEST/RESOLVED`, `RESET`
- **Pattern**: Separate `ChatStateContext` and `ChatDispatchContext` — read with `useChatState()`, dispatch with `useChatDispatch()`
- **RESET nuance**: preserves `advisorEnabled` and `tuiStatus` across `/new` so they don't blink out

### projectStore.tsx
- **State**: `ProjectState` — `projects[]`, `activeProject`, `projectSessions`, `openTabs[]`, `activeTab`, `sessionPickerOpen`
- **Actions**: `SET_PROJECTS`, `ADD_PROJECT`, `REMOVE_PROJECT`, `ADD_TAB`, `REMOVE_TAB`, `SET_ACTIVE_TAB`, etc.
- **Purpose**: Multi-project support, session tab management, session picker dialog state

## 5. Backend Communication

### REST API Client (`api/client.ts`)
- Single `api` object with 50+ typed methods: `api.getSessions()`, `api.sendMessage(id, text)`, `api.getSpending()`, `api.getTheme()`, `api.listProjects()`...
- `fetchJSON<T>()` wraps `fetch()` with auth headers, JSON parsing, error handling
- **Base path**: `apiPath()` prepends `_basePath` to every path — derived from `location.pathname` matching `/session/<id>` for tailscale path-prefix support
- **Auth**: Bearer token from `?token=` URL param (embedded by `/rc` command), stored at load time. `authHeaders()` returns `{Authorization: Bearer <token>}`. `authToken()` returns the raw token for EventSource URLs
- All `/api/*` calls proxied through Vite in dev (`localhost:5173` → `localhost:4096`)

### SSE Live Streaming — Dual Pipeline

Two SSE endpoints:

| Endpoint | Purpose |
|----------|---------|
| `GET /api/chat/messages?session=X` | **Persistent live mirror** of a TUI session — 2-way sync. Carries `messages` (full snapshot), `user_message`, `thinking`, `text`, `tool_start`, `tool_result`, `turn_done`, `status` events |
| `GET /api/chat/stream?session=X&message=...` | **One-off streaming chat** (direct mode, no TUI bridge). Returns `session`, `text`, `tool_start`, `tool_result`, `done`, `error` |

Key connectors in `client.ts`:
- `connectSessionMirror(session, onEvent)` — opens EventSource, registers listeners for all event types, returns cleanup. Used in `SessionPage` for live streaming
- `connectAgentRunsSSE(session, onRuns)` — live agent-run tree stream

**Auth for EventSource**: EventSource cannot set headers, so the auth token is passed as `?token=` query param. The server's `checkAuth()` checks both `Authorization: Bearer` header and `?token=` param.

### RC Bridge Architecture (TUI co-location mode)
When the web server runs alongside the TUI (`/rc` command):
1. `RCBridge` in `rc_bridge.go` is a channel-based proxy — `RcCh` receives requests, `StreamCh` sends SSE events back
2. The TUI's Update loop reads from `RcCh` and processes through its own agent
3. `RCBridge.Broadcast()` publishes events to all SSE subscribers (fan-out pattern)
4. `HandleSessionMessages` subscribes to `RCBridge` on connection, sends history immediately, then forwards all live events

## 6. Routing

`react-router-dom` v7, `<BrowserRouter basename={_basePath}>`:

```
/                    → App (workspace layout, home page)
/session/:id         → SessionPage (full chat workspace)
```

**`_basePath`**: Everything before `/session/<id>` in the URL path. Critical for tailscale path-prefix forwarding — injected via inline `<script>` in `index.html` that parses `location.pathname` and sets `<base href>` before any asset tag loads.

## 7. Streaming & Message Lifecycle

**Single source of truth**: The TUI/server holds the canonical message list. The `turn_done` SSE event carries a full snapshot (`messages` event) that replaces the current state via `MERGE_SNAPSHOT`. The live buffer (`live[]`) is cleared when the snapshot arrives. Dropped SSE events self-heal at the next snapshot.

**Failed turns keep their work**: when an LLM call fails part-way through a turn, `Agent.Step` returns the rounds that already completed alongside the error, and the server persists them and emits a `messages` snapshot *before* `turn_error`. So a failed turn ends the same way a successful one does — snapshot first, terminal frame second — and reopening the session shows everything that actually ran. The same applies to the permission/question continuations (`/api/permissions/resolve`, `/api/questions/answer`), which keep the resolved tool result instead of leaving the session on an unresolved sentinel.

**Lazy-loading chat history**: `ChatPanel` loads the last 50 messages (via `GET /api/sessions/{id}/messages` with `?after=` cursor), then prepends older messages on scroll-up via `PREPEND_MESSAGES`.

**Live rendering flow**:
1. User types message → `POST /api/chat` → returns session ID
2. `SessionPage` opens the mirror SSE connection (`GET /api/chat/messages?session=X`)
3. SSE emits `user_message` → first live fragment
4. SSE emits `thinking`, `tool_start`, `tool_result`, `text` → appended to `live[]`
5. SSE emits `turn_done` with `messages` snapshot → `MERGE_SNAPSHOT` replaces state, clears `live[]`
6. SSE emits `status` → updates `tuiStatus`

**Key nuance**: The web UI is always a remote viewer/input. Every turn runs through the TUI's agent (in RC mode) or a headless server agent. The frontend sends a message via REST, then the response arrives via SSE — no WebSocket.

## 8. Theming

- **Dark-first**: `<html class="dark">` in `index.html`, `color-scheme: dark` in CSS, light mode via `html.light`
- **CSS variables** in `index.css`: `:root` carries HSL triplets for `--background`, `--foreground`, `--primary`, `--border`, etc. (shadcn pattern)
- **Dynamic theming**: `useTheme.ts` fetches `GET /api/theme` on mount, converts hex colors to HSL triplets, sets them on `:root` CSS vars via `document.documentElement.style.setProperty()`. Server-pushed colors override defaults
- **shadcn/ui** via `components.json` — New York style, zinc base color, CSS variables mode
- **Tailwind** classes reference CSS vars: `bg-background`, `text-foreground`, `border-border`
- The same theme engine powers both the web UI and the TUI — the Go theme system is the single source of truth

## 9. Slash Commands — Double-Routed

The canonical `COMMANDS` array in `web/src/components/Chat/commands.ts` is shared by `ChatInput` (autocomplete), `SlashCommandMenu` (popup), and `CommandPalette` (⌘K). 

`dispatchCommand(name, ...)` handles them asynchronously:
- **Frontend-only** (don't hit the agent): `/help`, `/clear`, `/export` (generates Markdown + triggers browser download)
- **API-call**: `/session`, `/ocr`, `/mask`, `/compact`, `/recap`, `/share`, `/btw` — call backend endpoints directly
- **Model/agent switches**: `/model`, `/small-model`, `/advisor` — dispatched via API
- **Unknown commands**: displayed in UI but fall through to the next agent turn as plain messages

## 10. Global Shortcuts

Wired in `useKeyboard.ts`, registered in `App.tsx` top-level `useEffect`:

| Shortcut | Action |
|----------|--------|
| `⌘K` / `Ctrl+K` | Open CommandPalette |
| `⌘N` / `Ctrl+N` | New session |
| `Escape` | Close dialogs / sidebar |
| `⌘,` / `Ctrl+,` | Toggle CoworkSidebar |

All keyboard bindings are centralized — never register raw listeners in child components without a `useKeyboard` pattern.

## 11. Key Architectural Decisions & Gotchas

1. **No client-side agent** — The browser is always a remote viewer/input. Every turn runs server-side (TUI agent or headless agent). The frontend sends a message via REST, response arrives via SSE — no WebSocket.

2. **Dual SSE pipeline** — `HandleChatStream` (one-off) and `HandleSessionMessages` (persistent mirror) coexist. The web UI uses the **mirror** endpoint so it stays in sync with the TUI transparently. Don't wire the one-off endpoint unless you need a standalone stream without TUI backing.

3. **Snapshot self-heals** — The `turn_done` event carries a `messages` snapshot that fully replaces state via `MERGE_SNAPSHOT`. If a few SSE events are dropped, the next snapshot fixes everything. Never treat the live buffer as authoritative.

4. **Lazy-loading** — `ChatPanel` loads 50 messages at a time. Scroll-up triggers `PREPEND_MESSAGES`. The pagination cursor is the `before` parameter (message ID before the oldest loaded).

5. **Runtime `<base>` injection** — A synchronous inline script in `<head>` parses the URL and injects `<base href>` before any asset tag loads. `_basePath` in `client.ts` mirrors this for API paths. Both must stay in sync.

6. **Monaco offline-first** — `monaco-setup.ts` bundles all workers via Vite `?worker` imports so the editor works fully offline in ocode-desktop's webview (no CDN). If you add a new worker type, bundle it the same way.

7. **shadcn/ui with CSS vars** — All components use Tailwind classes referencing CSS variables. The server theme engine swaps these at runtime by setting HSL values on `:root`. If you add a new color role, it must be a CSS var, not a hardcoded hex.

8. **Two stores, not one** — `chatStore` + `projectStore` as separate contexts. `SessionPage` reads from both. If state needs to cross boundaries (e.g. "which project owns this session?"), wire it through `SessionPage`, not through a merged store.

9. **Auth for EventSource** — EventSource cannot set headers. The auth token is passed as `?token=` query param. The server's `checkAuth()` checks both header and query param. If you add a new SSE endpoint, make sure the URL includes `?token=`.

10. **AdvisorEnabled preserved across `/new`** — The `RESET` action preserves `advisorEnabled` and `tuiStatus`. Any new state that should survive a session reset must be added to the RESET preservation list.

11. **No test files in `web/src/`** — There are no React component tests (no vitest, no testing-library). All tests are Go-side (`web/embed_test.go`, `internal/server/handler_sse_test.go`, etc.). Add tests in the Go server layer, not in web/src.

12. **Session id routing** — The `/session/:id` route in react-router and the `sessionId` in `ChatState` must match. `SessionPage` reads `:id` from the URL params and dispatches `SET_SESSION_ID`. URL changes trigger a full session switch.

13. **SSE `status` event format** — The `status` SSE event carries a JSON blob that populates `tuiStatus` in chatStore. It mirrors the TUI's status rendering (model, agent, thinking state, spinner, spending). The web UI renders it in the StatusBar and CoworkSidebar. If the TUI adds a new status field, the web UI typing in `types.ts` needs updating.

14. **Permission flow** — When the server's agent needs a permission grant, it sends a `permission_request` SSE event. `PermissionDialog` opens, captures user choice, and sends the result via `POST /api/permissions/resolve`. The SSE then continues with the resolved tool call.
