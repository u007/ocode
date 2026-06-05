# Web UI Serve — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port the ocode TUI to a browser-based React SPA served from `ocode serve`, with SSE streaming for real-time chat.

**Architecture:** React 18 + TypeScript + Tailwind SPA built with Vite, embedded in Go binary via `go:embed`. SSE for streaming chat, REST for state queries. Stub fallback page for `go install` without pre-built assets.

**Tech Stack:** Go 1.23, React 18, TypeScript, Tailwind CSS, Vite, go:embed

**Spec:** `docs/superpowers/specs/2026-05-28-web-ui-serve-design.md`

---

## File Structure

### Go (server-side)

| File | Action | Responsibility |
|---|---|---|
| `internal/server/web.go` | Create | `go:embed` directive, SPA file server, fallback detection |
| `internal/server/handler_sse.go` | Create | SSE streaming chat handler |
| `internal/server/handler_git.go` | Create | Git status API endpoint |
| `internal/server/handler_files.go` | Create | File tree + content API endpoints |
| `internal/server/server.go` | Modify | Register new routes, add static file serving |
| `internal/server/server_test.go` | Modify | Add tests for new endpoints |
| `Makefile` | Modify | Add `web-build`, `web-dev` targets |

### React (frontend)

| File | Action | Responsibility |
|---|---|---|
| `web/package.json` | Create | Dependencies (react, tailwind, vite, typescript) |
| `web/tsconfig.json` | Create | TypeScript config |
| `web/vite.config.ts` | Create | Vite config with API proxy for dev mode |
| `web/tailwind.config.ts` | Create | Tailwind config |
| `web/index.html` | Create | SPA entry point |
| `web/src/main.tsx` | Create | React root mount |
| `web/src/App.tsx` | Create | Router + layout |
| `web/src/api/types.ts` | Create | Shared TypeScript types matching Go structs |
| `web/src/api/client.ts` | Create | REST + SSE API client |
| `web/src/stores/chatStore.ts` | Create | Chat state management (React context) |
| `web/src/hooks/useSSE.ts` | Create | SSE connection hook |
| `web/src/hooks/useChat.ts` | Create | Chat logic hook |
| `web/src/hooks/useSessions.ts` | Create | Session CRUD hook |
| `web/src/components/Chat/ChatPanel.tsx` | Create | Message list + auto-scroll |
| `web/src/components/Chat/ChatInput.tsx` | Create | Text input + send |
| `web/src/components/Chat/MessageBubble.tsx` | Create | Single message rendering |
| `web/src/components/Chat/ToolOutput.tsx` | Create | Tool execution display |
| `web/src/components/Sidebar/SessionList.tsx` | Create | Session list + switch |
| `web/src/components/Sidebar/ModelSelector.tsx` | Create | Model dropdown |
| `web/src/components/Sidebar/AgentTabs.tsx` | Create | Agent tab switcher |
| `web/src/components/Git/GitPanel.tsx` | Create | Git status display |
| `web/src/components/Files/FileTree.tsx` | Create | File browser tree |
| `web/src/components/common/PermissionDialog.tsx` | Create | Tool permission approve/deny |
| `web/src/components/common/CommandPalette.tsx` | Create | Slash command palette |
| `web/src/components/common/StatusBar.tsx` | Create | Status bar (model, tokens) |
| `web/dist/index.html` | Create | Stub fallback page (committed to git) |
| `web/dist/.gitkeep` | Create | Keep dist dir in git |

---

## Phase 1: Foundation + Chat

### Task 1: Scaffold React app

**Files:**
- Create: `web/package.json`
- Create: `web/tsconfig.json`
- Create: `web/vite.config.ts`
- Create: `web/tailwind.config.ts`
- Create: `web/index.html`
- Create: `web/src/main.tsx`
- Create: `web/src/App.tsx`
- Create: `web/.gitignore`

- [ ] **Step 1: Create package.json**

```json
{
  "name": "ocode-web",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc && vite build",
    "preview": "vite preview"
  },
  "dependencies": {
    "react": "^18.3.1",
    "react-dom": "^18.3.1"
  },
  "devDependencies": {
    "@types/react": "^18.3.12",
    "@types/react-dom": "^18.3.1",
    "@vitejs/plugin-react": "^4.3.4",
    "autoprefixer": "^10.4.20",
    "postcss": "^8.4.49",
    "tailwindcss": "^3.4.17",
    "typescript": "^5.7.2",
    "vite": "^6.0.5"
  }
}
```

- [ ] **Step 2: Create tsconfig.json**

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "forceConsistentCasingInFileNames": true
  },
  "include": ["src"]
}
```

- [ ] **Step 3: Create vite.config.ts**

```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://localhost:4096",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: false,
  },
});
```

- [ ] **Step 4: Create tailwind.config.ts**

```ts
import type { Config } from "tailwindcss";

export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {},
  },
  plugins: [],
} satisfies Config;
```

- [ ] **Step 5: Create postcss.config.js**

```js
export default {
  plugins: {
    tailwindcss: {},
    autoprefixer: {},
  },
};
```

- [ ] **Step 6: Create index.html**

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>ocode</title>
  </head>
  <body class="bg-zinc-900 text-zinc-100">
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 7: Create src/main.tsx**

```tsx
import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import "./index.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
```

- [ ] **Step 8: Create src/index.css**

```css
@tailwind base;
@tailwind components;
@tailwind utilities;
```

- [ ] **Step 9: Create src/App.tsx**

```tsx
export default function App() {
  return (
    <div className="flex h-screen items-center justify-center">
      <h1 className="text-2xl font-bold">ocode web</h1>
    </div>
  );
}
```

- [ ] **Step 10: Create web/.gitignore**

```
node_modules/
dist/*
!dist/index.html
!dist/.gitkeep
```

- [ ] **Step 11: Verify build**

Run: `cd web && npm install && npm run build`
Expected: Build succeeds, `web/dist/` contains `index.html`, `assets/*.js`, `assets/*.css`

- [ ] **Step 12: Commit**

```bash
git add web/
git commit -m "feat(web): scaffold React app with Vite + TS + Tailwind"
```

---

### Task 2: Create Go embed + static serving

**Files:**
- Create: `internal/server/web.go`
- Create: `web/dist/index.html` (stub)
- Create: `web/dist/.gitkeep`
- Modify: `internal/server/server.go` (add catch-all route)
- Modify: `.gitignore` (add web/dist assets exclusion)

- [ ] **Step 1: Create stub fallback page**

Create `web/dist/index.html`:
```html
<!doctype html>
<html lang="en">
<head><meta charset="UTF-8"><title>ocode</title></head>
<body style="background:#18181b;color:#e4e4e7;font-family:system-ui;display:flex;align-items:center;justify-content:center;height:100vh;margin:0">
  <div style="text-align:center">
    <h1 style="font-size:1.5rem;margin-bottom:0.5rem">ocode web UI not built</h1>
    <p style="color:#a1a1aa">Run <code style="background:#27272a;padding:2px 6px;border-radius:4px">make web-build</code> then rebuild ocode.</p>
  </div>
</body>
</html>
```

- [ ] **Step 2: Create web/dist/.gitkeep**

```bash
touch web/dist/.gitkeep
```

- [ ] **Step 3: Update .gitignore**

Add to `.gitignore`:
```
web/node_modules/
web/dist/assets/
```

- [ ] **Step 4: Create internal/server/web.go**

```go
package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:../../web/dist
var webAssets embed.FS

func webFS() fs.FS {
	f, err := fs.Sub(webAssets, "web/dist")
	if err != nil {
		// Fallback to empty FS - should never happen with go:embed
		return nil
	}
	return f
}

// spaHandler serves the embedded React SPA.
// API routes (/api/*) are handled separately; everything else falls through to index.html.
func spaHandler() http.Handler {
	web := webFS()
	if web == nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(web))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Check if file exists in the embedded FS
		if f, err := web.(fs.ReadFileFS).Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for client-side routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 5: Modify server.go to register SPA route**

In `internal/server/server.go`, add to `registerRoutes()` after existing routes:

```go
// Serve embedded web UI for non-API routes
s.mux.Handle("/", spaHandler())
```

- [ ] **Step 6: Verify stub works**

Run: `go build -o /tmp/ocode-test && /tmp/ocode-test serve -port 4097 &`
Run: `curl -s http://localhost:4097/`
Expected: Contains "ocode web UI not built"
Kill: `kill %1`

- [ ] **Step 7: Commit**

```bash
git add internal/server/web.go internal/server/server.go web/dist/ .gitignore
git commit -m "feat(server): embed web assets with go:embed and SPA fallback"
```

---

### Task 3: Add SSE streaming endpoint

**Files:**
- Create: `internal/server/handler_sse.go`
- Modify: `internal/server/server.go` (register route)
- Modify: `internal/server/server_test.go` (add test)

- [ ] **Step 1: Create handler_sse.go**

```go
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

type SSEEvent struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

type TextDelta struct {
	Delta string `json:"delta"`
}

type ToolStartEvent struct {
	Tool    string `json:"tool"`
	Command string `json:"command,omitempty"`
	Content string `json:"content,omitempty"`
}

type ToolResultEvent struct {
	Tool   string `json:"tool"`
	Output string `json:"output"`
}

type ToolErrorEvent struct {
	Tool  string `json:"tool"`
	Error string `json:"error"`
}

type DoneEvent struct {
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
}

func (h *Handler) HandleChatStream(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	message := r.URL.Query().Get("message")

	if message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	model := h.cfg.Model
	if model == "" {
		writeError(w, http.StatusBadRequest, "no model configured")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	h.mu.Lock()

	var as *agentSession
	if sessionID != "" {
		as = h.agents[sessionID]
	}

	if as == nil {
		if sessionID == "" {
			sessionID = session.NewSessionID()
		}

		var messages []agent.Message
		if sessionID != "" {
			s, err := session.Load(sessionID)
			if err == nil {
				messages = s.Messages
			}
		}

		client := agent.NewClient(h.cfg, model)
		if client == nil {
			h.mu.Unlock()
			writeError(w, http.StatusInternalServerError, "failed to create LLM client")
			return
		}

		tools := tool.LoadBuiltins(h.cfg)
		ag := agent.NewAgent(client, tools, h.cfg)
		ag.LoadExternalTools(h.cfg)

		as = &agentSession{
			agent:    ag,
			messages: messages,
			model:    model,
		}
		h.agents[sessionID] = as
	}

	as.messages = append(as.messages, agent.Message{Role: "user", Content: message})
	messages := as.messages
	ag := as.agent
	sessModel := as.model
	h.mu.Unlock()

	// Send session ID as first event
	sendSSE(w, flusher, "session", map[string]string{"session_id": sessionID})

	resp, err := ag.Step(messages)
	if err != nil {
		sendSSE(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}

	h.mu.Lock()
	as.messages = append(as.messages, resp...)

	var content strings.Builder
	for _, m := range resp {
		if m.Role == "assistant" && m.Content != "" {
			sendSSE(w, flusher, "text", TextDelta{Delta: m.Content})
			content.WriteString(m.Content)
		}
	}

	_ = session.Save(sessionID, "", as.messages, nil)
	h.mu.Unlock()

	sendSSE(w, flusher, "done", DoneEvent{
		SessionID: sessionID,
		Model:     sessModel,
	})
}

func sendSSE(w http.ResponseWriter, flusher http.Flusher, event string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
	flusher.Flush()
}
```

- [ ] **Step 2: Register route in server.go**

Add to `registerRoutes()`:
```go
s.mux.HandleFunc("GET /api/chat/stream", s.authMiddleware(s.handleChatStream))
```

Add handler method:
```go
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleChatStream(w, r)
}
```

- [ ] **Step 3: Add test for SSE endpoint**

Add to `internal/server/server_test.go`:
```go
func TestChatStreamNoMessage(t *testing) {
	h := NewHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/chat/stream", nil)
	h.HandleChatStream(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
```

- [ ] **Step 4: Verify tests pass**

Run: `go test ./internal/server/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/handler_sse.go internal/server/server.go internal/server/server_test.go
git commit -m "feat(server): add SSE streaming chat endpoint"
```

---

### Task 4: Build API client + types

**Files:**
- Create: `web/src/api/types.ts`
- Create: `web/src/api/client.ts`

- [ ] **Step 1: Create types.ts**

```ts
export interface Message {
  role: "user" | "assistant" | "system";
  content: string;
}

export interface ChatRequest {
  content: string;
  sessionId?: string;
  model?: string;
}

export interface ChatResponse {
  content: string;
  sessionId: string;
  model: string;
}

export interface SessionInfo {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
}

export interface ModelInfo {
  name: string;
  provider: string;
}

export interface AgentInfo {
  name: string;
  description: string;
  mode: string;
}

export interface SSETextEvent {
  delta: string;
}

export interface SSEToolStartEvent {
  tool: string;
  command?: string;
  content?: string;
}

export interface SSEToolResultEvent {
  tool: string;
  output: string;
}

export interface SSEToolErrorEvent {
  tool: string;
  error: string;
}

export interface SSEPermissionEvent {
  tool: string;
  command?: string;
  request_id: string;
}

export interface SSEdoneEvent {
  session_id: string;
  model: string;
}

export interface SSESessionEvent {
  session_id: string;
}
```

- [ ] **Step 2: Create client.ts**

```ts
import type {
  ChatResponse,
  SessionInfo,
  ModelInfo,
  AgentInfo,
} from "./types";

const BASE = "";

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || res.statusText);
  }
  return res.json();
}

export const api = {
  listSessions: () => fetchJSON<SessionInfo[]>("/api/sessions"),

  getSession: (id: string) => fetchJSON<SessionInfo>(`/api/sessions/${id}`),

  listModels: () => fetchJSON<ModelInfo[]>("/api/models"),

  listAgents: () => fetchJSON<AgentInfo[]>("/api/agents"),

  sendMessage: (sessionId: string, content: string) =>
    fetchJSON<ChatResponse>(`/api/sessions/${sessionId}/message`, {
      method: "POST",
      body: JSON.stringify({ content }),
    }),

  chat: (content: string, sessionId?: string, model?: string) =>
    fetchJSON<ChatResponse>("/api/chat", {
      method: "POST",
      body: JSON.stringify({ content, sessionId, model }),
    }),
};

export type SSEEventHandler = (event: string, data: unknown) => void;

export function connectChatSSE(
  message: string,
  session: string | undefined,
  onEvent: SSEEventHandler,
  onError?: (err: Error) => void,
): () => void {
  const params = new URLSearchParams({ message });
  if (session) params.set("session", session);

  const es = new EventSource(`/api/chat/stream?${params}`);

  es.addEventListener("text", (e) => onEvent("text", JSON.parse(e.data)));
  es.addEventListener("tool_start", (e) =>
    onEvent("tool_start", JSON.parse(e.data)),
  );
  es.addEventListener("tool_result", (e) =>
    onEvent("tool_result", JSON.parse(e.data)),
  );
  es.addEventListener("tool_error", (e) =>
    onEvent("tool_error", JSON.parse(e.data)),
  );
  es.addEventListener("permission_required", (e) =>
    onEvent("permission_required", JSON.parse(e.data)),
  );
  es.addEventListener("session", (e) =>
    onEvent("session", JSON.parse(e.data)),
  );
  es.addEventListener("done", (e) => {
    onEvent("done", JSON.parse(e.data));
    es.close();
  });
  es.addEventListener("error", (e) => {
    onEvent("error", { error: "connection lost" });
    onError?.(new Error("SSE connection error"));
    es.close();
  });

  return () => es.close();
}
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add web/src/api/
git commit -m "feat(web): add API client with REST and SSE support"
```

---

### Task 5: Build ChatPanel + MessageBubble

**Files:**
- Create: `web/src/stores/chatStore.ts`
- Create: `web/src/components/Chat/ChatPanel.tsx`
- Create: `web/src/components/Chat/MessageBubble.tsx`

- [ ] **Step 1: Create chatStore.ts**

```tsx
import { createContext, useContext, useReducer, type ReactNode } from "react";
import type { Message } from "../api/types";

interface ChatState {
  messages: Message[];
  sessionId: string | null;
  model: string | null;
  isStreaming: boolean;
  error: string | null;
}

type ChatAction =
  | { type: "ADD_MESSAGE"; message: Message }
  | { type: "SET_MESSAGES"; messages: Message[] }
  | { type: "SET_SESSION"; sessionId: string }
  | { type: "SET_MODEL"; model: string }
  | { type: "SET_STREAMING"; isStreaming: boolean }
  | { type: "SET_ERROR"; error: string | null }
  | { type: "APPEND_DELTA"; delta: string }
  | { type: "RESET" };

const initialState: ChatState = {
  messages: [],
  sessionId: null,
  model: null,
  isStreaming: false,
  error: null,
};

function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case "ADD_MESSAGE":
      return { ...state, messages: [...state.messages, action.message] };
    case "SET_MESSAGES":
      return { ...state, messages: action.messages };
    case "SET_SESSION":
      return { ...state, sessionId: action.sessionId };
    case "SET_MODEL":
      return { ...state, model: action.model };
    case "SET_STREAMING":
      return { ...state, isStreaming: action.isStreaming };
    case "SET_ERROR":
      return { ...state, error: action.error };
    case "APPEND_DELTA": {
      const msgs = [...state.messages];
      const last = msgs[msgs.length - 1];
      if (last && last.role === "assistant") {
        msgs[msgs.length - 1] = {
          ...last,
          content: last.content + action.delta,
        };
      } else {
        msgs.push({ role: "assistant", content: action.delta });
      }
      return { ...state, messages: msgs };
    }
    case "RESET":
      return initialState;
    default:
      return state;
  }
}

const ChatStateContext = createContext<ChatState>(initialState);
const ChatDispatchContext = createContext<React.Dispatch<ChatAction>>(() => {});

export function ChatProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(chatReducer, initialState);
  return (
    <ChatStateContext.Provider value={state}>
      <ChatDispatchContext.Provider value={dispatch}>
        {children}
      </ChatDispatchContext.Provider>
    </ChatStateContext.Provider>
  );
}

export function useChatState() {
  return useContext(ChatStateContext);
}

export function useChatDispatch() {
  return useContext(ChatDispatchContext);
}
```

- [ ] **Step 2: Create MessageBubble.tsx**

```tsx
import type { Message } from "../../api/types";

interface Props {
  message: Message;
}

export default function MessageBubble({ message }: Props) {
  const isUser = message.role === "user";

  return (
    <div className={`flex ${isUser ? "justify-end" : "justify-start"} mb-3`}>
      <div
        className={`max-w-[80%] rounded-lg px-4 py-2 ${
          isUser
            ? "bg-blue-600 text-white"
            : "bg-zinc-800 text-zinc-100"
        }`}
      >
        <pre className="whitespace-pre-wrap font-sans text-sm">
          {message.content}
        </pre>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Create ChatPanel.tsx**

```tsx
import { useEffect, useRef } from "react";
import { useChatState } from "../../stores/chatStore";
import MessageBubble from "./MessageBubble";

export default function ChatPanel() {
  const { messages } = useChatState();
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  return (
    <div className="flex-1 overflow-y-auto p-4">
      {messages.length === 0 && (
        <div className="flex h-full items-center justify-center text-zinc-500">
          Start a conversation
        </div>
      )}
      {messages.map((msg, i) => (
        <MessageBubble key={i} message={msg} />
      ))}
      <div ref={bottomRef} />
    </div>
  );
}
```

- [ ] **Step 4: Wire into App.tsx**

Update `web/src/App.tsx`:
```tsx
import { ChatProvider } from "./stores/chatStore";
import ChatPanel from "./components/Chat/ChatPanel";

export default function App() {
  return (
    <ChatProvider>
      <div className="flex h-screen flex-col">
        <ChatPanel />
      </div>
    </ChatProvider>
  );
}
```

- [ ] **Step 5: Verify build**

Run: `cd web && npm run build`
Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add web/src/stores/ web/src/components/Chat/ web/src/App.tsx
git commit -m "feat(web): add ChatPanel, MessageBubble, and chat store"
```

---

### Task 6: Build ChatInput with SSE integration

**Files:**
- Create: `web/src/hooks/useSSE.ts`
- Create: `web/src/hooks/useChat.ts`
- Create: `web/src/components/Chat/ChatInput.tsx`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: Create useSSE.ts**

```ts
import { useCallback, useRef } from "react";
import { connectChatSSE, type SSEEventHandler } from "../api/client";

export function useSSE() {
  const cleanupRef = useRef<(() => void) | null>(null);

  const send = useCallback(
    (message: string, session: string | undefined, onEvent: SSEEventHandler) => {
      // Close any existing connection
      cleanupRef.current?.();
      cleanupRef.current = connectChatSSE(message, session, onEvent);
    },
    [],
  );

  const close = useCallback(() => {
    cleanupRef.current?.();
    cleanupRef.current = null;
  }, []);

  return { send, close };
}
```

- [ ] **Step 2: Create useChat.ts**

```ts
import { useCallback } from "react";
import { useChatState, useChatDispatch } from "../stores/chatStore";
import { useSSE } from "./useSSE";
import type {
  SSETextEvent,
  SSESessionEvent,
  SSEdoneEvent,
} from "../api/types";

export function useChat() {
  const state = useChatState();
  const dispatch = useChatDispatch();
  const { send, close } = useSSE();

  const sendMessage = useCallback(
    (content: string) => {
      dispatch({ type: "ADD_MESSAGE", message: { role: "user", content } });
      dispatch({ type: "SET_STREAMING", isStreaming: true });
      dispatch({ type: "SET_ERROR", error: null });

      send(content, state.sessionId ?? undefined, (event, data) => {
        switch (event) {
          case "session":
            dispatch({
              type: "SET_SESSION",
              sessionId: (data as SSESessionEvent).session_id,
            });
            break;
          case "text":
            dispatch({
              type: "APPEND_DELTA",
              delta: (data as SSETextEvent).delta,
            });
            break;
          case "done":
            dispatch({
              type: "SET_MODEL",
              model: (data as SSEdoneEvent).model,
            });
            dispatch({ type: "SET_STREAMING", isStreaming: false });
            break;
          case "error":
            dispatch({
              type: "SET_ERROR",
              error: (data as { error: string }).error,
            });
            dispatch({ type: "SET_STREAMING", isStreaming: false });
            break;
        }
      });
    },
    [state.sessionId, send, dispatch],
  );

  const stop = useCallback(() => {
    close();
    dispatch({ type: "SET_STREAMING", isStreaming: false });
  }, [close, dispatch]);

  return { sendMessage, stop, isStreaming: state.isStreaming };
}
```

- [ ] **Step 3: Create ChatInput.tsx**

```tsx
import { useState, type KeyboardEvent } from "react";
import { useChat } from "../../hooks/useChat";

export default function ChatInput() {
  const [input, setInput] = useState("");
  const { sendMessage, stop, isStreaming } = useChat();

  const handleSend = () => {
    const trimmed = input.trim();
    if (!trimmed || isStreaming) return;
    setInput("");
    sendMessage(trimmed);
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  return (
    <div className="border-t border-zinc-700 p-4">
      <div className="flex gap-2">
        <textarea
          className="flex-1 resize-none rounded-lg border border-zinc-600 bg-zinc-800 p-3 text-sm text-zinc-100 placeholder-zinc-500 focus:border-blue-500 focus:outline-none"
          rows={2}
          placeholder="Type a message... (Enter to send, Shift+Enter for newline)"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
        />
        {isStreaming ? (
          <button
            onClick={stop}
            className="self-end rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700"
          >
            Stop
          </button>
        ) : (
          <button
            onClick={handleSend}
            disabled={!input.trim()}
            className="self-end rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
          >
            Send
          </button>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Update App.tsx**

```tsx
import { ChatProvider } from "./stores/chatStore";
import ChatPanel from "./components/Chat/ChatPanel";
import ChatInput from "./components/Chat/ChatInput";

export default function App() {
  return (
    <ChatProvider>
      <div className="flex h-screen flex-col">
        <ChatPanel />
        <ChatInput />
      </div>
    </ChatProvider>
  );
}
```

- [ ] **Step 5: Verify build**

Run: `cd web && npm run build`
Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add web/src/hooks/ web/src/components/Chat/ChatInput.tsx web/src/App.tsx
git commit -m "feat(web): add ChatInput with SSE streaming integration"
```

---

### Task 7: End-to-end integration test

- [ ] **Step 1: Build web assets**

Run: `cd web && npm install && npm run build`
Expected: Build succeeds

- [ ] **Step 2: Build Go binary**

Run: `go build -o ./ocode .`
Expected: Binary includes embedded web assets

- [ ] **Step 3: Start server and test in browser**

Run: `./ocode serve -port 4096`
Open: `http://localhost:4096`
Expected: Web UI loads, can type messages, see streaming responses

- [ ] **Step 4: Verify SSE works**

Run: `curl -N "http://localhost:4096/api/chat/stream?message=hello"`
Expected: SSE events stream back

- [ ] **Step 5: Commit any fixes**

```bash
git add -A
git commit -m "fix(web): integration fixes from e2e testing"
```

---

## Phase 2: Tools + Sessions

### Task 8: Enhance SSE with tool events

**Goal:** Extend `handler_sse.go` to emit `tool_start`, `tool_result`, `tool_error` events during agent execution.

- [ ] **Step 1: Add tool event callback to agent.Step**

Modify `internal/agent/agent.go` to accept an optional callback for tool events, or wrap tool execution to capture events. The exact approach depends on the agent's current architecture — inspect `agent.Step()` to determine how tool calls are dispatched.

- [ ] **Step 2: Update handler_sse.go to emit tool events**

Replace the single `ag.Step(messages)` call with a streaming version that captures tool execution events and sends them as SSE.

- [ ] **Step 3: Update web/src/api/client.ts**

Add event listeners for `tool_start`, `tool_result`, `tool_error` in `connectChatSSE`.

- [ ] **Step 4: Create ToolOutput component**

`web/src/components/Chat/ToolOutput.tsx` — renders tool name, command, and output with syntax highlighting.

- [ ] **Step 5: Update ChatPanel to render tool events**

Modify `ChatPanel.tsx` to track tool events from the chat store and render `<ToolOutput>` blocks inline.

- [ ] **Step 6: Test tool execution visibility**

Send a message that triggers a bash tool. Verify tool events appear in the browser.

- [ ] **Step 7: Commit**

```bash
git commit -m "feat(web): show tool execution events in chat UI"
```

---

### Task 9: Session management UI

- [ ] **Step 1: Create useSessions hook**

`web/src/hooks/useSessions.ts` — wraps `api.listSessions()`, `api.getSession()`, session switching logic.

- [ ] **Step 2: Create SessionList component**

`web/src/components/Sidebar/SessionList.tsx` — displays sessions, click to switch, "New Session" button.

- [ ] **Step 3: Add session switching to chatStore**

Add `SET_SESSION` action to load messages from the selected session.

- [ ] **Step 4: Wire SessionList into App layout**

Add sidebar layout to `App.tsx` with `SessionList` on the left, chat on the right.

- [ ] **Step 5: Test session create/switch**

Create multiple sessions, switch between them. Verify messages persist.

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(web): add session list and switching"
```

---

### Task 10: Model selector + Agent tabs

- [ ] **Step 1: Create ModelSelector component**

`web/src/components/Sidebar/ModelSelector.tsx` — dropdown from `/api/models`.

- [ ] **Step 2: Create AgentTabs component**

`web/src/components/Sidebar/AgentTabs.tsx` — tab bar from `/api/agents`.

- [ ] **Step 3: Add /api/agents endpoint**

Create `handler_agents.go` that returns configured agents from the registry.

- [ ] **Step 4: Wire into sidebar layout**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(web): add model selector and agent tabs"
```

---

## Phase 3: Sidebar + Status

### Task 11: StatusBar component

- [ ] **Step 1: Add /api/status endpoint**

Returns current model, token usage, uptime.

- [ ] **Step 2: Create StatusBar component**

`web/src/components/common/StatusBar.tsx` — bottom bar showing model, tokens, connection status.

- [ ] **Step 3: Commit**

---

### Task 12: Command palette

- [ ] **Step 1: Define slash commands in client**

List of built-in commands (`/clear`, `/model`, `/session`, etc.) plus custom commands.

- [ ] **Step 2: Create CommandPalette component**

`web/src/components/common/CommandPalette.tsx` — modal with search, triggered by `/` key.

- [ ] **Step 3: Wire command execution**

- [ ] **Step 4: Commit**

---

### Task 13: Permission dialog

- [ ] **Step 1: Add permission SSE event**

When agent requests permission, emit `permission_required` SSE event.

- [ ] **Step 2: Add /api/permissions/approve endpoint**

POST with `request_id` + `approved` boolean.

- [ ] **Step 3: Create PermissionDialog component**

`web/src/components/common/PermissionDialog.tsx` — modal showing tool details + approve/deny.

- [ ] **Step 4: Commit**

---

### Task 14: Keyboard shortcuts

- [ ] **Step 1: Add global keyboard handler**

Enter to send, Ctrl+N new session, Ctrl+K command palette, Escape close dialogs.

- [ ] **Step 2: Commit**

---

## Phase 4: Git + Files

### Task 15: Git status endpoint + panel

- [ ] **Step 1: Create handler_git.go**

`/api/git/status` — returns branch name, staged files, unstaged files, diff summary.

- [ ] **Step 2: Create GitPanel component**

`web/src/components/Git/GitPanel.tsx` — branch display, file lists, diff preview.

- [ ] **Step 3: Commit**

---

### Task 16: File browser

- [ ] **Step 1: Create handler_files.go**

`/api/files/tree?path=` — returns directory tree. `/api/files/content?path=` — returns file content.

- [ ] **Step 2: Create FileTree component**

`web/src/components/Files/FileTree.tsx` — collapsible tree, click to view content.

- [ ] **Step 3: Commit**

---

## Phase 5: Polish

### Task 17: Theme support

- [ ] **Step 1: Add dark/light theme toggle**

- [ ] **Step 2: Match TUI theme colors**

- [ ] **Step 3: Commit**

---

### Task 18: Responsive layout + error handling

- [ ] **Step 1: Add mobile-responsive layout**

- [ ] **Step 2: Add loading states and error boundaries**

- [ ] **Step 3: Commit**

---

### Task 19: Makefile + CI integration

- [ ] **Step 1: Add Makefile targets**

```makefile
web-build:
	cd web && npm install && npm run build

web-dev:
	cd web && npm run dev

build: web-build
	go build -o ocode
```

- [ ] **Step 2: Update CI pipeline**

Add Node.js step for web build before Go build.

- [ ] **Step 3: Final commit**

```bash
git commit -m "feat: add web build to Makefile and CI"
```
