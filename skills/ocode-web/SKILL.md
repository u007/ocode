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
├── package.json            # React 19, react-router-dom 7, Monaco, Radix, lucide, cmdk, xterm 6, @dnd-kit, @tanstack/react-store, shiki, mermaid, pdfjs-dist, xlsx
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
    │   ├── projectGitCounts.ts # Per-project git changed-file counts (project-list badges)
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
    │   ├── useKeyboard.ts       # Global shortcuts (⌘K palette, ⌘N new chat, ⌘T terminal, Escape)
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
        │   ├── CoworkSidebar.tsx   # Right sidebar: model, agent, context, permissions, MCP, git, LSP, plugins
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
        │   └── GitPanel.tsx        # SourceTree-style: staged/unstaged panes, hunk actions, commit log, commit box (Commit + Commit & Push)
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
        │   ├── VaultForm.tsx       # Password vault (server-side store)
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
        │   ├── StatusBar.tsx       # Bottom bar: tokens, model, session, context, spending; collapsible to a slim row
        │   ├── statusBarCollapse.ts # Persisted collapsed state (localStorage + same/cross-window sync)
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
- **Frontend-only** (don't hit the agent): `/help`, `/commands`, `/clear`, `/export` (generates Markdown + triggers browser download), `/export-claude` (appends to Claude history)
- **API-call**: `/session`, `/ocr`, `/mask`, `/compact`, `/recap`, `/share`, `/btw`, `/sandbox` — call backend endpoints directly
- **Model/agent switches**: `/model`, `/small-model`, `/advisor`, `/explorer-model`, `/context-model` — dispatched via API (a bare `/x-model model` returns `openModelPicker` + `modelPickerPurpose`, which `App.tsx` turns into a `ModelDialog` open)
- **Config**: `/fake-agent`, `/editor`, `/editor-mode`, `/themes` — read/write the same REST endpoints as the Settings forms
- **In-chat find**: `/search`, `/find` — dispatch a `ocode:open-chat-search` window CustomEvent (`{query}`); `ChatPanel` listens and opens its find bar with the same tab-visibility guard as `Ctrl/Cmd+F`. The find bar searches the **whole transcript**, not just the loaded window: a debounced `api.searchSession(id, q, {}, host)` hits `GET /api/sessions/{id}/search` and returns matching **server indices**; `web/src/lib/sessionSearch.ts` translates them to render-entry positions via the store's `windowStartServerIndex`. Jumping to an off-window hit fetches the contiguous prefix (`{limit: windowStart - matchIndex, offset: messages.length}`) and `PREPEND_MESSAGES` is cap-exempt, so `MAX_SLICE_MESSAGES` (400) is never raised. A `"N total, M in view"` note appears only when hits sit outside the window; a draft tab (`new-*`) skips the server query and a failure falls back to the instant local matcher.
- **CLI utilities**: `/tools`, `/tool` — `GET /api/cli-tools` lists catalog tools with PATH-detected status; `/tools <name>` starts `POST /api/cli-tools/install` (202 + `job_id`) and polls `GET /api/cli-tools/install/{id}`. Install can take **minutes**, so the handler returns immediately and reports the outcome via `ctx.notify` (mapped in `App.tsx` to `ADD_MESSAGE`). Remote tabs route through `/api/remote/<host>/…`, so the probe runs on the remote host's PATH.
- **Computer-use**: `/computer` — show/enable/disable computer-use status
- **TUI-only** (no web equivalent): `/ide`, `/secret`, `/sidebar`, `/details`, `/sound`, `/rc`, `/exit` — these return an explanatory message naming the web alternative. They must NOT fall through to the LLM: a picker entry without a dispatch case silently becomes a chat message (this is how `/fake-agent` was reported "missing").
- **MCP OAuth**: `/mcp-auth <server>` — `POST /api/mcp/{name}/auth` starts the flow (202 + `job_id`), then polls `GET /api/mcp/auth/{id}` (`running`→`done`/`error`). The operation **refuses non-loopback callers with 403**: the flow launches a system browser and listens on the server's `127.0.0.1:8085`, so it only works when the browser and the server share a machine (desktop app / local web). A remote SSH session is refused **by the local server before it even connects** (the remote cannot tell: the `ssh -L` tunnel makes its peer loopback), so the fast 403 applies there too — WSL is exempt because its server shares the Windows machine. The command returns immediately and reports the outcome via `ctx.notify`. That is a real 403, not a "TUI-only" stub. **Never tell a web/desktop user to run a command "in the desktop shell"** — the desktop app renders this same SPA, so that advice loops.
- **Subcommands**: a command present in the picker can still silently ignore its arguments. `/recap`, `/agents`, `/compact`, `/init`, `/cron` all take subcommands/arguments in the TUI and must forward `args` in their dispatch case (a bare `return handleX(ctx)` dropped them). Tested by `commands.subcommands.test.tsx`.
- **Unknown commands**: displayed in UI but fall through to the next agent turn as plain messages

**Parity guard.** `web/src/components/Chat/commands.tuiParity.test.tsx` iterates the real `COMMANDS` array and asserts every entry dispatches (`handled: true`) except `/new` and `/clear`, which `App.tsx` intercepts before `dispatchCommand`. When adding a command, add both the `COMMANDS` entry and a `case` — the test fails otherwise. TUI-side reference list: `internal/tui/commands.go`'s `commandSpecs`.

## 10. Global Shortcuts

Wired in `useKeyboard.ts`, registered in `App.tsx` top-level `useEffect`:

| Shortcut | Action |
|----------|--------|
| `⌘K` / `Ctrl+K` | Open CommandPalette |
| `⌘P` / `Ctrl+P` | Open FilePicker |
| `⌘S` / `Ctrl+S` | Save the active editor tab — the same action as the editor header's **Save** button (the touch path; see gotcha 26h) |
| `⌘N` / `Ctrl+N` | New chat — reveals the Sessions view on the chat half, then opens (or reuses the blank) chat tab |
| `⌘T` / `Ctrl+T` | New terminal on the Sessions view; the same new-chat action on any other view |
| `⌘W` / `Ctrl+W` | Close the frontmost tab (browser/terminal/editor/chat) — **desktop shell only**; `Ctrl+W` is passed through inside `.xterm` (readline word-delete) |
| `Escape` | Close the command palette / file picker (other dialogs handle their own Esc) |

`⌘,` / `Ctrl+,` opens **Settings** — it is bound by the desktop native menu
(`cmd/ocode-desktop/main.go` `buildAppMenu`), not by `useKeyboard.ts`, so it
does nothing in a plain browser tab. There is no global CoworkSidebar
shortcut.

`⌘N` and `⌘T` are `useKeyboard`-only (no native menu accelerators; the Edit
menu is `menu.AddRole(application.EditMenu)` and claims none of them). In a
plain browser tab they are best-effort: `⌘N` is a new window and `⌘T` a new tab
— both on Chrome's **non-overridable** list — so outside the desktop shell the
reliable entry point is the tab bar buttons. `App.tsx` shares one `openNewChat`
helper for both keys' chat path so the view switch cannot drift between them.

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

13. **SSE `status` event format** — The `status` event (via the unified event bus) carries a JSON blob that populates `tuiStatus` in chatStore. It mirrors the TUI's status rendering (model, agent, thinking state, spinner, spending). The web UI renders it in the StatusBar and CoworkSidebar. If the TUI adds a new status field, the web UI typing in `types.ts` needs updating. **Per-session token totals** (`input_tokens`/`output_tokens`/`cached_tokens`/`total_tokens`) are populated for headless web/desktop sessions by the server's `applySessionUsage` (`internal/server/handler_session_state.go`): live agent atomics → persisted session metadata (the same keys the TUI's `sidebarTelemetry` writes) → else absent. The CoworkSidebar Context section shows an Input/Cached/Output breakdown (independent of the context gauge, so a restored session with no provider reading still shows its totals); the Cached row carries the cache hit % (`cached ÷ (input+cached)`, mirroring the TUI's `Cache <n> (<pct>%)` line), and there is deliberately no billed Total row. The StatusBar row 2 shows `in … · cache … · out …`; both are hidden when all counts are 0 (never a fabricated 0). Tokens persist at turn end via `persistSessionTelemetry`; `agent.Message.Usage` is `json:"-"`, so the transcript cannot be used to reconstruct them — metadata is the only cross-restart source. **Live agent-loop activity arrives on a SEPARATE `agent_activity` event, not on `status`.** `status` is a whole-snapshot REPLACE (`SET_TUI_STATUS`), so it cannot carry an activity-only update; `agent_activity` carries just `llm_running` / `active_tools` / `active_agents` and the reducer MERGES them (`SET_AGENT_ACTIVITY`). The server publishes it per turn from `agent.Activity().Notify()` (`startAgentActivityBroadcast`), i.e. **headless web/desktop only** — with an RC bridge attached the TUI owns that single-consumer channel and publishes the same fields through complete `status` snapshots instead. Both sources write the same `TUIStatus` field names on purpose, so `StatusBar.runningStatusParts` renders either identically. Rules when touching this: (a) never add a new field to `agent_activity` without also adding it to `AgentActivityEvent` in `internal/server/tui_status.go` AND `AgentActivityEvent` in `api/types.ts`; (b) an event added to `SESSION_SCOPED_EVENTS` is automatically subscribed (`ROUTABLE_EVENTS` derives from it) and routed to a tracked session's slice, but it is NOT automatically replayed — `agent_activity` is deliberately excluded from the server's `liveFrameEvents` because replaying a momentary reading would show a stale `⟳ llm` for a tool that already finished; (c) the three fields are cleared on the turn boundary (`SET_TURN_STATE` → `turnActive:false`) so a late frame can't park a dead indicator that the next turn would inherit.

14. **Permission flow** — When the server's agent needs a permission grant, it sends a `permission` SSE event. `PermissionDialog` shows the command and complete `args` in a formatted, wrapped, scrollable “Tool execution parameters” section (no truncation), captures user choice, and sends the result via `POST /api/permissions/resolve`. Full arguments are forwarded by both server-owned and TUI RC events and preserved through live-state and transcript recovery. Legacy requests without `args` retain the command-only display. The SSE then continues with the resolved tool call.

15. **Ask dialogs (`PermissionDialog`, `QuestionDialog`) must cap to the visible viewport** — both are Radix dialogs centred with `top-1/2 translate-y-[-50%]`, so without a height cap tall content paints off **both** edges with no scrollbar. They use the shared `.dialog-viewport-max` utility (`web/src/index.css`, inside `@layer utilities`) plus `overflow-y-auto`. The utility deliberately declares `100vh` in one rule and upgrades to `100dvh` in an `@supports (height:100dvh)` block — **do not** merge them into one rule with two `max-height` declarations: the CSS minifier drops the earlier one, leaving browsers without `dvh` support with no cap at all. `QuestionDialog` additionally lays out as `flex flex-col overflow-hidden` with the header/footer `shrink-0` and a `flex-1 min-h-0 overflow-y-auto` body, so the Cancel/Submit row is pinned (never scrolls out of reach) on a long prompt — mirror `SessionDialog`/`PluginsPanel` for any new ask dialog rather than relying on whole-dialog scroll alone.

16. **Tabs are keep-alive — never unmount to "switch"** — every visited session tab (across **all** projects) stays mounted and is toggled with `hidden` CSS: the chat surfaces via `allChatTabs.map` + `visitedTabsRef`, the terminals via `terminalProjectPaths.map` in `App.tsx`. The `UnifiedTabBar` is a drag-and-drop sortable tab strip (`@dnd-kit`) with multi-row wrap overflow. That is what lets a background tab keep streaming and a pty/WebSocket survive a switch. Anything that unmounts on switch (e.g. rendering only the active project's surfaces, or a `key` that changes per tab) breaks streaming and terminal state — do not "simplify" it.

17. **Memo on the per-tab surfaces requires the *dispatch-only* project hook** — the hot per-tab children (`ChatPanel`, `ChatInput`, `ChangesPanel`, `LogPanel`, `AgentsPanel`, `PreviewTabPage`) are `React.memo`'d so a tab switch does not re-render every hidden tab. That only works if they do **not** call `useProjectState()`: the provider's context value is recreated on every dispatch, so any consumer of it re-renders on every tab/project switch and memo is defeated. Use `useProjectDispatch()` (stable identity) when a component only fires project actions, and pass per-project data (e.g. `projectPath`) as a prop. Passing an inline arrow/array prop to a memo'd child also defeats it — hoist with `useCallback`/`useMemo`, or use the trampoline pattern in `App.tsx` (`handleCommandRef` + `stableHandleCommand`) when the underlying closure must stay fresh.

18. **Heavy viewers must stay code-split (`React.lazy`)** — `PreviewSurface` lazily loads `PdfViewer`/`DocxViewer`/`PptxViewer`/`ExcelViewer`/`MmdViewer`/`MarkdownViewer`/`TextViewer`, and `FileTabContent` lazily loads `FileEditor` (Monaco). Together these were ~4.8 MB of the old 6.7 MB entry chunk (Monaco, pdf.js, xlsx, docx-preview, mermaid). Never re-introduce a **static** import of those modules on the entry path (`App.tsx` → `FileTabContent`/`PreviewSurface`): a single static import puts the whole dependency back in the initial bundle. Type-only imports are fine (they erase). Tests that render a lazy surface must `await waitFor(...)` — the first paint is the Suspense fallback.

19. **Speech reads RENDERED text, never the markdown source** — the per-message `Speak` button (`AssistantText`), the `at-bottom` auto-speak (`ChatPanel`'s `ocode:assistant-complete` dispatch), and `Speak visible` all extract from the DOM via `renderedSpeechText` / `renderedSpeechTexts` / `lastRenderedSpeechText` in `web/src/components/Speech/speechUtils.ts`, not from `message.content`. Speaking the source reads heading hashes, `**` markers, backticks and link targets aloud, and `Element.textContent` alone merges blocks (`<p>one</p><p>two</p>` → "onetwo") — the extractor inserts breaks at block tags and skips `[data-speech-exclude]`/`aria-hidden` chrome. The rendered markdown subtree carries `data-speech-content`; the Speak button is a **sibling** of that subtree (a control inside it would have its own label read aloud). Any new speak surface must extract rendered text too — a markdown-source stripper silently drifts from what is on screen. Thinking blocks and terminal selections are plain text (not markdown) and are passed through unchanged.

20. **Terminal is a project-level top tab, not a session sub-tab** — `TerminalTabs` renders at the project level (beside Sessions in `App.tsx`), not inside `SessionSubTabs`. The terminal store (`terminalStore.tsx`) is per-project, not per-session. Terminal instances persist via `terminalPersistence.ts` and survive session switches. `ProcessesPanel` shows running background processes for the active terminal. Terminal scrollback is persisted via `terminalHistory.ts` and restored on reconnect. **Wheel gestures over the terminal must never scroll the app chrome**: xterm consumes the wheels it can use (its own scrollback, or a mouse report forwarded to the TUI that owns the mouse — e.g. Claude Code's fullscreen/alternate-screen renderer) and stops propagation when it does, so `TerminalPanel`'s container-level non-passive `onWheelGuard` only ever sees gestures xterm ignored (scrollback at an edge, a mouse report the app did not answer) and calls `preventDefault()` on those. Without it the leftover gesture scrolls the nearest scrollable ancestor, and in the desktop shell's WKWebView even the non-scrollable document rubber-bands — reported as "cannot scroll the claude code that run inside the terminal, only can scroll the window". The guard preserves the container's own `overflow-y-auto` fallback (it only swallows when the container cannot scroll in that direction), and the container carries `overscroll-contain` while `index.css` sets `overscroll-behavior: none` on `html, body` to kill the document-level bounce. Regression: `TerminalPanel.wheelGuard.test.tsx`.

21. **Browser tabs live in `browserTabsStore`** — The embedded browser panel (`BrowserPanel`) uses its own store (`browserTabsStore.tsx`) separate from session tabs. Browser tabs are keyed by project path and tracked with `browserPersistence.ts`. `ChromeViewport` renders pages via Chrome DevTools Protocol (`cdpProtocol.ts`, `useCdpSocket.ts`). The browser panel is **disabled in remote sessions** (`isRemoteSession()` check in `BrowserPanel.tsx`).

22. **Remote sessions use URL-fragment auth** — Remote-mode sessions bootstrap the auth token from the URL fragment (`#token=...`) via `App.tsx`'s `bootstrapRemoteAuthToken()`. The `RemoteReconnect` component shows a minimal reconnect page when a remote session has no valid token. `ShareDialog` disables the desktop-share link in remote sessions.

23. **`ActiveView` is `"files" | "git" | "cron" | "assets" | "sessions" | "settings"`** — The top-level view state (which TopTabs panel is active) is persisted per project via `viewPersistence.ts`. `FocusedKind` is `"chat" | "terminal" | "browser"` and tracks which content type has focus within the sessions view.

24. **Markdown tabs have an Edit / Preview / Split mode switch, Edit by default** — `.md`/`.markdown`/`.mdx` stay **editable** (they are deliberately NOT in `PREVIEW_ONLY_KINDS`), so `FileTabContent` owns a mode group (`role="group"` + `aria-pressed` buttons, matching `FileTree`'s view-mode switch) instead of routing them to a read-only surface: `edit` (default), `preview` (full-width `PreviewSurface kind="markdown"`), `split` (Monaco left, preview right). The Monaco pane is **hidden with CSS, never unmounted**, so cursor/scroll/undo survive a trip through the preview — and `FileEditor` sets `automaticLayout: true` because the pane width changes (split drag, file-tree resize) and Monaco otherwise paints against stale geometry. Split mode feeds the editor's live source into the preview via `PreviewSurface`'s optional `content` prop → `MarkdownViewer` **controlled mode** (`content !== undefined` ⇒ skip the `/api/files/content` fetch), debounced 200ms so a typing burst does not re-parse react-markdown every keystroke. The divider (`role="separator"`) comes from `useResizableSplit` (ratio-based, persisted at `ocode.ui.split_ratio`, double-click resets); mode is per-tab component state and survives tab switches via keep-alive. The mode state must stay in `FileTabContent` — not in `FileEditor` (reused by `TextViewer`/`MarkdownViewer`) and not in `MarkdownViewer` (it imports `FileEditor`, so split logic there would be circular). **MDX (`.mdx`) rides the same path** (`kindByExt[".mdx"] → "markdown"`), with two deliberate rules: the **preview renders it as Markdown** — `react-markdown` does NOT evaluate MDX, so ESM `import`/`export` lines and JSX components show as source-level text (evaluating MDX would execute arbitrary JavaScript from a previewed file) — while the **editor highlights it with Monaco's bundled `mdx` grammar** (Markdown + JSX, registered by `monaco-editor`'s basic-languages contribution; `.mdx` → `mdx`, `.md`/`.markdown` → `markdown`). The extension→language table lives in `web/src/lib/editorLanguage.ts` (`languageForFile`), shared by `FileEditor` and `TextViewer` so the two cannot drift; the server mirrors the extension in `HandleFileRaw.previewRawTypes` (`text/markdown; charset=utf-8`) and `preview_open`'s `previewOpenKinds` (`text`) — keep all three in sync.

25. **OS-native file-manager reveal is local-only and server-labelled** — the Files-tab context menus (tree rows *and* Miller-column rows) offer "Open in Finder" (directory) / "Show in Finder" (file) via `api.revealInFileManager` → `POST /api/files/open {mode:"reveal"}`. Two rules: (a) `reveal` is the **only** open mode that accepts a directory (`editor`/`os` 404 it), and the per-platform argv lives in `internal/server/reveal.go` (`open -R` on darwin, `explorer /select,` on windows, `dbus-send …FileManager1.ShowItems` on linux with an `xdg-open` parent-folder fallback). (b) The action is **hidden for remote projects** (`canReveal: !projectHost`) because there is no remote branch — the server would reveal an unrelated path on the *local* machine. The menu noun comes from `GET /api/config/ocode/paths.platform` (the server's GOOS), never `navigator.platform`, which describes the browser, not the machine running the command.

26. **Mobile (≤767px) is an overlay layout, not a squeezed desktop one** — `useIsMobile()` (`max-width: 767px`) drives it. Two rails that are inline flex columns on desktop become **fixed off-canvas drawers with a scrim** on phones: `ProjectSidebar` (left, `isMobile` prop) and `CoworkSidebar` (right, its own mobile branch). Rules when touching this: (a) a drawer is *always mounted* and slid with `translate-x`/`-translate-x-full` so the open/close animation works; the desktop collapsed rail (`w-10`) is skipped on mobile. (b) In `App.tsx` the drawer's flex wrapper must NOT reserve its desktop width on mobile — `CoworkSidebar`'s wrapper is `isMobile ? "" : "w-72 flex-shrink-0 …"`, otherwise the empty `w-72` box eats 288px of the row even though the `aside` is `position: fixed`, and `main` measured 0px wide. (c) `sidebarOpen`/`coworkOpen` **lazy-init from `window.innerWidth >= 768`**; the media-query listener only fires on a breakpoint *change*, so a direct load at ≤767px would otherwise leave both rails open. (d) The only mobile trigger for the project drawer is the `PanelLeft` button in `TopTabs` (`onMenuToggle`, `md:hidden`; hidden when the prop is absent), and selecting a project auto-dismisses the drawer. (e) `UnifiedTabBar` collapses below `lg` (<1024px — phones **and** tablets): the tab strip becomes a single **dropdown** whose closed trigger shows the ACTIVE tab (emoji + title + chevron, `data-testid="mobile-tab-dropdown-trigger"`) and whose open list has full pill parity (pending dot, chat running/stalled turn badge, terminal unread bell, close X through the same confirm dialog, rename via double-click, Processes pseudo-tab row). Chat pills (and the dropdown trigger/rows) carry a fixed-size live turn-status slot (`data-testid="tab-turn-state"`, `data-state` = `running`|`stalled`|`idle`) — a blue spinner while the turn is in flight, an amber pause when the heartbeat stalled; it mirrors the project sidebar's streaming/stalled badges and is derived per chat slice (`deriveTurnState`) so background tabs show it too. The always-rendered slot avoids pill-width reflow when the state flips (same reason the pending dot is a transparent placeholder when idle). The new-chat/new-terminal/new-browser/Processes/All-sessions buttons stay on the RIGHT of the same row (`shrink-0`, single cluster shared by both layouts — the actions JSX is rendered once). ≥`lg` restores the original two-column grid (`lg:grid-cols-[minmax(0,1fr)_auto]`) with 208px drag-reorder pills (`w-full lg:w-52`) — the old fixed 208px pills painted over the action-button column once the rails squeezed the centre. Both branches stay mounted (selection via the same activation handlers ⇒ keep-alive surfaces are switched, never unmounted). `MobileTabDropdown` lives in `UnifiedTabBar.tsx`; entries come from `tabEntries` built alongside the pill `renderPill` (keep them in lockstep). (f) The floating bottom bar is **`SpeechToolbar`** (not the inline `StatusBar`): phones render it full-width and wrapping (`inset-x-2 flex-wrap`, ≥sm restores the centered `left-1/2 -translate-x-1/2 sm:flex-nowrap` pill) — the old nowrap row overflowed a 390px viewport and clipped its error/Retry. (g) The right-hand **Browser / Preview side pane is desktop-only**: `shouldRenderSidePane` (`lib/sidePaneVisibility.ts`) returns false on mobile, so `App.tsx` never renders the pane (a fixed-width `flex-shrink-0` child would overflow the phone row) and hides its 🌐 toggle. Browser + preview stay reachable **as tabs** — the `UnifiedTabBar` browser pills / "New browser tab" button, and the session **Preview sub-tab** — and a preview activation (AI `preview_open` tool / file-tree "Preview in sidebar") is routed by App to the Preview sub-tab (`PreviewTabPage` consumes `request`/`nonce`/`onConsumeActivation`) instead of the pane. Regression tests: `ProjectSidebar.test.tsx` ("mobile drawer"), `UnifiedTabBar.test.tsx` ("shows the tab dropdown on phones and keeps the pill grid from lg up" + dropdown item/pending/bell/close describes), `TopTabs.test.tsx`, `SpeechToolbar.layout.test.tsx`, `sidePaneVisibility.test.ts` ("never renders on mobile"), `App.previewActivation.test.tsx` ("App side pane on mobile"), `PreviewTabPage.activation.test.tsx`.
26h. **The Files-tab editor header carries the touch save path** — `FileEditor` renders a **Save** button (lucide `Save`, `aria-label="Save file"`) whenever `onSave` is provided, enabled only when `dirty`; touch devices have no `⌘/Ctrl+S`, so this is the only way to save an edited file on a phone (the shortcut still works on desktop). App wires it per tab: `dirty={et.isDirty}` + `onSave={() => void saveEditorTab(et.id).catch((e) => reportActionError(e, "Save file"))}` (`reportActionError` feeds the existing `ActionErrorToast`; a 409 also sets the in-editor conflict banner). Read-only surfaces (`TextViewer`/`MarkdownViewer`) pass no `onSave`, so no button renders there. The header path is `truncate min-w-0`, the action cluster `shrink-0`, and the "Settings" label is `hidden sm:inline` — the Save button is placed FIRST in the cluster so it stays visible when space is tight (verified visible at 360/390px with the 260px file tree both expanded and collapsed; at ≤320px with the tree expanded the editor pane itself is only ~60px, so collapse the tree). `onSave` is forwarded through an `onSaveRef` and `dirty` is compared in `arePropsEqual` (memoization must not go stale). Regression: `FileEditor.save.test.tsx` (disabled/enabled/absent) + `App.editorTabScope.test.tsx` ("wires a Save handler and the dirty flag into each editor pane").

27. **Out-of-tree view focus goes through `lib/tabFocus.ts` and is applied by a PASSIVE effect** — `activeView`/`focusedKind` are `HomeApp` state, so a component outside its tree cannot switch the view itself. The remote project's sidebar inventory (`RemoteProjectStatus`, Chats/Terminals) is the case in point: it calls `tabFocusActions.request({kind, projectPath, host?, terminalId?})` (`web/src/lib/tabFocus.ts`, a module-level TanStack store, no provider) and `HomeApp` consumes the queue. The consumer **must be a passive `useEffect`, not a layout effect**: the per-project view restore (`loadViewStateForProject`) is a layout effect, so a request that arrives in the same commit as a project switch has to run *after* it to win — and the effect must gate on `projectPath === activeProject.path` (staying queued otherwise) so it can't apply against the outgoing project. Opening a session from a NON-active project must both `selectProject` it and bind the tab to it (`openSessionTab(id, title, projectPath)`) — a tab bound to the active project routes the remote session through the local server (`resolveSessionHost`). On mobile, a nested row control that `stopPropagation()`s (the inventory rows) opts out of the row's `onSelect`, which is the only drawer-dismiss path: thread an `onRevealTab` callback to dismiss the drawer, and make the control's own expander `stopPropagation()` too (the status line originally didn't, so tapping it selected the project and closed the drawer before the list could be used). Regression: `App.tabFocusRemote.test.tsx` (real App + real sidebar, one click on a non-active remote project's chat overrides that project's persisted Files view with Sessions), plus `App.tabFocus.test.tsx` / `tabFocus.test.ts` / `RemoteProjectStatus.test.tsx` / `ProjectSidebar.test.tsx`.

28. **Session-bound dialogs mount only on that session's Chat surface** — a pending permission/question ask is a per-session store value (`pendingPermission`/`pendingQuestion` in `chatStore`), but `activeTabId` (`projectStore.tsx` `activeTabId()`) tracks the active project's tab independently of `activeView`, `focusedKind` and the session sub-tab. Mounting `PermissionDialog`/`QuestionDialog` at the App root from `useChat(activeTabId)` therefore opened a full-screen Radix modal (`DialogPortal` + `fixed inset-0` overlay + focus trap) over the Files/Git/Cron/Assets/Settings view, the terminal half, or a non-Chat sub-tab of the same session — blocking the whole app for a session the user was not looking at. Gate every chat-session-bound dialog on `sessionAskSurfaceVisible({ activeView, focusedKind, activeSubTab })` (`web/src/lib/dialogScope.ts`), true only for `sessions` + `chat` + `chat`; `App.tsx` computes `sessionAskVisible` from `activeSessionTab?.activeSubTab` and gates with `{pendingPermission && sessionAskVisible && …}` (same for the question ask). The ask is not lost: it stays in its per-session slice and re-opens when the user returns to that session's Chat sub-tab; the project-sidebar Bell `pendingCount` badge and the `AttentionSoundBridge` chime are the off-surface signal. Regression: `web/src/lib/dialogScope.test.ts` (predicate matrix) + `web/src/App.askDialogScope.test.tsx` (mount/no-mount/re-open through the real App). Documented in `docs/concepts/session-bound-dialog-scoping.md`.

29. **The project list shows a per-project git changed-files badge** — `useProjectIndicators` (`ProjectSidebar.tsx`) folds in `useProjectGitCounts(projectPath, host, enabled)` from `web/src/lib/projectGitCounts.ts`: a module-level store keyed `host\0project` so the expanded row and the collapsed rail of one project share a single `GET /api/git/status` (initial fetch on first subscriber, then a 60s poll while a row is mounted — a directory that answers `is_repo:false` is re-probed only every 5 min — plus an instant refresh on a `git_status` bus event naming that project). Only projects with an **open tab** are declared "viewed" (`App.tsx` `eventBus.setProjects`), which is why the store fetches instead of waiting for the server push. Three rules that must not regress: (a) the `enabled` flag is `!host || hostStatus.status?.connected` — `GET /api/git/status?host=` is the **cold-connect path**, so rendering an unconnected remote row must never dial it (same guard as the hover prefetch); while disabled the hook snapshots `NO_GIT_COUNTS`, which is also what clears a stale badge when a host drops. (b) the `git_status` envelope carries **no host**, so its payload is never applied directly — the entry re-fetches through its own host (the same path can exist on two machines). (c) the badge shows `staged_files.length + changed_files.length`, the SAME total as the Git tab badge, and is informational: it never joins the attention Bell total; a failed refresh keeps the last known counts so one transient error cannot flash every badge to zero. Regression: `web/src/lib/projectGitCounts.test.ts` (fetch/dedupe/host-keying/event refresh/enabled gate/error-keeps-last/timer teardown/non-repo backoff) + `ProjectSidebar.test.tsx` ("shows the git changed-files badge", "renders no git badge when the project has no changes", "asks for git counts only when the project's remote host is connected", collapsed-rail "shows the git changed-file count").

30. **The composer Retry re-runs the last turn IN PLACE (never duplicates the user message)** — `ChatInput` shows a `RotateCcw` icon beside the input when `!busy && (wasInterrupted || turnError)`; clicking it calls `useChat.retryLastTurn` → `api.retrySession(id, host)` → `POST /api/sessions/{id}/retry`. The reason a plain `sendMessage` won't do: `runTurn` (`internal/server/agent_session.go`) **always** appends the user message, so re-sending the text would create a second user row. Server side, `turnOptions.retryLast` makes `runTurn` skip both the user-row append and the `user_message` SSE echo and step the existing transcript tail (mirrors the TUI's Ctrl+Y `retryLastLLMError`), and `executeTurnJob` skips `persistUserMessage`/`PushPending` and runs the single in-place turn; `HandleRetrySession` (`internal/server/handler_retry.go`) 409s on an active turn (or a held agent lock) and on a transcript with no user row, and remote sessions reach it through `/api/remote/{host}`. Client-side the trigger is `SessionSlice.turnError`, **not** `error`: `turnError` is set only by the `turn_error`/headless `error` SSE frames and cleared by `turn_started`/`SET_ERROR(null)`, so a submit/validation failure (which sets `error` only) never offers Retry. Nuance: a retry does **not** rewind the transcript or revert tool side effects — if a full round already landed after a Stop, the model simply continues from that tail. Regression: `internal/server/handler_retry_test.go` (no duplicate user row in memory AND on disk, 409 active/nothing; the no-duplicate assertion mutation-verified) + web `ChatInput.retry.test.tsx`, `sessionEvents.test.ts`, `useChat.remoteHost.test.tsx`.
31. **The Browser / Preview side pane is scoped PER CHAT SESSION (open state and previewed file)** — the pane's `stateKey` is `side:chat:<sessionId>` (or `side:term:<terminalId>` for a focused terminal; `App.tsx` builds it from `activeTabId`) and lives in `browserStore` (`useBrowserStore(sideStateKey).panelOpen`). Opening the pane in one chat must NEVER open it in another, so `App.tsx` deliberately has **no** effect propagating `panelOpen` across a tab switch (an older effect did, plus a `panelClosedByUser` ref to counteract it — both removed). The AI `preview_open` activation (and the 🌐 toggle) opens the pane for the CURRENT session only. The pane's shell state (surface + previewed file/page) is persisted by `components/Preview/sidebarPreviewState.ts` keyed by the SAME stateKey, NOT by project: `STORAGE_KEY = "ocode.ui.sidebarPreview.v2"` (v1 was project-keyed `host::projectRoot`; those entries are intentionally orphaned). Because a chat tab's id is not stable — a brand-new tab starts as `new-<ts>` and is rekeyed to its real `ses_...` id on the first message, and `/reset-id` rekeys again — the side state must MOVE with the tab or the pane detaches and closes: `web/src/lib/sidePaneState.ts` `rekeySidePaneState(oldId,newId)` moves both the live `browserStore` surface (`browserActions.rekey`) and the persisted preview (`rekeySidebarPreviewState`), and is called from `App.rekeySession` plus `sessionEvents.ts` in the `session_started` and `session_rekeyed` handlers (alongside `rekeyDraft`/`rekeyQueue`/`rekeyInputHistory`). `PreviewHost` keeps its foreign-anchor containment guard (`docBelongsHere`) unchanged. `shouldRenderSidePane` (`lib/sidePaneVisibility.ts`) still gates rendering to a focused chat/terminal on the chat sub-tab, desktop only. Regression: `web/src/App.sidePaneScope.test.tsx` (open in s1 → switch to s2 closed → back to s1 open; mutation-verified), `PreviewHost.test.tsx` ("keeps each chat session's preview separate within the SAME project"; mutation-verified), `lib/sidePaneState.test.ts`, `components/Preview/sidebarPreviewState.test.ts`, `lib/browserStore.test.ts` (rekey).

32. **The chat sidebar's MCP toggle is PER CHAT and forces a scoped agent rebuild** — `CoworkSidebar.tsx`'s `MCP` section (collapsible, default collapsed) lists each configured server from `api.getMCP(host, sessionId)` and flips one with `api.setMCPEnabled(name, enabled, host, sessionId)`. Two hard rules: (a) always pass the active `sessionId` (undefined for a `new-*` draft) — the server records a per-session override and rebuilds ONLY that chat, so a toggle never disturbs other open chats; (b) the list is fetched with the same session scope so the switch shows THIS chat's state. Server: `HandleListMCP`/`HandleSetMCPEnabled` (`internal/server/handler_mcp.go`) accept `?session_id=`; the toggle still persists the process-wide `opencode.json` (matching `/mcp`, so new sessions inherit it) but `h.mcpSessionOverrides` wins for the toggling chat. Why the rebuild is needed: MCP tools are enumerated once into a process-wide `mcpCache` at boot, so without `rebuildAgentForMCP` a toggle would not reach any live agent. `mcpToolsForSession` re-enumerates from a CLONED effective config only when the session has overrides (override-less sessions keep the `mcpCache` fast path); `applyMCPSessionOverrides` must never mutate the shared `h.cfg.MCP`. A toggle during an active turn is deferred (debuglog `MCP` line; the next turn rebuilds). Regression: `internal/server/handler_mcp_session_test.go`, `web/src/components/Layout/CoworkSidebar.mcp.test.tsx`, `commands.hostScope.test.tsx` (session-id threading).
33. **The "All sessions" dialog (`SessionDialog`) lists MAIN sessions only and renders them in infinite-scroll pages** — two independent fixes for a project with thousands of sessions (measured: 6,646 sessions / 949 KB → the dialog mounted 6,650 row buttons in ~0.73 s). (a) **Child filtering**: subagent/child-context sessions are minted by the agent as `<parentID>_child_<agentName>_<ts>` (`internal/agent/child_session.go`), e.g. `ses_…_child_context_…`; they are execution detail, not resumable chats, so `projectSessions` is filtered through `isChildSessionId(id)` (`web/src/lib/sessionId.ts`, an `_child_` infix test kept in sync with the Go format) before search/render. (b) **Pagination**: only `SESSION_DIALOG_PAGE_SIZE` (50) rows render at first; `visibleCount` grows on an `IntersectionObserver` sentinel (`root: listRef`, `rootMargin: 240px`) with an explicit "Load more (N remaining)" button as the jsdom fallback (`typeof IntersectionObserver === "undefined"` guard — jsdom has none). `visibleCount` resets to page one on dialog open and on every search change; a background list revalidation only clamps it so the user keeps their place. The store cache (`projectStore.projectSessions`) and the unpaginated `GET /api/projects/sessions` are unchanged — the window is purely client-side, which keeps the warm cached list as an instant first paint and works against older remote servers. Verified in a real browser: popup 0.73 s → 0.11 s, first page 50 rows, `Load more (6153 remaining)` (50 + 6153 = 6,203 = 6,646 − 443 children exactly), search resets to page one. Regression: `web/src/components/Layout/SessionDialog.test.tsx` (child hidden; first page + Load more), `web/src/lib/sessionId.test.ts` — both mutation-verified (removing the filter/slice fails them). The 390 ms server scan (5,527 legacy `.json`/`.ojsonl` files re-read per request) is unchanged and is a separate, future optimization.

34. **The password vault is SERVER-side; the SPA only calls `/api/vault/*` with a `surface` id** — the encrypted store lives in Go (`internal/vault`, file `<GlobalDataDir>/browse/vault.json`, mode `0600`); the browser never sees the data key, the master password, or a raw item blob. `VaultForm.tsx` (Settings → Passwords) is the only consumer in Phase 1 and uses the fixed surface id **`"settings"`**; a browse panel will use its own `stateKey` as the surface. Rules when touching this: (a) **secrets must never reach logs** — vault request bodies (master password, passwords) must not be added to the debuglog, `/api/logs`, or access logs; the server logs only the *operation* (`vault: unlock: …`). (b) The `surface` is **client-supplied UX state, not a security boundary**; the auth token is. It is what lets `handleBrowseRevoke` revoke exactly one panel's grant, and an empty surface is never granted. (c) **No silent coercion** — the client must send a `sort`/`limit`/`offset` it was given and the server 400s malformed input (`atoiDefault` reports "present but non-numeric" separately from absent). (d) The master password exists only in its two inputs and is cleared after init/unlock; revealed passwords are transient per-row state, never persisted client-side. (e) Mutations are a load-modify-write under the vault's OS file lock, merging on-disk items with local ones — a stale-snapshot save would erase a concurrent ocode process's item. Autofill (local iframe, then Chrome/CDP) is Phases 2–3, tracked in `TODO.md` under `(password-vault)`. Regression: `internal/vault/*_test.go`, `internal/server/handler_vault_test.go`, web `api/client.vault.test.ts`, `Settings/VaultForm.test.tsx`, `Settings/SettingsPanel.vault.test.tsx`.
