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
| `cd web && npm run build` | `tsgo && vite build` → `web/dist/` |
| `cd web && npm run test` | Vitest test suite |
| `cd web && npm run typecheck` | `tsgo --noEmit` |
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
├── package.json            # React 18, react-router-dom 7, Monaco, Radix, lucide, cmdk, xterm 6, @dnd-kit, @tanstack/react-store, shiki, mermaid, pdfjs-dist, xlsx
├── embed.go                # Go embed.FS: embeds web/dist/ into the Go binary
├── embed_test.go           # Verifies index.html is in the embedded FS
└── src/
    ├── main.tsx            # React root: BrowserRouter(basename) → App
    ├── App.tsx             # Routes, layout, global state wiring, keyboard shortcuts
    ├── index.css           # :root CSS vars (HSL for shadcn), Tailwind directives, dark-first
    ├── lib/
    │   ├── utils.ts            # cn() = clsx + tailwind-merge
    │   ├── monaco-setup.ts     # Monaco worker bundling (offline-capable, no CDN)
    │   ├── fileLinks.tsx       # rehype plugin for clickable file links
    │   ├── eventBus.ts         # Single SSE transport (fetch-based, replaces EventSource)
    │   ├── sessionEvents.ts    # Session lifecycle event handlers
    │   ├── browserStore.ts     # Browser surface state (TanStack Store)
    │   ├── viewPersistence.ts  # Active view + focused kind persistence
    │   ├── tabDrafts.ts        # New-tab draft message management
    │   ├── tabQueue.ts         # Tab creation queue
    │   ├── fileSearchHighlight.ts # File search highlight state
    │   ├── projectDrag.ts      # Project drag-and-drop reordering
    │   ├── previewKind.ts      # Preview file type detection
    │   ├── trustedProject.ts   # Trusted project resolution
    │   ├── wails.ts            # Wails desktop runtime bridge
    │   └── debug/              # Debug utilities
    ├── pages/
    │   └── SessionPage.tsx # /session/:id — main workspace with tabs + chat + streaming
    ├── api/
    │   ├── client.ts       # fetchJSON(), authedFetch(), api object (50+ methods), readSSEStream()
    │   └── types.ts        # TS types matching Go backend structs
    ├── stores/
    │   ├── chatStore.tsx       # Chat state: useReducer + Context (messages, live, TUI status)
    │   ├── projectStore.tsx    # Multi-project state: projects, tabs, sessions, sub-tabs
    │   ├── browserTabsStore.tsx # Embedded browser tabs per project (useReducer + Context)
    │   └── terminalStore.tsx   # Terminal instances per project (TanStack Store)
    ├── hooks/
    │   ├── useChat.ts           # sendMessage, stop, resolvePermission, executeShell
    │   ├── useSessions.ts       # List/refresh sessions
    │   ├── useTheme.ts          # Fetch theme, hex→HSL conversion, CSS var injection
    │   ├── useKeyboard.ts       # Global shortcuts (⌘K palette, ⌘N new, Escape)
    │   ├── useAgentRuns.ts      # SSE subscription to agent-run tree
    │   ├── useEditorTabs.ts     # Editor tab state and persistence
    │   ├── useIsMobile.ts       # Responsive breakpoint hook
    │   ├── useLogPrefs.ts       # Log panel filter preferences
    │   ├── useResizableSidebar.ts # Sidebar resize state
    │   ├── useSessionStatus.ts  # Session status polling
    │   ├── useTerminalConfig.ts # Terminal configuration preferences
    │   └── useTurnWatchdog.ts   # Detect stuck turns
    └── components/
        ├── Layout/
        │   ├── TopTabs.tsx         # Header nav: Sessions, Files, Git, Cron, Assets, Settings
        │   ├── UnifiedTabBar.tsx   # Drag-and-drop sortable session/terminal tabs (@dnd-kit)
        │   ├── SessionSubTabs.tsx  # Per-session sub-tabs: Chat, Agents, Changes, Logs, Status, Preview
        │   ├── SessionTabSync.tsx  # URL ↔ store sync for session tabs
        │   ├── SessionDialog.tsx   # Filterable session picker dialog
        │   ├── ProjectSidebar.tsx  # Multi-project sidebar (left edge)
        │   ├── CoworkSidebar.tsx   # Right sidebar: model, agent, context, files, LSP
        │   ├── ModelDialog.tsx     # Model selector, one purpose per open (main/small/advisor/recap/ocr/mask/commit/summary/permission/explorer/context/autocontinue)
        │   ├── DirectoryBrowser.tsx# Folder picker for adding projects
        │   ├── EditorTabBar.tsx    # File editor tab bar
        │   ├── ShareDialog.tsx     # Session share dialog
        │   ├── PluginsPanel.tsx    # MCP plugins panel
        │   ├── PortMapsWidget.tsx  # Port forwarding status widget
        │   ├── ContextMenu.tsx     # Right-click context menu
        │   └── SyncStatusWidget.tsx# Sync status indicator
        ├── Chat/
        │   ├── ChatPanel.tsx       # Message list with lazy loading, auto-scroll
        │   ├── ChatInput.tsx       # Textarea, slash commands, file attach, !shell
        │   ├── ChatSearchBar.tsx   # In-chat search bar (ctrl+f on chat tab)
        │   ├── MessageBubble.tsx   # Renders messages with react-markdown, file links
        │   ├── HighlightedCode.tsx # Syntax-highlighted code blocks (shiki)
        │   ├── TurnParts.tsx       # ThinkingBlock (collapsible) + ToolBlock (collapsible)
        │   ├── AgentPreview.tsx    # Nested agent-run tree viewer
        │   ├── EditorContextChip.tsx # Editor context indicator chip
        │   ├── ModelPromptRow.tsx  # Model/prompt info row
        │   ├── PermissionDialog.tsx# Permission approval dialog
        │   ├── QuestionDialog.tsx  # Question prompt dialog
        │   ├── SlashCommandMenu.tsx# /-command autocomplete popup
        │   └── commands.ts         # Canonical COMMANDS array + dispatchCommand()
        ├── Files/
        │   ├── FileTree.tsx        # Project file browser
        │   ├── FileEditor.tsx      # Monaco editor (tabbed, multi-language)
        │   ├── FileTabContent.tsx  # Lazy-loaded file content wrapper
        │   ├── FilePicker.tsx      # File picker dialog
        │   └── MonacoSettingsPanel.tsx # Monaco editor settings
        ├── Git/
        │   └── GitPanel.tsx        # SourceTree-style: staged/unstaged panes, hunk actions, commit log, commit box
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
        │   ├── TerminalPanel.tsx   # xterm.js 6 + WebSocket terminal emulator (WebGL)
        │   ├── TerminalFindBar.tsx # Terminal search bar
        │   ├── ProcessesPanel.tsx  # Running processes viewer
        │   ├── terminalHistory.ts  # Terminal scrollback persistence
        │   ├── terminalSnapshot.ts # Dirty-generation guard for periodic buffer saves
        │   └── terminalPersistence.ts # Terminal config persistence
        ├── Settings/
        │   ├── SettingsPanel.tsx   # App settings UI
        │   ├── ProfilesManager.tsx # Model profiles manager
        │   ├── TTSForm.tsx         # Text-to-speech settings
        │   └── AdvisorForm.tsx     # Advisor model settings
        ├── Speech/
        │   ├── SpeechProvider.tsx  # TTS state context provider
        │   ├── SpeechToolbar.tsx   # Speech control toolbar
        │   └── speechUtils.ts      # DOM-based rendered-text extraction for TTS
        ├── Browser/
        │   ├── BrowserPanel.tsx    # Embedded browser panel (side + standalone)
        │   ├── ChromeViewport.tsx  # Chrome DevTools Protocol viewport renderer
        │   ├── DevConsole.tsx      # Browser DevTools console
        │   ├── AddressBar.tsx      # Browser address bar
        │   ├── cdpProtocol.ts      # CDP message types
        │   ├── useCdpSocket.ts     # CDP WebSocket hook
        │   ├── useBrowserMessages.ts # Browser message handler
        │   └── browserPersistence.ts # Browser tab state persistence
        ├── RemoteReconnect.tsx     # Remote session reconnect page
        ├── ProfileSwitcher.tsx     # Model profile switcher
        ├── common/
        │   ├── StatusBar.tsx       # Bottom bar: tokens, model, session, context, spending
        │   ├── CommandPalette.tsx  # ⌘K palette (cmdk-based)
        │   └── ErrorBoundary.tsx   # React error boundary with reload button
        └── ui/                    # shadcn/ui primitives (12+ components)
            ├── button.tsx, badge.tsx, input.tsx, dialog.tsx
            ├── tabs.tsx, command.tsx, select.tsx, separator.tsx
            ├── scroll-area.tsx, tooltip.tsx
            ├── context-menu.tsx, popover.tsx, progress.tsx
```

## 3. Component Hierarchy

```
<BrowserRouter basename={_basePath}>
  <ProjectProvider>                    ← project store context
    <ChatProvider>                     ← chat store context
      <TerminalProvider>               ← terminal store context
        <BrowserTabsProvider>          ← browser tabs store context
          <App>                        ← All routes + layout
            Routes:
            ├─ "/" → <App> (default layout)
            └─ "/session/:id" → <SessionPage> (full workspace)

            Both layouts share:
            ├── <ProjectSidebar>       (left, collapsible project list)
            ├── <TopTabs>              (Sessions | Files | Git | Cron | Assets | Settings)
            ├── Main content (tab-switched):
            │   ├── Sessions tab:
            │   │   ├── <UnifiedTabBar> (drag-and-drop session/terminal tabs, multi-row wrap)
            │   │   ├── <SessionSubTabs> (Chat | Agents | Changes | Logs | Status | Preview)
            │   │   ├── <ChatPanel>
            │   │   │   ├── <MessageBubble> (react-markdown, ThinkingBlock, ToolBlock)
            │   │   │   └── <AgentPreview>  (nested agent-run tree, modal)
            │   │   ├── <FileTree> + <FileTabContent> (Files sub-tab)
            │   │   ├── <GitPanel>      (Git sub-tab)
            │   │   ├── <StatusPanel>   (Status sub-tab)
            │   │   ├── <LogPanel>      (Logs sub-tab)
            │   │   ├── <AgentsPanel>   (Agents sub-tab)
            │   │   ├── <ChangesPanel>  (Changes sub-tab)
            │   │   ├── <CronPanel>     (Cron tab)
            │   │   ├── <AssetsPanel>   (Assets tab)
            │   │   ├── <SettingsPanel> (Settings tab)
            │   │   └── <PreviewHost>   (file preview side panel)
            │   ├── <TerminalTabs>      (Terminal, project-level top tab)
            │   └── <BrowserPanel>      (Browser, project-level top tab)
            ├── <ChatInput>             (textarea, slash menu, file attach)
            │   └── <SlashCommandMenu>
            ├── <StatusBar>             (bottom bar)
            ├── <CoworkSidebar>         (right, model/agent/context/files/LSP panel)
            ├── <SpeechProvider>        (TTS state)
            ├── <SpeechToolbar>         (speech controls)
            ├── <RemoteReconnect>       (remote session reconnect)
            ├── <ProfileSwitcher>       (model profile switcher)
            │
            └── Dialogs:
                ├── <SessionDialog>     (filterable session picker)
                ├── <ModelDialog>       (model selector, one purpose per open — see ModelDialogTab)
                ├── <PermissionDialog>  (permission approval)
                ├── <QuestionDialog>    (question prompt)
                ├── <ShareDialog>       (session share)
                ├── <CommandPalette>    (⌘K, cmdk-based)
                └── <AgentPreview>      (agent-run tree, also opens as dialog)
```

## 4. State Management

Four stores — no Redux, no Zustand.

### chatStore.tsx
- **State**: `ChatState` — `messages[]`, `sessionId`, `model/smallModel/advisorModel`, `ocr*`, `isStreaming`, `live[]` buffer, `pendingPermission`, pagination cursor, `tuiStatus` snapshot, `spending`, `sessionContext`
- **Actions**: 24+ action types including `ADD_MESSAGE`, `SET_MESSAGES`, `LIVE_DELTA`, `LIVE_TOOL_START/RESULT`, `MERGE_SNAPSHOT` (from turn-done), `PREPEND_MESSAGES` (scroll-up lazy load), `SET_TUI_STATUS`, `PERMISSION_REQUEST/RESOLVED`, `QUESTION_REQUEST/RESOLVED/ANSWERED`, `RESET`
- **Pattern**: Separate `ChatStateContext` and `ChatDispatchContext` — read with `useChatState()`, dispatch with `useChatDispatch()`
- **RESET nuance**: preserves `advisorEnabled` and `tuiStatus` across `/new` so they don't blink out

### projectStore.tsx
- **State**: `ProjectState` — `projects[]`, `activeProject`, `projectSessions`, `tabsByProject{}`, `activeTabByProject{}`, `sessionPickerOpen`, `groups[]`, `sessionsByProject{}` (per-project cache)
- **Actions**: `SET_PROJECTS`, `ADD_PROJECT`, `REMOVE_PROJECT`, `ADD_TAB`, `REMOVE_TAB`, `SET_ACTIVE_TAB`, `SET_TAB_SUB_TAB`, `UPDATE_TAB_TITLE`, `RESTORE_TABS`, `SET_SESSION_PICKER`, `SET_GROUPS`, `REKEY_TABS`, etc.
- **Purpose**: Multi-project support, session tab management, sub-tab routing (`SessionSubTabId`: chat|agents|changes|logs|status|preview), session picker dialog state, project groups
- **Pattern**: `useProjectState()` for reads, `useProjectDispatch()` for writes (dispatch identity is stable — use this in `React.memo`'d children to avoid re-renders)

### browserTabsStore.tsx
- **State**: `tabsByProject{}` → `BrowserTab[]`, `activeByProject{}` → `string|null`
- **Actions**: `OPEN`, `OPEN_BACKGROUND`, `OPEN_RESTORED`, `CLOSE`, `RENAME`, `CLEAR_MANUAL`, `ACTIVATE`, `RESTORE`
- **Purpose**: Embedded browser tabs per project (useReducer + Context). Tracks per-tab manual titles and active selection. Persisted via `browserPersistence.ts`

### terminalStore.tsx
- **State**: `TerminalInstance[]` per project (with `id`, `title`, `renamed`, `oscTitle`), active terminal per project
- **Actions**: `ADD`, `REMOVE`, `RENAME`, `SET_OSC_TITLE`, `ACTIVATE`, `RESTORE`
- **Purpose**: Terminal tab instances per project (TanStack Store, not useReducer). Persisted via `terminalPersistence.ts`. `terminalDisplayTitle()` resolves manual rename → OSC title → default name

## 5. Backend Communication

### REST API Client (`api/client.ts`)
- Single `api` object with 50+ typed methods: `api.getSessions()`, `api.sendMessage(id, text)`, `api.getSpending()`, `api.getTheme()`, `api.listProjects()`...
- `fetchJSON<T>()` wraps `fetch()` with auth headers, JSON parsing, error handling
- `authedFetch()` wraps `fetch()` with auth headers (used for non-JSON endpoints)
- **Base path**: `apiPath()` prepends `_basePath` to every path — derived from `location.pathname` matching `/session/<id>` for tailscale path-prefix support
- **Auth**: Bearer token from `?token=` URL param (embedded by `/rc` command), stored at load time. `authHeaders()` returns `{Authorization: Bearer <token>}`. `authToken()` returns the raw token for SSE URLs
- All `/api/*` calls proxied through Vite in dev (`localhost:5173` → `localhost:4096`)

### SSE Live Streaming — Unified Event Bus

A single long-lived fetch-based SSE stream on `GET /api/events` carries every event type: chat mirror frames, turn lifecycle, status, logs, agent runs, git status, spending. Consumers register per-event-type handlers via `lib/eventBus.ts`.

| Transport | Purpose |
|-----------|---------|
| `GET /api/events` | **Single persistent SSE stream** — all event types for all subscribed projects |
| `readSSEStream()` | Fetch-based SSE parser (used instead of EventSource for auth header support) |

**Why fetch, not EventSource**: `EventSource` cannot set custom headers. Remote-mode auth requires `Authorization: Bearer` header. The `readSSEStream()` helper in `api/client.ts` parses the SSE text protocol from a fetch Response body.

**EventBus** (`lib/eventBus.ts`):
- Singleton module-level instance, auto-starts on first `on()` call
- `eventBus.on(eventType, handler)` — register per-event handler, returns cleanup function
- `eventBus.onReconnect(handler)` — fires on stream re-establishment for state reconciliation
- `eventBus.setProjects(paths[])` — drives server's subscriber-aware emitters; changing it restarts the stream
- Reliability: exponential backoff reconnect, `seq` gap detection (warning + reconcile, no replay)
- Reconcile recovery: `reconcileOpenSessions` refetches `GET /api/sessions/:id/state` and hydrates a paused permission/question dialog from its `pending_asks` field (read from the live agent transcript). The `PERMISSION_ASK:` sentinel is normally in the persisted transcript, but a failed/conflicting save or a pause after the last write can leave it absent. Three client triggers hydrate the dialog: reconcile (reconnect/load/activation), a 409 send (`useChat`'s `hydratePendingAsks`), and the `turn_error`/`error` frame (`scheduleHydratePendingAsks` in `sessionEvents.ts`) — the last is required because the web client always sends `async:true`, so a refused send resolves 202 and the `ErrPermissionPending` refusal arrives later as an error frame rather than a 409.
- Handlers receive full envelope with `session_id` and `project` for routing

**Key event types** (via `sessionEvents.ts`):
- `messages` — full session snapshot (replaces state via `MERGE_SNAPSHOT`)
- `user_message`, `thinking`, `text`, `tool_start`, `tool_result`, `turn_done`, `turn_error` — live streaming fragments
- `status` — TUI status snapshot
- `git_status` — working-tree change counts
- `spending` — token usage
- `permission` — permission approval needed (paired with `permission_resolved`)
- `question` — agent question prompt (paired with `question_resolved`)
- **Answered questions render as a Q&A card.** On a successful `POST /api/questions`, `useChat.submitQuestionAnswers` dispatches `QUESTION_ANSWERED`, which rewrites the local `QUESTION_PROMPT:` sentinel tool result in place with a shape-compatible payload parsed from the same answers the server sends the model — so the chat shows the questions + the selected answers the instant the dialog is submitted, without waiting for the continuation turn's `messages` snapshot. `TurnParts.parseQuestionAnswers` / `QuestionAnswerBlock` render that payload for any `question` tool call whose result is the answer array (grouped, loose, and live paths all go through `ToolBlock`). The raw `QUESTION_PROMPT:` sentinel is never rendered, including from the live stream.
- `permission_check` — auto-permission judge activity
- `log` — server log lines

**Auth for SSE**: `GET /api/events` is a fetch request (not EventSource), so it carries `Authorization: Bearer` headers natively. No `?token=` query param needed for the event bus stream.

### RC Bridge Architecture (TUI co-location mode)
When the web server runs alongside the TUI (`/rc` command):
1. `RCBridge` in `rc_bridge.go` is a channel-based proxy — `RcCh` receives requests, `StreamCh` sends SSE events back
2. The TUI's Update loop reads from `RcCh` and processes through its own agent
3. `RCBridge.Broadcast()` publishes events to the unified event bus (`/api/events`) for all SSE subscribers (fan-out pattern)
4. The event bus delivers events to all connected web clients

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
2. The event bus (`eventBus.on(...)`) receives live events from the `GET /api/events` stream
3. SSE emits `user_message` → first live fragment
4. SSE emits `thinking`, `tool_start`, `tool_result`, `text` → appended to `live[]`
5. SSE emits `turn_done` with `messages` snapshot → `MERGE_SNAPSHOT` replaces state, clears `live[]`
6. SSE emits `status` → updates `tuiStatus`

**Key nuance**: The web UI is always a remote viewer/input. Every turn runs through the TUI's agent (in RC mode) or a headless server agent. The frontend sends a message via REST, then the response arrives via the event bus SSE stream — no WebSocket.

## 8. Theming

- **Dark-first**: `<html class="dark">` in `index.html`, `color-scheme: dark` in CSS, light mode via `html.light`
- **CSS variables** in `index.css`: `:root` carries HSL triplets for `--background`, `--foreground`, `--primary`, `--border`, etc. (shadcn pattern)
- **Dynamic theming**: `useTheme.ts` fetches `GET /api/theme` on mount, converts hex colors to HSL triplets, sets them on `:root` CSS vars via `document.documentElement.style.setProperty()`. Server-pushed colors override defaults
- **Contrast-safe mapping (gotcha)**: terminal palettes are designed for text-on-background only — mapping colors 1:1 onto shadcn surface roles makes vivid themes unreadable (e.g. `lcars` tan text on orange user bubbles, gray hint on brown muted cards, ~1.4:1). `computeThemeVars()` checks WCAG AA (4.5:1) per pair and only repairs what fails: foregrounds on colored surfaces (`--primary-foreground`, `--accent-foreground`, `--destructive-foreground`, `--secondary-foreground`) pick the best AA-passing theme color (black/white last resort); the `--muted`/`--muted-foreground` pair darkens the surface toward `--background` first (never toward `text` — that can flip surface polarity, see dracula in `useTheme.test.ts`), then brightens the foreground toward `text`→neutral. Consequence: any component painting text on `bg-accent`/`hover:bg-accent` MUST use `text-accent-foreground`/`hover:text-accent-foreground`, never inherit `text-foreground`. `web/src/hooks/useTheme.test.ts` asserts all pairs across every built-in palette (`themePalettes.fixture.ts`, snapshot of `internal/theme/theme.go` — regenerate when palettes change)
- **shadcn/ui** via `components.json` — New York style, zinc base color, CSS variables mode
- **Tailwind** classes reference CSS vars: `bg-background`, `text-foreground`, `border-border`
- The same theme engine powers both the web UI and the TUI — the Go theme system is the single source of truth

## 9. Slash Commands — Double-Routed

The canonical `COMMANDS` array in `web/src/components/Chat/commands.ts` is shared by `ChatInput` (autocomplete), `SlashCommandMenu` (popup), and `CommandPalette` (⌘K). Commands are **dynamically loaded** via `loadDynamicCommands()` (called by `useCommands()` hook), which merges additional commands from the server at runtime. The static array contains core commands (`/help`, `/clear`, `/export`, `/model`, etc.); server-provided commands (like `/computer`, `/sandbox`) are appended on first load.

`dispatchCommand(name, ...)` handles them asynchronously:
- **Frontend-only** (don't hit the agent): `/help`, `/clear`, `/export` (generates Markdown + triggers browser download), `/export-claude` (appends to Claude history)
- **API-call**: `/session`, `/ocr`, `/mask`, `/compact`, `/recap`, `/share`, `/btw`, `/sandbox` — call backend endpoints directly
- **Model/agent switches**: `/model`, `/small-model`, `/advisor` — dispatched via API
- **Computer-use**: `/computer` — show/enable/disable computer-use status
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

2. **Unified event bus** — `GET /api/events` is a single long-lived fetch-based SSE stream carrying all event types. `lib/eventBus.ts` is the singleton transport; consumers register per-event-type handlers via `eventBus.on(event, handler)`. The old per-session EventSource connectors (`connectSessionMirror`, `connectAgentRunsSSE`) are deleted. `readSSEStream()` in `api/client.ts` parses the SSE text protocol from a fetch Response body — used because `EventSource` cannot set auth headers.

3. **Snapshot self-heals** — The `turn_done` event carries a `messages` snapshot that fully replaces state via `MERGE_SNAPSHOT`. If a few SSE events are dropped, the next snapshot fixes everything. Never treat the live buffer as authoritative.

4. **Lazy-loading** — `ChatPanel` loads 50 messages at a time. Scroll-up triggers `PREPEND_MESSAGES`. The pagination cursor is the `before` parameter (message ID before the oldest loaded).

5. **Runtime `<base>` injection** — A synchronous inline script in `<head>` parses the URL and injects `<base href>` before any asset tag loads. `_basePath` in `client.ts` mirrors this for API paths. Both must stay in sync.

6. **Monaco offline-first** — `monaco-setup.ts` bundles all workers via Vite `?worker` imports so the editor works fully offline in ocode-desktop's webview (no CDN). If you add a new worker type, bundle it the same way.

7. **shadcn/ui with CSS vars** — All components use Tailwind classes referencing CSS variables. The server theme engine swaps these at runtime by setting HSL values on `:root`. If you add a new color role, it must be a CSS var, not a hardcoded hex.

8. **Four stores, not one** — `chatStore` + `projectStore` + `browserTabsStore` + `terminalStore` as separate contexts. `SessionPage` reads from `chatStore` and `projectStore`; `App` wraps all four providers. If state needs to cross boundaries (e.g. "which project owns this session?"), wire it through `App`/`SessionPage`, not through a merged store.

9. **Auth for SSE** — The event bus (`GET /api/events`) is a fetch request, not EventSource, so it carries `Authorization: Bearer` headers natively. Legacy one-shot SSE endpoints (file search, exports) use `readSSEStream()` which also carries auth headers. If you add a new SSE endpoint, use `readSSEStream()` — never bare `EventSource` — so auth headers flow through.

10. **AdvisorEnabled preserved across `/new`** — The `RESET` action preserves `advisorEnabled` and `tuiStatus`. Any new state that should survive a session reset must be added to the RESET preservation list.

11. **Tests live in `web/src/` too** — the SPA has a vitest + testing-library suite (`npm run test`, ~150 files) alongside the Go-side tests (`web/embed_test.go`, `internal/server/handler_sse_test.go`). Add a component/store test next to the code you change; `npm run typecheck` and `npm run build` must both stay green.

12. **Session id routing** — The `/session/:id` route in react-router and the `sessionId` in `ChatState` must match. `SessionPage` reads `:id` from the URL params and dispatches `SET_SESSION_ID`. URL changes trigger a full session switch.

13. **SSE `status` event format** — The `status` event (via the unified event bus) carries a JSON blob that populates `tuiStatus` in chatStore. It mirrors the TUI's status rendering (model, agent, thinking state, spinner, spending). The web UI renders it in the StatusBar and CoworkSidebar. If the TUI adds a new status field, the web UI typing in `types.ts` needs updating.

14. **Permission flow** — When the server's agent needs a permission grant, it sends a `permission` SSE event. `PermissionDialog` shows the command and complete `args` in a formatted, wrapped, scrollable “Tool execution parameters” section (no truncation), captures user choice, and sends the result via `POST /api/permissions/resolve`. Full arguments are forwarded by both server-owned and TUI RC events and preserved through live-state and transcript recovery. Legacy requests without `args` retain the command-only display. The SSE then continues with the resolved tool call.

15. **Tabs are keep-alive — never unmount to "switch"** — every visited session tab (across **all** projects) stays mounted and is toggled with `hidden` CSS: the chat surfaces via `allChatTabs.map` + `visitedTabsRef`, the terminals via `terminalProjectPaths.map` in `App.tsx`. The `UnifiedTabBar` is a drag-and-drop sortable tab strip (`@dnd-kit`) with multi-row wrap overflow. That is what lets a background tab keep streaming and a pty/WebSocket survive a switch. Anything that unmounts on switch (e.g. rendering only the active project's surfaces, or a `key` that changes per tab) breaks streaming and terminal state — do not "simplify" it.

16. **Memo on the per-tab surfaces requires the *dispatch-only* project hook** — the hot per-tab children (`ChatPanel`, `ChatInput`, `ChangesPanel`, `LogPanel`, `AgentsPanel`, `PreviewTabPage`) are `React.memo`'d so a tab switch does not re-render every hidden tab. That only works if they do **not** call `useProjectState()`: the provider's context value is recreated on every dispatch, so any consumer of it re-renders on every tab/project switch and memo is defeated. Use `useProjectDispatch()` (stable identity) when a component only fires project actions, and pass per-project data (e.g. `projectPath`) as a prop. Passing an inline arrow/array prop to a memo'd child also defeats it — hoist with `useCallback`/`useMemo`, or use the trampoline pattern in `App.tsx` (`handleCommandRef` + `stableHandleCommand`) when the underlying closure must stay fresh.

17. **Heavy viewers must stay code-split (`React.lazy`)** — `PreviewSurface` lazily loads `PdfViewer`/`DocxViewer`/`PptxViewer`/`ExcelViewer`/`MmdViewer`/`MarkdownViewer`/`TextViewer`, and `FileTabContent` lazily loads `FileEditor` (Monaco). Together these were ~4.8 MB of the old 6.7 MB entry chunk (Monaco, pdf.js, xlsx, docx-preview, mermaid). Never re-introduce a **static** import of those modules on the entry path (`App.tsx` → `FileTabContent`/`PreviewSurface`): a single static import puts the whole dependency back in the initial bundle. Type-only imports are fine (they erase). Tests that render a lazy surface must `await waitFor(...)` — the first paint is the Suspense fallback.

18. **Speech reads RENDERED text, never the markdown source** — the per-message `Speak` button (`AssistantText`), the `at-bottom` auto-speak (`ChatPanel`'s `ocode:assistant-complete` dispatch), and `Speak visible` all extract from the DOM via `renderedSpeechText` / `renderedSpeechTexts` / `lastRenderedSpeechText` in `web/src/components/Speech/speechUtils.ts`, not from `message.content`. Speaking the source reads heading hashes, `**` markers, backticks and link targets aloud, and `Element.textContent` alone merges blocks (`<p>one</p><p>two</p>` → "onetwo") — the extractor inserts breaks at block tags and skips `[data-speech-exclude]`/`aria-hidden` chrome. The rendered markdown subtree carries `data-speech-content`; the Speak button is a **sibling** of that subtree (a control inside it would have its own label read aloud). Any new speak surface must extract rendered text too — a markdown-source stripper silently drifts from what is on screen. Thinking blocks and terminal selections are plain text (not markdown) and are passed through unchanged.

19. **Terminal is a project-level top tab, not a session sub-tab** — `TerminalTabs` renders at the project level (beside Sessions in `App.tsx`), not inside `SessionSubTabs`. The terminal store (`terminalStore.tsx`) is per-project, not per-session. Terminal instances persist via `terminalPersistence.ts` and survive session switches. `ProcessesPanel` shows running background processes for the active terminal. Terminal scrollback is persisted via `terminalHistory.ts` and restored on reconnect.

20. **Browser tabs live in `browserTabsStore`** — The embedded browser panel (`BrowserPanel`) uses its own store (`browserTabsStore.tsx`) separate from session tabs. Browser tabs are keyed by project path and tracked with `browserPersistence.ts`. `ChromeViewport` renders pages via Chrome DevTools Protocol (`cdpProtocol.ts`, `useCdpSocket.ts`). The browser panel is **disabled in remote sessions** (`isRemoteSession()` check in `BrowserPanel.tsx`).

21. **Remote sessions use URL-fragment auth** — Remote-mode sessions bootstrap the auth token from the URL fragment (`#token=...`) via `App.tsx`'s `bootstrapRemoteAuthToken()`. The `RemoteReconnect` component shows a minimal reconnect page when a remote session has no valid token. `ShareDialog` disables the desktop-share link in remote sessions.

22. **`ActiveView` is `"files" | "git" | "cron" | "assets" | "sessions" | "settings"`** — The top-level view state (which TopTabs panel is active) is persisted per project via `viewPersistence.ts`. `FocusedKind` is `"chat" | "terminal" | "browser"` and tracks which content type has focus within the sessions view.

23. **Markdown tabs have an Edit / Preview / Split mode switch, Edit by default** — `.md`/`.markdown` stay **editable** (they are deliberately NOT in `PREVIEW_ONLY_KINDS`), so `FileTabContent` owns a mode group (`role="group"` + `aria-pressed` buttons, matching `FileTree`'s view-mode switch) instead of routing them to a read-only surface: `edit` (default), `preview` (full-width `PreviewSurface kind="markdown"`), `split` (Monaco left, preview right). The Monaco pane is **hidden with CSS, never unmounted**, so cursor/scroll/undo survive a trip through the preview — and `FileEditor` sets `automaticLayout: true` because the pane width changes (split drag, file-tree resize) and Monaco otherwise paints against stale geometry. Split mode feeds the editor's live source into the preview via `PreviewSurface`'s optional `content` prop → `MarkdownViewer` **controlled mode** (`content !== undefined` ⇒ skip the `/api/files/content` fetch), debounced 200ms so a typing burst does not re-parse react-markdown every keystroke. The divider (`role="separator"`) comes from `useResizableSplit` (ratio-based, persisted at `ocode.ui.split_ratio`, double-click resets); mode is per-tab component state and survives tab switches via keep-alive. The mode state must stay in `FileTabContent` — not in `FileEditor` (reused by `TextViewer`/`MarkdownViewer`) and not in `MarkdownViewer` (it imports `FileEditor`, so split logic there would be circular).
