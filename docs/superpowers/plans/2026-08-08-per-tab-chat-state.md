# Per-Tab Chat State Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make session tabs in the web/desktop chat UI real tabs — switching tabs (session, project, or top-nav) never loses scroll position, transcript, or in-flight streaming, and background tabs keep updating live.

**Architecture:** Move messages/live-stream/permission state out of the single global `chatStore` slice into a `sessions: Record<sessionId, SessionSlice>` map; mount one `ChatPanel` per open tab (CSS-hidden when inactive, like the existing `FileEditor` tab pattern) instead of one shared instance; route every SSE event to its own session's slice regardless of which tab is active.

**Tech Stack:** React 18, TypeScript, Vitest + Testing Library, existing `chatStore`/`projectStore` reducer-context pattern.

## Global Constraints

- No backend/API changes — spec scope is `web/src/` only.
- No new dependencies.
- Preserve existing visual style/classnames except where structurally required (documented per task).
- Every per-session store action takes an explicit `sessionId`; there is no more implicit "current session" in `chatStore`.
- `ChatInput` and `AgentPreview` remain single (not one-per-tab) instances, driven by `projectStore`'s `activeTabId`.

---

## Reference: spec

Full design at `docs/superpowers/specs/2026-08-08-per-tab-chat-state-design.md`. Read it before starting if anything below is ambiguous.

---

### Task 1: `projectStore` — scope tab-rename/title actions to their owning project

**Why first:** `UPDATE_TAB_TITLE` and `UPDATE_TAB_ID` currently only touch `state.tabsByProject[state.activeProject.path]` — they silently no-op for a tab that belongs to a *different* project than the one currently active. Once background tabs (Task 5, Task 6) can rename themselves or get their title updated while some other project is active, this bug becomes reachable and must be fixed first.

**Files:**
- Modify: `web/src/stores/projectStore.tsx:65-161` (the `projectReducer` function)
- Test: `web/src/stores/projectStore.test.tsx` (new)

**Interfaces:**
- Produces: `projectReducer` now resolves the owning project internally for `UPDATE_TAB_TITLE`/`UPDATE_TAB_ID` — callers don't change, both actions still take `{id, title}` / `{oldId, newId, newTitle?}` with no project path.

- [ ] **Step 1: Write the failing tests**

Create `web/src/stores/projectStore.test.tsx`:

```tsx
import { describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { ProjectProvider, useProjectState } from "./projectStore";

// projectReducer is not exported — drive it through the provider + dispatch,
// same as a real consumer would.
function setup() {
  return renderHook(() => useProjectState(), {
    wrapper: ({ children }) => <ProjectProvider>{children}</ProjectProvider>,
  });
}

describe("projectStore tab actions across projects", () => {
  it("UPDATE_TAB_TITLE updates a tab that belongs to a non-active project", () => {
    const { result } = setup();
    act(() => {
      result.current.dispatch({
        type: "SET_ACTIVE_PROJECT",
        project: { path: "/proj-a", name: "a" },
      });
      result.current.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-b", projectPath: "/proj-b", title: "old" },
      });
      // Switch active project away from proj-b before renaming its tab.
      result.current.dispatch({
        type: "SET_ACTIVE_PROJECT",
        project: { path: "/proj-a", name: "a" },
      });
      result.current.dispatch({
        type: "UPDATE_TAB_TITLE",
        id: "sess-b",
        title: "renamed",
      });
    });
    expect(result.current.state.tabsByProject["/proj-b"][0].title).toBe(
      "renamed",
    );
  });

  it("UPDATE_TAB_ID rekeys a tab that belongs to a non-active project", () => {
    const { result } = setup();
    act(() => {
      result.current.dispatch({
        type: "SET_ACTIVE_PROJECT",
        project: { path: "/proj-a", name: "a" },
      });
      result.current.dispatch({
        type: "ADD_TAB",
        tab: { id: "new-1", projectPath: "/proj-b", title: "New session" },
      });
      result.current.dispatch({
        type: "UPDATE_TAB_ID",
        oldId: "new-1",
        newId: "sess-real",
        newTitle: "Real title",
      });
    });
    const tabs = result.current.state.tabsByProject["/proj-b"];
    expect(tabs.find((t) => t.id === "sess-real")?.title).toBe("Real title");
    expect(tabs.find((t) => t.id === "new-1")).toBeUndefined();
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npx vitest run src/stores/projectStore.test.tsx`
Expected: FAIL — both assertions fail because the reducer currently looks up `state.activeProject.path` (`/proj-a`), not the tab's real project (`/proj-b`), so the title/rekey lands nowhere.

- [ ] **Step 3: Add an owning-project lookup helper and use it in both cases**

In `web/src/stores/projectStore.tsx`, add above `projectReducer` (near `activeTabId`/`activeTabs`):

```tsx
/** Finds which project's tab list contains this tab id, regardless of which
 *  project is currently active. Needed because tab rename/rekey can be
 *  triggered by a background session (SessionTabSync, ChatPanel) whose
 *  project isn't the one the user is currently looking at. */
function findProjectPathForTab(state: ProjectState, tabId: string): string | null {
  for (const [path, list] of Object.entries(state.tabsByProject)) {
    if (list.some((t) => t.id === tabId)) return path;
  }
  return null;
}
```

Replace the `UPDATE_TAB_TITLE` and `UPDATE_TAB_ID` cases:

```tsx
    case "UPDATE_TAB_TITLE": {
      const ownerPath = findProjectPathForTab(state, action.id);
      if (!ownerPath) return state;
      const list = state.tabsByProject[ownerPath].map((t) =>
        t.id === action.id ? { ...t, title: action.title } : t
      );
      return { ...state, tabsByProject: { ...state.tabsByProject, [ownerPath]: list } };
    }
    case "UPDATE_TAB_ID": {
      const ownerPath = findProjectPathForTab(state, action.oldId);
      if (!ownerPath) return state;
      const list = state.tabsByProject[ownerPath];
      const tab = list.find((t) => t.id === action.oldId);
      if (!tab) return state;
      const newTab = { ...tab, id: action.newId, title: action.newTitle || tab.title };
      return {
        ...state,
        tabsByProject: {
          ...state.tabsByProject,
          [ownerPath]: list.map((t) => (t.id === action.oldId ? newTab : t)),
        },
        activeTabByProject: {
          ...state.activeTabByProject,
          [ownerPath]:
            state.activeTabByProject[ownerPath] === action.oldId
              ? action.newId
              : state.activeTabByProject[ownerPath],
        },
      };
    }
```

These replace the two existing cases wholesale (both previously started with `if (!path) return state;` using the outer `path = state.activeProject?.path || ""` — leave that outer `path` const as-is, it's still used by `ADD_TAB`/`REMOVE_TAB`/`SET_ACTIVE_TAB`/`UPDATE_TAB_TITLE`'s old body being replaced/`ENSURE_NEW_TAB`, none of which change).

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && npx vitest run src/stores/projectStore.test.tsx`
Expected: PASS

- [ ] **Step 5: Run the full web test suite to check for regressions**

Run: `cd web && npx vitest run`
Expected: PASS (no other suite touches these reducer cases yet)

- [ ] **Step 6: Commit**

```bash
cd web && git add src/stores/projectStore.tsx src/stores/projectStore.test.tsx
git commit -m "fix(web): scope tab rename/rekey to the tab's own project"
```

---

### Task 2: `chatStore` — per-session state model

**Files:**
- Modify: `web/src/stores/chatStore.tsx` (full rewrite of the state shape, actions, reducer — types/context plumbing at top/bottom stay the same pattern)
- Test: `web/src/stores/chatStore.test.tsx` (new)

**Interfaces:**
- Produces:
  - `export interface SessionSlice` — `messages`, `live`, `isStreaming`, `error`, `pendingPermission`, `pendingQuestion`, `totalMessages`, `hasMore`, `loadingMore`, `initialized`.
  - `export const emptySessionSlice: SessionSlice`
  - `export function getSessionSlice(state: ChatState, sessionId: string | null | undefined): SessionSlice`
  - `export function chatReducer(state: ChatState, action: ChatAction): ChatState` (now exported for tests)
  - `ChatState.sessions: Record<string, SessionSlice>`
  - All previously-global-but-really-per-session actions now require `sessionId: string`: `ADD_MESSAGE`, `SET_MESSAGES`, `SET_STREAMING`, `SET_ERROR`, `APPEND_DELTA`, `LIVE_DELTA`, `LIVE_TOOL_START`, `LIVE_TOOL_RESULT`, `LIVE_RESET`, `PERMISSION_REQUEST`, `PERMISSION_RESOLVED`, `QUESTION_REQUEST`, `QUESTION_RESOLVED`, `PREPEND_MESSAGES`, `SET_LOADING_MORE`, `MERGE_SNAPSHOT`, `SET_TOTAL`, `RESET`.
  - New action: `REKEY_SESSION { oldId: string; newId: string }` — moves one slice from `oldId` to `newId`, no-op if `oldId` has no slice (idempotent, safe to dispatch from two independent call sites racing each other — see Task 5/6).
  - `SET_SESSION` action is **removed**.
  - Global (session-independent) fields/actions are unchanged: `model`, `smallModel*`, `advisorModel`, `advisorEnabled`, `ocr*`, `tuiStatus`, `sessionContext`, `spendingUSD`, `tuiStatusReady` and their setters.
  - `RESET`'s old "preserve advisor/small-model/tuiStatus across reset" behavior is dropped — those fields are global now and were never touched by a per-session `RESET` in the first place.

- [ ] **Step 1: Write the failing reducer tests**

Create `web/src/stores/chatStore.test.tsx`:

```tsx
import { describe, expect, it } from "vitest";
import { chatReducer, getSessionSlice, initialState } from "./chatStore";
import type { ChatState } from "./chatStore";

function initial(): ChatState {
  return initialState;
}

describe("chatStore per-session isolation", () => {
  it("ADD_MESSAGE only affects the targeted session", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "a",
      message: { role: "user", content: "hi a" },
    });
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "b",
      message: { role: "user", content: "hi b" },
    });
    expect(getSessionSlice(state, "a").messages).toEqual([
      { role: "user", content: "hi a" },
    ]);
    expect(getSessionSlice(state, "b").messages).toEqual([
      { role: "user", content: "hi b" },
    ]);
  });

  it("getSessionSlice returns the empty default for an unknown or null session", () => {
    const state = initial();
    expect(getSessionSlice(state, "unknown").messages).toEqual([]);
    expect(getSessionSlice(state, null).messages).toEqual([]);
  });

  it("RESET drops only the targeted session's slice", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "a",
      message: { role: "user", content: "hi a" },
    });
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "b",
      message: { role: "user", content: "hi b" },
    });
    state = chatReducer(state, { type: "RESET", sessionId: "a" });
    expect(getSessionSlice(state, "a").messages).toEqual([]);
    expect(getSessionSlice(state, "b").messages).toEqual([
      { role: "user", content: "hi b" },
    ]);
  });

  it("REKEY_SESSION moves a slice's content to the new id and drops the old key", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "new-123",
      message: { role: "user", content: "first turn" },
    });
    state = chatReducer(state, {
      type: "REKEY_SESSION",
      oldId: "new-123",
      newId: "sess-real",
    });
    expect(getSessionSlice(state, "sess-real").messages).toEqual([
      { role: "user", content: "first turn" },
    ]);
    expect(state.sessions["new-123"]).toBeUndefined();
  });

  it("REKEY_SESSION is a no-op when the old id has no slice (idempotent under a race)", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "REKEY_SESSION",
      oldId: "new-123",
      newId: "sess-real",
    });
    expect(state.sessions["new-123"]).toBeUndefined();
    expect(state.sessions["sess-real"]).toBeUndefined();
  });

  it("global fields (e.g. model) are unaffected by per-session actions", () => {
    let state = initial();
    state = chatReducer(state, { type: "SET_MODEL", model: "claude-sonnet-5" });
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "a",
      message: { role: "user", content: "hi" },
    });
    expect(state.model).toBe("claude-sonnet-5");
  });

  it("MERGE_SNAPSHOT marks the session initialized", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "MERGE_SNAPSHOT",
      sessionId: "a",
      messages: [{ role: "user", content: "hi" }],
      total: 1,
    });
    expect(getSessionSlice(state, "a").initialized).toBe(true);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npx vitest run src/stores/chatStore.test.tsx`
Expected: FAIL — `chatReducer`/`getSessionSlice`/`ChatState` aren't exported yet and the action shapes don't match.

- [ ] **Step 3: Rewrite `chatStore.tsx`**

Replace the whole file with:

```tsx
import { createContext, useContext, useReducer, type ReactNode } from "react";
import type { Message, LivePart, TUIStatus, QuestionPrompt } from "../api/types";

export interface PermissionRequest {
  tool: string;
  command?: string;
  rule?: string;
  summary?: string;
  deny_reason?: string;
  model_unavailable?: string;
  request_id: string;
}

export interface QuestionRequest {
  request_id: string;
  questions: QuestionPrompt[];
}

export interface SessionContextMetrics {
  currentTokens: number;
  maxTokens: number;
  model: string;
}

/** Per-session chat state — one entry per open tab, keyed by session id (or
 *  the temporary `new-<ts>` tab id before the first message creates a real
 *  session). Kept in `ChatState.sessions` so every open tab can render and
 *  stream independently instead of sharing one global "current session". */
export interface SessionSlice {
  messages: Message[];
  // In-progress turn, streamed live until the turn_done snapshot commits it.
  live: LivePart[];
  isStreaming: boolean;
  error: string | null;
  pendingPermission: PermissionRequest | null;
  pendingQuestion: QuestionRequest | null;
  totalMessages: number; // total messages on server
  hasMore: boolean; // whether older messages exist
  loadingMore: boolean; // currently fetching older messages
  // True once this session's first page has been fetched at least once.
  // Lets ChatPanel skip re-fetching on remount and lets OpenSessionBar know
  // when a tab's "loading" spinner should clear.
  initialized: boolean;
}

export const emptySessionSlice: SessionSlice = {
  messages: [],
  live: [],
  isStreaming: false,
  error: null,
  pendingPermission: null,
  pendingQuestion: null,
  totalMessages: 0,
  hasMore: false,
  loadingMore: false,
  initialized: false,
};

/** Reads one session's slice, falling back to the shared empty default for a
 *  session that hasn't been touched yet (or a null/missing id). Never
 *  mutated — always spread when producing an updated slice. */
export function getSessionSlice(
  state: ChatState,
  sessionId: string | null | undefined,
): SessionSlice {
  if (!sessionId) return emptySessionSlice;
  return state.sessions[sessionId] ?? emptySessionSlice;
}

export interface ChatState {
  sessions: Record<string, SessionSlice>;
  // Global fields: these reflect the single backend TUI/process, not any one
  // tab, so they stay flat on the top-level state.
  model: string | null;
  smallModel: string | null;
  smallModelEnabled: boolean;
  advisorModel: string | null;
  advisorEnabled: boolean;
  ocrModel: string | null;
  ocrEnabled: boolean;
  ocrBackend: string | null;
  // Live TUI status (model, advisor, IDE, session, cwd, context, spending,
  // modified files, LSP servers, extra paths). Updated by the SSE "status"
  // event so the bar tracks the TUI without polling. Null until the first
  // event arrives or the initial fetch resolves.
  tuiStatus: TUIStatus | null;
  sessionContext: SessionContextMetrics | null;
  spendingUSD: number | null;
  // True once the very first /api/tui-status fetch has resolved. Lets the UI
  // show "loading…" vs. "not connected" while waiting for the first frame.
  tuiStatusReady: boolean;
}

export type ChatAction =
  | { type: "ADD_MESSAGE"; sessionId: string; message: Message }
  | { type: "SET_MESSAGES"; sessionId: string; messages: Message[] }
  | { type: "SET_MODEL"; model: string }
  | { type: "SET_SMALL_MODEL"; model: string }
  | { type: "SET_SMALL_MODEL_ENABLED"; enabled: boolean }
  | { type: "SET_ADVISOR_MODEL"; model: string }
  | { type: "SET_ADVISOR_ENABLED"; enabled: boolean }
  | { type: "SET_OCR_MODEL"; model: string }
  | { type: "SET_OCR_ENABLED"; enabled: boolean }
  | { type: "SET_OCR_BACKEND"; backend: string }
  | { type: "SET_STREAMING"; sessionId: string; isStreaming: boolean }
  | { type: "SET_ERROR"; sessionId: string; error: string | null }
  | { type: "APPEND_DELTA"; sessionId: string; delta: string }
  | { type: "LIVE_DELTA"; sessionId: string; kind: "thinking" | "text"; delta: string }
  | { type: "LIVE_TOOL_START"; sessionId: string; tool: string; command?: string }
  | { type: "LIVE_TOOL_RESULT"; sessionId: string; output: string }
  | { type: "LIVE_RESET"; sessionId: string }
  | { type: "PERMISSION_REQUEST"; sessionId: string; permission: PermissionRequest }
  | { type: "PERMISSION_RESOLVED"; sessionId: string }
  | { type: "QUESTION_REQUEST"; sessionId: string; question: QuestionRequest }
  | { type: "QUESTION_RESOLVED"; sessionId: string }
  | { type: "PREPEND_MESSAGES"; sessionId: string; messages: Message[]; total: number }
  | { type: "SET_LOADING_MORE"; sessionId: string; loading: boolean }
  | { type: "MERGE_SNAPSHOT"; sessionId: string; messages: Message[]; total: number }
  | { type: "SET_TOTAL"; sessionId: string; total: number }
  | { type: "SET_SESSION_CONTEXT"; context: SessionContextMetrics | null }
  | { type: "SET_SPENDING"; spendingUSD: number | null }
  | { type: "SET_TUI_STATUS"; status: TUIStatus }
  | { type: "SET_TUI_STATUS_READY"; ready: boolean }
  | { type: "REKEY_SESSION"; oldId: string; newId: string }
  | { type: "RESET"; sessionId: string };

export const initialState: ChatState = {
  sessions: {},
  model: null,
  smallModel: null,
  smallModelEnabled: false,
  advisorModel: null,
  advisorEnabled: true,
  ocrModel: null,
  ocrEnabled: false,
  ocrBackend: "openai-compat",
  tuiStatus: null,
  sessionContext: null,
  spendingUSD: null,
  tuiStatusReady: false,
};

function updateSession(
  state: ChatState,
  sessionId: string,
  updater: (slice: SessionSlice) => SessionSlice,
): ChatState {
  const current = state.sessions[sessionId] ?? emptySessionSlice;
  return { ...state, sessions: { ...state.sessions, [sessionId]: updater(current) } };
}

export function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case "ADD_MESSAGE":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        messages: [...s.messages, action.message],
      }));
    case "SET_MESSAGES":
      // Authoritative snapshot lands at a turn boundary — commit it and clear
      // the live buffer it supersedes.
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        messages: action.messages,
        live: [],
      }));
    case "SET_MODEL":
      return { ...state, model: action.model };
    case "SET_SMALL_MODEL":
      return { ...state, smallModel: action.model };
    case "SET_SMALL_MODEL_ENABLED":
      return { ...state, smallModelEnabled: action.enabled };
    case "SET_ADVISOR_MODEL":
      return { ...state, advisorModel: action.model };
    case "SET_ADVISOR_ENABLED":
      return { ...state, advisorEnabled: action.enabled };
    case "SET_OCR_MODEL":
      return { ...state, ocrModel: action.model };
    case "SET_OCR_ENABLED":
      return { ...state, ocrEnabled: action.enabled };
    case "SET_OCR_BACKEND":
      return { ...state, ocrBackend: action.backend };
    case "SET_STREAMING":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        isStreaming: action.isStreaming,
      }));
    case "SET_ERROR":
      return updateSession(state, action.sessionId, (s) => ({ ...s, error: action.error }));
    case "APPEND_DELTA":
      return updateSession(state, action.sessionId, (s) => {
        const msgs = [...s.messages];
        const last = msgs[msgs.length - 1];
        if (last && last.role === "assistant") {
          msgs[msgs.length - 1] = { ...last, content: last.content + action.delta };
        } else {
          msgs.push({ role: "assistant", content: action.delta });
        }
        return { ...s, messages: msgs };
      });
    case "LIVE_DELTA":
      return updateSession(state, action.sessionId, (s) => {
        const live = [...s.live];
        const last = live[live.length - 1];
        if (last && last.kind === action.kind) {
          live[live.length - 1] = { ...last, text: last.text + action.delta };
        } else {
          live.push({ kind: action.kind, text: action.delta });
        }
        return { ...s, live };
      });
    case "LIVE_TOOL_START":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        live: [...s.live, { kind: "tool", tool: action.tool, command: action.command }],
      }));
    case "LIVE_TOOL_RESULT":
      return updateSession(state, action.sessionId, (s) => {
        const live = [...s.live];
        // Attach to the most recent tool part still awaiting its result.
        for (let i = live.length - 1; i >= 0; i--) {
          const part = live[i];
          if (part.kind === "tool" && part.output === undefined) {
            live[i] = { ...part, output: action.output };
            return { ...s, live };
          }
        }
        return s;
      });
    case "LIVE_RESET":
      return updateSession(state, action.sessionId, (s) => ({ ...s, live: [] }));
    case "PERMISSION_REQUEST":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        pendingPermission: action.permission,
      }));
    case "PERMISSION_RESOLVED":
      return updateSession(state, action.sessionId, (s) => ({ ...s, pendingPermission: null }));
    case "QUESTION_REQUEST":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        pendingQuestion: action.question,
      }));
    case "QUESTION_RESOLVED":
      return updateSession(state, action.sessionId, (s) => ({ ...s, pendingQuestion: null }));
    case "RESET": {
      const sessions = { ...state.sessions };
      delete sessions[action.sessionId];
      return { ...state, sessions };
    }
    case "REKEY_SESSION": {
      const slice = state.sessions[action.oldId];
      if (!slice) return state; // already rekeyed by a racing dispatch — no-op
      const sessions = { ...state.sessions };
      delete sessions[action.oldId];
      sessions[action.newId] = slice;
      return { ...state, sessions };
    }
    case "SET_SESSION_CONTEXT":
      return { ...state, sessionContext: action.context };
    case "SET_SPENDING":
      return { ...state, spendingUSD: action.spendingUSD };
    case "SET_TUI_STATUS":
      return { ...state, tuiStatus: action.status, tuiStatusReady: true };
    case "SET_TUI_STATUS_READY":
      return { ...state, tuiStatusReady: action.ready };
    case "PREPEND_MESSAGES":
      // Older messages loaded via scroll-up. Prepend and update pagination state.
      return updateSession(state, action.sessionId, (s) => {
        const hasMore = action.messages.length > 0 && s.messages.length + action.messages.length < action.total;
        return {
          ...s,
          messages: [...action.messages, ...s.messages],
          totalMessages: action.total,
          hasMore,
          loadingMore: false,
        };
      });
    case "SET_TOTAL":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        totalMessages: action.total,
        hasMore: s.messages.length < action.total,
      }));
    case "SET_LOADING_MORE":
      return updateSession(state, action.sessionId, (s) => ({ ...s, loadingMore: action.loading }));
    case "MERGE_SNAPSHOT":
      // Merge snapshot into current state.
      // If action.messages is a full snapshot (length == total), replace all.
      // Otherwise it's a paginated subset — the initial page load.
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        messages: action.messages,
        totalMessages: action.total,
        hasMore: action.messages.length < action.total,
        live: [],
        initialized: true,
      }));
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && npx vitest run src/stores/chatStore.test.tsx`
Expected: PASS

- [ ] **Step 5: Confirm the rest of the app doesn't type-check yet (expected)**

Run: `cd web && npx tsgo --noEmit 2>&1 | head -50` (or `bun run typecheck` if configured — check `package.json`)
Expected: Many errors in `ChatPanel.tsx`, `SessionTabSync.tsx`, `useChat.ts`, `App.tsx`, `OpenSessionBar.tsx`, `SessionDialog.tsx`, `AgentPreview.tsx`, `AgentsPanel.tsx`, `StatusBar.tsx`, `CoworkSidebar.tsx`, `SessionSidebar.tsx` — all fixed by Tasks 3-10. This step is just confirming the compiler agrees with the plan's file list; nothing to fix here yet.

- [ ] **Step 6: Commit**

```bash
cd web && git add src/stores/chatStore.tsx src/stores/chatStore.test.tsx
git commit -m "feat(web): split chatStore into per-session slices"
```

---

### Task 3: `useChat` hook — explicit session id

**Files:**
- Modify: `web/src/hooks/useChat.ts` (full rewrite)

**Interfaces:**
- Consumes: `getSessionSlice`, `SessionSlice` from `../stores/chatStore` (Task 2).
- Produces: `useChat(sessionId: string | null, options?: UseChatOptions): {...}` — same return shape as before (`sendMessage`, `executeShell`, `stop`, `resolvePermission`, `submitQuestionAnswers`, `isStreaming`, `pendingPermission`, `pendingQuestion`), now scoped to `sessionId`. `UseChatOptions` drops `requestId` (redundant with the new `sessionId` param — the caller already passes the tab id as `sessionId`, so the `api.chat()` correlator uses that directly).

- [ ] **Step 1: Rewrite `useChat.ts`**

```ts
import { useCallback } from "react";
import { useChatState, useChatDispatch, getSessionSlice } from "../stores/chatStore";
import { api } from "../api/client";
import type { QuestionAnswerPayload } from "../api/types";

interface UseChatOptions {
  /** Called when a new session is created (first message from an empty tab). */
  onNewSession?: (sessionId: string) => void;
}

// sessionId is the tab this hook is scoped to — a real session id, a
// temporary `new-<ts>` tab id (before the first message creates a session),
// or null when no tab is active.
export function useChat(sessionId: string | null, options?: UseChatOptions) {
  const state = useChatState();
  const dispatch = useChatDispatch();
  const slice = getSessionSlice(state, sessionId);

  // Submit is fire-and-forget: the message is forwarded to the TUI's agent and
  // ALL rendering (the user echo, live thinking/text tokens, tool activity, and
  // the final answer) arrives over the persistent mirror stream in
  // SessionTabSync. This keeps a single source of truth and makes the view
  // identical whether the turn was started here or in the TUI.
  const sendMessage = useCallback(
    (content: string) => {
      if (!sessionId) return;
      const isRealSession = !sessionId.startsWith("new-");
      dispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
      dispatch({ type: "SET_ERROR", sessionId, error: null });

      // A `new-*` tab has no session yet — api.chat() creates one and the
      // request_id (this tab's id) lets SessionTabSync rekey the tab once the
      // "session_started" event (or this response, whichever wins the race)
      // reports the real session id.
      const submitPromise = isRealSession
        ? api.sendMessage(sessionId, content)
        : api.chat(content, undefined, undefined, sessionId).then((res) => {
            options?.onNewSession?.(res.sessionId);
            return res;
          });

      // HandleSendMessage blocks until the turn completes; the mirror's turn_done
      // is the primary completion signal. The .then is a safety net in case that
      // frame is missed; the .catch surfaces a failed submit.
      submitPromise
        .then(() => dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false }))
        .catch((err) => {
          dispatch({ type: "SET_ERROR", sessionId, error: err.message || "send failed" });
          dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
        });
    },
    [sessionId, dispatch, options?.onNewSession],
  );

  // Local stop: the browser can't cancel the TUI's agent, so this only releases
  // the input. The turn continues in the TUI and the mirror will still commit it.
  const stop = useCallback(() => {
    if (!sessionId) return;
    dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
  }, [dispatch, sessionId]);

  // Resolve a pending agent permission ask via the dedicated resolve endpoint
  // (NOT the config POST /api/permissions, which sets a tool rule). Only a
  // confirmed success dismisses the dialog; failures keep it open so the user
  // can retry.
  const resolvePermission = useCallback(
    async (requestId: string, approved: boolean) => {
      if (!sessionId) return false;
      try {
        await api.resolvePermission(requestId, sessionId, approved);
        dispatch({ type: "PERMISSION_RESOLVED", sessionId });
        return true;
      } catch (err) {
        console.error("Failed to resolve permission:", err);
        dispatch({
          type: "SET_ERROR",
          sessionId,
          error: err instanceof Error ? err.message : "permission resolve failed",
        });
        return false;
      }
    },
    [dispatch, sessionId],
  );

  // Submit answers to a pending agent question prompt. Mirrors the TUI's
  // submitQuestionAnswers: all answers go in one POST, and only a confirmed
  // success dismisses the dialog. Failures keep it open and surface an error.
  const submitQuestionAnswers = useCallback(
    async (requestId: string, answers: QuestionAnswerPayload[]) => {
      if (!sessionId) return false;
      try {
        await api.answerQuestion(requestId, sessionId, answers);
        dispatch({ type: "QUESTION_RESOLVED", sessionId });
        return true;
      } catch (err) {
        console.error("Failed to answer question:", err);
        dispatch({
          type: "SET_ERROR",
          sessionId,
          error: err instanceof Error ? err.message : "question answer failed",
        });
        return false;
      }
    },
    [dispatch, sessionId],
  );

  // Execute a shell command directly (for ! prefix commands)
  const executeShell = useCallback(
    async (
      command: string,
    ): Promise<{ output: string; exitCode: number; error: string }> => {
      try {
        return await api.shellCommand(command);
      } catch (err) {
        return {
          output: "",
          exitCode: 1,
          error:
            err instanceof Error ? err.message : "Failed to execute command",
        };
      }
    },
    [],
  );

  return {
    sendMessage,
    executeShell,
    stop,
    resolvePermission,
    submitQuestionAnswers,
    isStreaming: slice.isStreaming,
    pendingPermission: slice.pendingPermission,
    pendingQuestion: slice.pendingQuestion,
  };
}
```

- [ ] **Step 2: Update `ChatInput.tsx`'s call site**

In `web/src/components/Chat/ChatInput.tsx`, change:

```ts
  const { sendMessage, executeShell, stop, isStreaming } = useChat({
    requestId: sessionTabId ?? undefined,
    onNewSession: (sessionId) => {
      if (sessionTabId?.startsWith("new-")) {
        onSessionCreated?.(sessionTabId, sessionId);
      }
    },
  });
```

to:

```ts
  const { sendMessage, executeShell, stop, isStreaming } = useChat(sessionTabId ?? null, {
    onNewSession: (sessionId) => {
      if (sessionTabId?.startsWith("new-")) {
        onSessionCreated?.(sessionTabId, sessionId);
      }
    },
  });
```

(App.tsx's own `useChat()` call site is updated in Task 6, once `activeTabId` is wired there.)

- [ ] **Step 3: Verify with typecheck (full fix lands after later tasks, but confirm this file is now internally consistent)**

Run: `cd web && npx tsgo --noEmit 2>&1 | grep -E "useChat.ts|ChatInput.tsx"`
Expected: No errors from these two files (errors from other not-yet-updated files are expected and are handled in later tasks).

- [ ] **Step 4: Commit**

```bash
cd web && git add src/hooks/useChat.ts src/components/Chat/ChatInput.tsx
git commit -m "feat(web): scope useChat to an explicit session id"
```

---

### Task 4: `ChatPanel` — per-tab instance

**Files:**
- Modify: `web/src/components/Chat/ChatPanel.tsx` (full rewrite)

**Interfaces:**
- Consumes: `getSessionSlice` from `../../stores/chatStore` (Task 2); `useProjectState` from `../../stores/projectStore` (existing, for the title-update dispatch — see Task 1 fix, which makes this safe even for a background-project tab).
- Produces: `ChatPanel` now takes a required `sessionId: string` prop. No more implicit "current session" — the parent decides which session each instance renders (Task 6).

- [ ] **Step 1: Rewrite `ChatPanel.tsx`**

```tsx
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useChatState, useChatDispatch, getSessionSlice } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { api } from "../../api/client";
import MessageBubble, { AssistantText } from "./MessageBubble";
import { ThinkingBlock, ToolBlock } from "./TurnParts";
import ChatSearchBar, { messageMatchesQuery } from "./ChatSearchBar";

const PAGE_SIZE = 50;

interface ChatPanelProps {
  /** The tab this instance renders — a real session id or a temporary
   *  `new-<ts>` tab id. One ChatPanel is mounted per open tab (App.tsx),
   *  so this never changes across this instance's lifetime. */
  sessionId: string;
}

export default function ChatPanel({ sessionId }: ChatPanelProps) {
  const chatState = useChatState();
  const dispatch = useChatDispatch();
  const { dispatch: projectDispatch } = useProjectState();
  const { messages, live, hasMore, loadingMore } = getSessionSlice(chatState, sessionId);
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const topRef = useRef<HTMLDivElement>(null);
  const [initialized, setInitialized] = useState(false);
  const loadGenerationRef = useRef(0);
  const stateRef = useRef(chatState);
  stateRef.current = chatState;
  const [reachedTop, setReachedTop] = useState(false);
  // Whether the viewport is pinned to the bottom. Driven by handleScroll and
  // consulted by the auto-scroll effect so we only follow the tail when the
  // user is already at the bottom (and resume reliably after they return).
  const atBottomRef = useRef(true);
  const [showJumpToBottom, setShowJumpToBottom] = useState(false);

  // In-chat find bar (Ctrl/Cmd+F). Client-side, searches only loaded messages.
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [matchCursor, setMatchCursor] = useState(-1);
  // Per-message DOM refs so the current match can be scrolled into view.
  const messageRefs = useRef<(HTMLDivElement | null)[]>([]);
  // Set true while a search jump is scrolling so handleScroll doesn't fire the
  // scroll-up pagination loader (which would shift every message index and
  // land the highlight on the wrong bubble).
  const searchJumpRef = useRef(false);

  // Match indices: message positions containing the query (case-insensitive).
  // Only loaded messages are searched — the "searching loaded messages" hint
  // in the bar sets that expectation.
  const matchIndices = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    if (!q) return [] as number[];
    const out: number[] = [];
    messages.forEach((msg, i) => {
      if (messageMatchesQuery(msg, q)) out.push(i);
    });
    return out;
  }, [messages, searchQuery]);

  const currentMatchMsgIndex =
    matchCursor >= 0 && matchCursor < matchIndices.length
      ? matchIndices[matchCursor]
      : -1;

  // Initial load: fetch this session's last 50 messages once (skipped for a
  // `new-*` tab, which has no session yet, and for a session whose slice is
  // already initialized — e.g. this ChatPanel remounted, or a live SSE event
  // populated the slice before this fetch resolved).
  useEffect(() => {
    const generation = ++loadGenerationRef.current;
    let cancelled = false;

    if (!sessionId || sessionId.startsWith("new-")) {
      setInitialized(true);
      return () => {
        cancelled = true;
      };
    }
    if (getSessionSlice(stateRef.current, sessionId).initialized) {
      setInitialized(true);
      return () => {
        cancelled = true;
      };
    }
    setInitialized(false);

    api
      .getSession(sessionId, { limit: PAGE_SIZE })
      .then((detail) => {
        if (cancelled || generation !== loadGenerationRef.current) return;
        // A live mirror event may have arrived while the initial history fetch
        // was in flight. Its in-memory state is newer than disk; do not wipe
        // it with this older snapshot.
        const current = getSessionSlice(stateRef.current, sessionId);
        if (current.messages.length > 0 || current.live.length > 0) {
          setInitialized(true);
          return;
        }
        dispatch({
          type: "MERGE_SNAPSHOT",
          sessionId,
          messages: detail.messages,
          total: detail.total,
        });
        if (detail.title && detail.title !== sessionId) {
          projectDispatch({ type: "UPDATE_TAB_TITLE", id: sessionId, title: detail.title });
        }
        setInitialized(true);
        // Scroll to bottom after initial render
        requestAnimationFrame(() => {
          const el = scrollRef.current;
          if (el) {
            el.scrollTop = el.scrollHeight;
            atBottomRef.current = true;
            setShowJumpToBottom(false);
          }
        });
      })
      .catch((err) => {
        if (cancelled || generation !== loadGenerationRef.current) return;
        console.error("Failed to load session:", err);
        setInitialized(true);
      });
    return () => {
      cancelled = true;
    };
  }, [sessionId, dispatch, projectDispatch]);

  // Auto-scroll to bottom on new messages/live content, but ONLY when the user
  // is already pinned to the bottom. We scroll instantly (not smooth) so a
  // burst of streaming tokens can't start a competing smooth animation — that
  // competition is what caused the down/up bounce and eventual lockout. The
  // explicit "jump to bottom" button uses smooth scrolling instead.
  useEffect(() => {
    if (!initialized) return;
    const el = scrollRef.current;
    if (!el) return;
    if (atBottomRef.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [messages, live, initialized]);

  // Toggle the find bar with Ctrl/Cmd+F. Local to this tab: each ChatPanel
  // instance is only visible while its tab is active (App.tsx CSS-hides the
  // rest), so this window listener would fire for every open tab — guard on
  // visibility via the DOM (offsetParent is null while `hidden`).
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === "f" && (e.metaKey || e.ctrlKey)) {
        if (scrollRef.current?.offsetParent === null) return;
        e.preventDefault();
        setSearchOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  const closeSearch = useCallback(() => {
    setSearchOpen(false);
    setSearchQuery("");
    setMatchCursor(-1);
  }, []);

  // Reset the cursor to the first match whenever the match set changes (new
  // query, or the loaded message set shifted). -1 when there is nothing to jump
  // to so the counter reads "No matches" instead of "1/0".
  useEffect(() => {
    setMatchCursor(matchIndices.length > 0 ? 0 : -1);
  }, [matchIndices]);

  // Scroll the current match into view. Flag the jump so handleScroll skips the
  // pagination loader while the smooth scroll settles.
  useEffect(() => {
    if (currentMatchMsgIndex < 0) return;
    const el = messageRefs.current[currentMatchMsgIndex];
    if (!el) return;
    atBottomRef.current = false;
    setShowJumpToBottom(true);
    searchJumpRef.current = true;
    el.scrollIntoView({ behavior: "smooth", block: "center" });
    const t = setTimeout(() => {
      searchJumpRef.current = false;
    }, 600);
    return () => clearTimeout(t);
  }, [currentMatchMsgIndex]);

  const gotoNextMatch = useCallback(() => {
    setMatchCursor((c) =>
      matchIndices.length === 0 ? -1 : (c + 1) % matchIndices.length,
    );
  }, [matchIndices.length]);

  const gotoPrevMatch = useCallback(() => {
    setMatchCursor((c) =>
      matchIndices.length === 0
        ? -1
        : (c - 1 + matchIndices.length) % matchIndices.length,
    );
  }, [matchIndices.length]);

  // Pin to bottom immediately (used by the "jump to bottom" affordance).
  const scrollToBottom = useCallback((smooth = false) => {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTo({ top: el.scrollHeight, behavior: smooth ? "smooth" : "auto" });
    requestAnimationFrame(() => {
      atBottomRef.current = true;
      setShowJumpToBottom(false);
    });
  }, []);

  // Scroll-up handler: load older messages when near top, and track whether we
  // are pinned to the bottom so the auto-scroll effect can decide to follow.
  // Uses requestAnimationFrame to defer the scroll position check, giving the
  // auto-scroll useEffect a chance to scroll first. This prevents a race where
  // content growth fires a scroll event before the effect runs, which would
  // incorrectly disable auto-scroll during lengthy tool call results.
  const rafRef = useRef<number>(0);
  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;

    cancelAnimationFrame(rafRef.current);
    rafRef.current = requestAnimationFrame(() => {
      const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
      const atBottom = distanceFromBottom < 200;
      atBottomRef.current = atBottom;
      setShowJumpToBottom(!atBottom);
    });

    setReachedTop(el.scrollTop < 5);

    if (!hasMore || loadingMore || sessionId.startsWith("new-") || searchJumpRef.current) return;
    if (el.scrollTop < 100) {
      const currentCount = messages.length;
      dispatch({ type: "SET_LOADING_MORE", sessionId, loading: true });

      api
        .getSession(sessionId, { limit: PAGE_SIZE, offset: currentCount })
        .then((detail) => {
          if (detail.messages.length > 0) {
            const scrollHeightBefore = el.scrollHeight;
            dispatch({
              type: "PREPEND_MESSAGES",
              sessionId,
              messages: detail.messages,
              total: detail.total,
            });
            requestAnimationFrame(() => {
              const scrollHeightAfter = el.scrollHeight;
              el.scrollTop = scrollHeightAfter - scrollHeightBefore;
            });
          } else {
            dispatch({ type: "SET_LOADING_MORE", sessionId, loading: false });
          }
        })
        .catch(() => {
          dispatch({ type: "SET_LOADING_MORE", sessionId, loading: false });
        });
    }
  }, [hasMore, loadingMore, messages.length, sessionId, dispatch]);

  return (
    <div className="relative h-full min-h-0">
      {searchOpen && (
        <div className="absolute inset-x-0 top-0 z-20">
          <ChatSearchBar
            query={searchQuery}
            onQueryChange={setSearchQuery}
            matchCount={matchIndices.length}
            current={matchCursor}
            onNext={gotoNextMatch}
            onPrev={gotoPrevMatch}
            onClose={closeSearch}
          />
        </div>
      )}
      <div
        ref={scrollRef}
        className="absolute inset-0 overflow-y-auto p-4"
        onScroll={handleScroll}
      >
        {initialized && messages.length > 0 && (
          <div ref={topRef} className="py-4">
            {loadingMore && (
              <div className="text-center text-zinc-500 text-sm py-2">
                Loading older messages…
              </div>
            )}
            {!loadingMore && !hasMore && reachedTop && (
              <div className="text-center text-zinc-600 text-xs py-2 border-b border-zinc-800 mb-4">
                Beginning of conversation
              </div>
            )}
            {!loadingMore && hasMore && !reachedTop && (
              <div className="text-center text-zinc-600 text-xs py-2">
                ↑ Scroll up for older messages
              </div>
            )}
            {!loadingMore && hasMore && reachedTop && (
              <div className="text-center text-zinc-500 text-sm py-2">
                Loading older messages…
              </div>
            )}
          </div>
        )}

        {messages.length === 0 && live.length === 0 && initialized && (
          <div className="flex h-full items-center justify-center text-zinc-500">
            Start a conversation
          </div>
        )}

        {messages.map((msg, i) => {
          if (
            msg.role === "tool" &&
            (msg.content?.startsWith("QUESTION_PROMPT:") ||
              msg.content?.startsWith("PERMISSION_ASK:"))
          ) {
            return null;
          }
          return (
          <div
            key={`${msg.role}-${i}-${msg.content?.slice(0, 20)}`}
            ref={(el) => {
              messageRefs.current[i] = el;
            }}
            className={
              i === currentMatchMsgIndex
                ? "scroll-mt-16 rounded-lg ring-2 ring-yellow-400/70 ring-offset-2 ring-offset-zinc-950"
                : "scroll-mt-16"
            }
          >
            <MessageBubble message={msg} highlight={searchOpen ? searchQuery : ""} />
          </div>
          );
        })}

        {live.map((part, i) => {
          if (part.kind === "thinking")
            return <ThinkingBlock key={`live-${i}`} text={part.text} />;
          if (part.kind === "text")
            return <AssistantText key={`live-${i}`} content={part.text} />;
          return (
            <ToolBlock
              key={`live-${i}`}
              tool={part.tool}
              command={part.command}
              output={part.output}
            />
          );
        })}

        {loadingMore && messages.length > 0 && (
          <div className="text-center text-zinc-500 text-sm py-2">
            Loading…
          </div>
        )}

        <div ref={bottomRef} />
      </div>
      {showJumpToBottom && (
        <button
          type="button"
          onClick={() => scrollToBottom(true)}
          className="absolute bottom-4 right-4 z-10 flex h-9 w-9 items-center justify-center rounded-full bg-zinc-700 text-zinc-100 shadow-lg transition-colors hover:bg-zinc-600"
          title="Scroll to bottom"
          aria-label="Scroll to bottom"
        >
          ↓
        </button>
      )}
    </div>
  );
}
```

Two behavioral notes versus the original, both required by the multi-mount change:
- Root class changed from `relative flex-1 min-h-0` to `relative h-full min-h-0`: each `ChatPanel` is now wrapped in an `absolute inset-0` positioning box (Task 6), not a direct flex child, so it needs `h-full` to fill that box instead of `flex-1`.
- The Cmd/Ctrl+F listener now guards on `offsetParent === null` (true when a CSS-hidden ancestor exists) so every open tab's `ChatPanel` — all mounted simultaneously — doesn't independently pop its own search bar when only one is visible.

- [ ] **Step 2: Commit**

```bash
cd web && git add src/components/Chat/ChatPanel.tsx
git commit -m "feat(web): ChatPanel renders one session per instance"
```

(Full manual verification of scrolling/search happens in Task 11, after App.tsx wires up multi-mount in Task 6 — this component won't render correctly stacked until then.)

---

### Task 5: `SessionTabSync` — route SSE to every open session

**Files:**
- Modify: `web/src/components/Layout/SessionTabSync.tsx` (full rewrite)

**Interfaces:**
- Consumes: `REKEY_SESSION` action (Task 2), `UPDATE_TAB_ID`/`UPDATE_TAB_TITLE` fixed in Task 1.
- Produces: no change to the component's public surface (still `<SessionTabSync />`, no props) — only its internal event routing changes.

- [ ] **Step 1: Rewrite `SessionTabSync.tsx`**

```tsx
import { useEffect, useRef } from "react";
import { useChatDispatch, useChatState } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { connectSessionMirror } from "../../api/client";
import type { Message, SSEPermissionEvent, TUIStatus } from "../../api/types";

/**
 * SessionTabSync owns the cross-cutting behaviors of the multi-session tab
 * bar on the Home view:
 *
 * 1. Live mirror subscription — one persistent, unfiltered stream for status
 *    and chat events. Every event is routed to its own session's slice in
 *    chatStore as long as that session has an open tab (in any project) —
 *    not just the currently active one, so background tabs keep streaming.
 * 2. Tab title replacement — live status keeps tab labels current.
 */
export default function SessionTabSync() {
  const chatState = useChatState();
  const chatDispatch = useChatDispatch();
  const { state: projectState, dispatch: projectDispatch } = useProjectState();

  // All open tab ids across every project, recomputed each render so the SSE
  // handler (a stable closure inside the effect below) always sees the
  // current set via this ref.
  const openSessionIdsRef = useRef<Set<string>>(new Set());
  openSessionIdsRef.current = new Set(
    Object.values(projectState.tabsByProject).flat().map((tab) => tab.id),
  );

  // One persistent mirror for status and chat events. The server places the
  // session ID in the SSE `id` field; existing event payloads remain unchanged.
  useEffect(() => {
    return connectSessionMirror(undefined, (event, data, transportSessionId) => {
      const payloadSessionId =
        data && typeof data === "object" && "session_id" in data
          ? String((data as { session_id?: unknown }).session_id || "")
          : "";
      const eventSessionId = transportSessionId || payloadSessionId || null;

      if (event === "session_started") {
        const started = data as { session_id?: string; request_id?: string };
        // A `new-*` tab's first message correlates via request_id. Rekey it
        // to the real session id — idempotent, so it's safe even if the
        // direct api.chat() response (App.tsx's handleSessionCreated) wins
        // this race instead.
        if (
          started.request_id &&
          started.request_id.startsWith("new-") &&
          eventSessionId &&
          openSessionIdsRef.current.has(started.request_id) &&
          !openSessionIdsRef.current.has(eventSessionId)
        ) {
          chatDispatch({
            type: "REKEY_SESSION",
            oldId: started.request_id,
            newId: eventSessionId,
          });
          projectDispatch({
            type: "UPDATE_TAB_ID",
            oldId: started.request_id,
            newId: eventSessionId,
            newTitle: "New session",
          });
        }
        return;
      }

      if (event === "status") {
        const status = data as TUIStatus;
        chatDispatch({ type: "SET_TUI_STATUS", status });
        if (status.advisor_enabled !== undefined) {
          chatDispatch({ type: "SET_ADVISOR_ENABLED", enabled: !!status.advisor_enabled });
        }
        if (status.advisor_model !== undefined) {
          chatDispatch({ type: "SET_ADVISOR_MODEL", model: status.advisor_model });
        }
        if (status.small_model !== undefined) {
          chatDispatch({ type: "SET_SMALL_MODEL", model: status.small_model });
        }
        if (status.ocr_backend !== undefined) {
          chatDispatch({ type: "SET_OCR_BACKEND", backend: status.ocr_backend || "openai-compat" });
        }
        if (status.main_model) {
          chatDispatch({ type: "SET_MODEL", model: status.main_model });
        }
        if (status.ocr_enabled !== undefined) {
          chatDispatch({ type: "SET_OCR_ENABLED", enabled: !!status.ocr_enabled });
        }
        if (status.ocr_model !== undefined) {
          chatDispatch({ type: "SET_OCR_MODEL", model: status.ocr_model });
        }
        return;
      }

      // Every other event type is per-session — route it to that session's
      // slice as long as it has an open tab. A session with no open tab (e.g.
      // driven purely from the TUI, never opened here) is ignored, same as
      // today's effective behavior for sessions this UI has never seen.
      if (!eventSessionId || !openSessionIdsRef.current.has(eventSessionId)) {
        return;
      }
      const sessionId = eventSessionId;

      switch (event) {
        case "messages": {
          const snapshot = Array.isArray(data)
            ? (data as Message[])
            : ((data as { messages?: Message[] }).messages ?? []);
          chatDispatch({ type: "SET_MESSAGES", sessionId, messages: snapshot });
          chatDispatch({ type: "SET_TOTAL", sessionId, total: snapshot.length });
          break;
        }
        case "user_message":
          chatDispatch({
            type: "ADD_MESSAGE",
            sessionId,
            message: { role: "user", content: (data as { content: string }).content },
          });
          chatDispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
          break;
        case "thinking":
          chatDispatch({
            type: "LIVE_DELTA",
            sessionId,
            kind: "thinking",
            delta: (data as { delta: string }).delta,
          });
          chatDispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
          break;
        case "text":
          chatDispatch({
            type: "LIVE_DELTA",
            sessionId,
            kind: "text",
            delta: (data as { delta: string }).delta,
          });
          chatDispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
          break;
        case "tool_start":
          chatDispatch({
            type: "LIVE_TOOL_START",
            sessionId,
            tool: (data as { tool: string }).tool,
            command: (data as { command?: string }).command,
          });
          chatDispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
          break;
        case "tool_result":
          chatDispatch({
            type: "LIVE_TOOL_RESULT",
            sessionId,
            output: (data as { output: string }).output,
          });
          break;
        case "turn_done":
          chatDispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
          break;
        case "question": {
          const question = data as {
            request_id: string;
            questions: import("../../api/types").QuestionPrompt[];
          };
          chatDispatch({
            type: "QUESTION_REQUEST",
            sessionId,
            question: {
              request_id: question.request_id,
              questions: question.questions,
            },
          });
          break;
        }
        case "question_resolved":
          chatDispatch({ type: "QUESTION_RESOLVED", sessionId });
          break;
        case "permission":
          chatDispatch({
            type: "PERMISSION_REQUEST",
            sessionId,
            permission: data as SSEPermissionEvent,
          });
          break;
        case "permission_resolved":
          chatDispatch({ type: "PERMISSION_RESOLVED", sessionId });
          break;
        case "error":
          chatDispatch({ type: "SET_ERROR", sessionId, error: (data as { error: string }).error });
          chatDispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
          break;
      }
    });
  }, [chatDispatch, projectDispatch]);

  // When a live status event carries a session title for one of our open
  // tabs (in any project), replace the tab label. Empty titles are ignored.
  useEffect(() => {
    const status = chatState.tuiStatus;
    if (!status?.session_title || !status.session_id) return;
    if (!openSessionIdsRef.current.has(status.session_id)) return;
    projectDispatch({
      type: "UPDATE_TAB_TITLE",
      id: status.session_id,
      title: status.session_title,
    });
  }, [chatState.tuiStatus, projectDispatch]);

  return null;
}
```

Note what's deleted versus the original: the whole first `useEffect` ("select the session represented by the active tab") is gone — it existed only to `RESET` + `SET_SESSION` + fetch on every `activeTabId` change, which `ChatPanel` now owns per-mount (Task 4) and no longer needs repeating on every switch. `loadedTabRef`, `activeSessionRef`, `pendingNewSessionRef`, `activeTabRef`, `tabsRef` are all gone with it — the multi-tab-aware `openSessionIdsRef` replaces their job.

- [ ] **Step 2: Commit**

```bash
cd web && git add src/components/Layout/SessionTabSync.tsx
git commit -m "feat(web): route SSE mirror events to every open tab, not just the active one"
```

---

### Task 6: `App.tsx` — mount every open tab, rewire the active-session source

**Files:**
- Modify: `web/src/App.tsx`

**Interfaces:**
- Consumes: `ChatPanel(sessionId)` (Task 4), `useChat(sessionId, options)` (Task 3), `REKEY_SESSION` (Task 2).

- [ ] **Step 1: Read `activeProject`/`tabsByProject` alongside `activeTabId`, drop `chatState.sessionId`**

Change (around line 93-95):

```tsx
  const { messages: chatMessages, sessionId: currentSessionId } = useChatState();
  const { resolvePermission, pendingPermission } = useChat();
  const { activeTabId, dispatch: projectDispatch } = useProjectState();
```

to:

```tsx
  const { state: projectState, activeTabId, dispatch: projectDispatch } = useProjectState();
  const { resolvePermission, pendingPermission } = useChat(activeTabId);
```

`chatMessages`/`currentSessionId` are gone — find their remaining uses below and replace each with `activeTabId` (there is no more `chatMessages` use; it was only destructured for `getMessages: () => chatMessages` in `handleCommand`, which now reads messages from the active tab's slice — see Step 3).

- [ ] **Step 2: Update the `selectedAgentRunId` reset effect**

Change:

```tsx
  useEffect(() => {
    setSelectedAgentRunId(null);
  }, [currentSessionId]);
```

to:

```tsx
  useEffect(() => {
    setSelectedAgentRunId(null);
  }, [activeTabId]);
```

- [ ] **Step 3: Update `handleCommand`'s message/session getters and the three bare `RESET` dispatches**

`handleCommand` needs the active tab's messages for `getMessages`. Add the chat state read near the top of `HomeApp` (with the other hooks, right after the `useProjectState`/`useChat` block from Step 1):

```tsx
  const chatState = useChatState();
  const dispatch = useChatDispatch();
```

(`useChatState`/`useChatDispatch` are already imported at the top of the file — only the destructuring inside `HomeApp` changes.)

Then in `handleCommand`, change:

```tsx
      getMessages: () => chatMessages,
      getSessionId: () => currentSessionId,
```

to:

```tsx
      getMessages: () => getSessionSlice(chatState, activeTabId).messages,
      getSessionId: () => activeTabId,
```

Add `getSessionSlice` to the `chatStore` import at the top of `App.tsx`:

```tsx
import { ChatProvider, useChatDispatch, useChatState, getSessionSlice } from "./stores/chatStore";
```

Change the three bare `dispatch({ type: "RESET" })` calls — the keyboard shortcut (around line 184), and inside `handleCommand`'s `/clear`/`/new` branch (around line 208) and its `result.newSession` branch (around line 254) — to:

```tsx
    onNewSession: () => {
      if (activeTabId) dispatch({ type: "RESET", sessionId: activeTabId });
    },
```

```tsx
    if (baseCmd === "/clear" || baseCmd === "/new") {
      if (activeTabId) dispatch({ type: "RESET", sessionId: activeTabId });
      return true;
    }
```

```tsx
    if (result.newSession) {
      if (activeTabId) dispatch({ type: "RESET", sessionId: activeTabId });
    }
```

- [ ] **Step 4: Rewrite `handleSessionCreated`**

Change:

```tsx
  const handleSessionCreated = (tempTabId: string, sessionId: string) => {
    projectDispatch({
      type: "UPDATE_TAB_ID",
      oldId: tempTabId,
      newId: sessionId,
      newTitle: "New session",
    });
  };
```

to:

```tsx
  // Direct fallback for the temp-tab → real-session rename: SessionTabSync
  // does the same rekey off the "session_started" SSE event, whichever
  // arrives first. REKEY_SESSION/UPDATE_TAB_ID are both idempotent (no-op if
  // the old id is already gone), so running this twice is safe.
  const handleSessionCreated = (tempTabId: string, sessionId: string) => {
    dispatch({ type: "REKEY_SESSION", oldId: tempTabId, newId: sessionId });
    projectDispatch({
      type: "UPDATE_TAB_ID",
      oldId: tempTabId,
      newId: sessionId,
      newTitle: "New session",
    });
  };
```

- [ ] **Step 5: Replace the single `<ChatPanel />` with one instance per open tab, across all projects**

Compute the full tab list once near the top of the render (after the other hooks, before the `return`):

```tsx
  const allChatTabs = Object.values(projectState.tabsByProject).flat();
```

Change the `chat` `TabsContent` block from:

```tsx
            <TabsContent value="chat" forceMount className="flex-1 overflow-hidden m-0">
              <div className="flex flex-col h-full">
                <ChatPanel />
                <AgentPreview onOpenDetail={openAgentDetail} />
                <ChatInput
                  onSlashCommand={handleCommand}
                  activeEditorContext={activeEditorContext}
                  sessionTabId={activeTabId}
                  onSessionCreated={handleSessionCreated}
                />
              </div>
            </TabsContent>
```

to:

```tsx
            <TabsContent value="chat" forceMount className="flex-1 overflow-hidden m-0">
              <div className="flex flex-col h-full">
                <div className="relative flex-1 min-h-0 overflow-hidden">
                  {allChatTabs.map((tab) => (
                    <div
                      key={tab.id}
                      className={
                        tab.projectPath === projectState.activeProject?.path &&
                        tab.id === activeTabId
                          ? "absolute inset-0"
                          : "absolute inset-0 hidden"
                      }
                    >
                      <ChatPanel sessionId={tab.id} />
                    </div>
                  ))}
                </div>
                <AgentPreview onOpenDetail={openAgentDetail} />
                <ChatInput
                  onSlashCommand={handleCommand}
                  activeEditorContext={activeEditorContext}
                  sessionTabId={activeTabId}
                  onSessionCreated={handleSessionCreated}
                />
              </div>
            </TabsContent>
```

- [ ] **Step 6: Replace `FileEditor`/`ChangesPanel`'s `session={currentSessionId}`**

Both currently read `session={currentSessionId ?? undefined}` — change both to `session={activeTabId ?? undefined}`.

- [ ] **Step 7: Remove the now-unused `ChatPanel` import if App.tsx imported `AgentPreview`'s props differently — verify import list**

`ChatPanel` is still imported and used (just with a prop now) — no import changes needed there. Double-check `useChatState`/`useChatDispatch`/`getSessionSlice` are all imported (Step 3) and `useProjectState`'s destructure includes `state` (Step 1).

- [ ] **Step 8: Typecheck**

Run: `cd web && npx tsgo --noEmit 2>&1 | grep App.tsx`
Expected: No errors from `App.tsx`. (Other files' errors are handled in Tasks 7-10.)

- [ ] **Step 9: Run the dev server and manually verify layout**

Run: `cd web && npm run dev` (or the project's existing dev command)
Open the app, open two session tabs in the same project, confirm the chat area still fills the available height and scrolls correctly in both tabs (this validates the `h-full`/`absolute inset-0` sizing chain introduced across Task 4 Step 1 and this task's Step 5).
Expected: chat area renders full-height, scrolls normally, no visible layout regression versus before this plan.

- [ ] **Step 10: Commit**

```bash
cd web && git add src/App.tsx
git commit -m "feat(web): mount one ChatPanel per open tab, source active session from projectStore"
```

---

### Task 7: `OpenSessionBar` + `SessionDialog` — active-tab comparisons, pending badges

**Files:**
- Modify: `web/src/components/Layout/OpenSessionBar.tsx`
- Modify: `web/src/components/Layout/SessionDialog.tsx`

**Interfaces:**
- Consumes: `getSessionSlice` from `../../stores/chatStore`.

- [ ] **Step 1: `OpenSessionBar.tsx` — swap `chatState.sessionId` for `activeTabId`, drop the now-unnecessary RESET, add pending badges**

Change the `useChatState`/`useChatDispatch` import line to also pull `getSessionSlice`:

```tsx
import { useChatDispatch, useChatState, getSessionSlice } from "../../stores/chatStore";
```

Change `handleCloseTab`:

```tsx
  const handleCloseTab = useCallback((e: React.MouseEvent, tabId: string) => {
    e.stopPropagation();
    closeSessionTab(tabId);
    chatDispatch({ type: "RESET", sessionId: tabId });
  }, [closeSessionTab, chatDispatch]);
```

Change `isLoadingTab`:

```tsx
  // A real session tab is "loading" while its slice hasn't finished its first
  // fetch yet (ChatPanel's own initial-load effect sets `initialized`).
  const isLoadingTab = (tabId: string) =>
    !tabId.startsWith("new-") && !getSessionSlice(chatState, tabId).initialized;
```

Change the "New session" button handler — drop the RESET dispatch entirely (a brand-new or reused-empty tab has nothing to reset):

```tsx
      <button
        onClick={() => {
          openNewSessionTab(isNewSessionTabEmpty(activeTabId));
        }}
        className="flex items-center gap-1 px-2 py-1 rounded text-xs text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition-colors shrink-0"
        title="New session"
      >
```

(`chatDispatch`/`chatState` are still used elsewhere in the file — don't remove those imports/vars, just this one call. `useChatDispatch` import for `chatDispatch` stays.)

Add a pending badge to each tab row. Inside the `tabs.map((tab) => { ... })` block, after computing `isActive`:

```tsx
      {tabs.map((tab) => {
        const isActive = activeTabId === tab.id;
        const slice = getSessionSlice(chatState, tab.id);
        const hasPending = !isActive && (slice.pendingPermission !== null || slice.pendingQuestion !== null);
        return (
          <div
            key={tab.id}
            className={`flex items-center gap-1 px-2 py-1 rounded-t text-xs cursor-pointer shrink-0 transition-colors ${
              isActive
                ? "bg-zinc-800 text-zinc-100 border-t border-t-blue-500"
                : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800/60"
            }`}
            onClick={() => handleTabClick(tab.id, tab.title)}
          >
            {isLoadingTab(tab.id) && (
              <Loader2 className="w-3 h-3 animate-spin shrink-0" />
            )}
            {hasPending && (
              <span
                className="h-1.5 w-1.5 rounded-full bg-amber-400 shrink-0"
                title="Waiting for a response in this tab"
              />
            )}
            <span className="max-w-28 truncate" title={tab.title || tab.id}>{tab.title || tab.id.slice(0, 12)}</span>
```

(The rest of that block — the close `<span>` — is unchanged.)

- [ ] **Step 2: `SessionDialog.tsx` — swap `chatState.sessionId` for `activeTabId`, drop the RESET call**

Change `handleCloseTab`:

```tsx
  const handleCloseTab = useCallback((e: React.MouseEvent, tabId: string) => {
    e.stopPropagation();
    closeSessionTab(tabId);
    chatDispatch({ type: "RESET", sessionId: tabId });
  }, [closeSessionTab, chatDispatch]);
```

Change `handleNewSession` — drop the RESET:

```tsx
  const handleNewSession = useCallback(() => {
    openNewSessionTab(isNewSessionTabEmpty(activeTabId));
    toggleSessionPicker();
    setSearchQuery("");
  }, [activeTabId, openNewSessionTab, toggleSessionPicker]);
```

`chatState` is now unused in this file if nothing else reads it — check: `isCurrentSession` already uses `activeTabId`, not `chatState`. Confirm no remaining reference to `chatState.sessionId`; if `useChatState()`'s only remaining consumer was these two spots, remove the now-unused `chatState`/`useChatState` import. (`chatDispatch`/`useChatDispatch` stays — still used by both handlers above.)

- [ ] **Step 3: Typecheck**

Run: `cd web && npx tsgo --noEmit 2>&1 | grep -E "OpenSessionBar.tsx|SessionDialog.tsx"`
Expected: No errors.

- [ ] **Step 4: Commit**

```bash
cd web && git add src/components/Layout/OpenSessionBar.tsx src/components/Layout/SessionDialog.tsx
git commit -m "feat(web): tab bar reads active tab from projectStore, shows pending-prompt badges"
```

---

### Task 8: `AgentPreview` + `AgentsPanel` — source session id from `projectStore`

**Files:**
- Modify: `web/src/components/Chat/AgentPreview.tsx`
- Modify: `web/src/components/Agents/AgentsPanel.tsx`
- Modify: `web/src/components/Chat/AgentPreview.test.tsx`
- Modify: `web/src/components/Agents/AgentsPanel.test.tsx`

**Interfaces:**
- Consumes: `useProjectState` from `../../stores/projectStore`.

- [ ] **Step 1: `AgentPreview.tsx`**

Change:

```tsx
import { useChatState } from "../../stores/chatStore";
import { useAgentRuns } from "../../hooks/useAgentRuns";
```
```tsx
  const { sessionId } = useChatState();
```

to:

```tsx
import { useProjectState } from "../../stores/projectStore";
import { useAgentRuns } from "../../hooks/useAgentRuns";
```
```tsx
  const { activeTabId: sessionId } = useProjectState();
```

- [ ] **Step 2: `AgentsPanel.tsx`**

Same swap — change:

```tsx
import { useChatState } from "../../stores/chatStore";
```
```tsx
  // Session comes from the chat store — the same single source the agent
  // preview rail uses — so a run clicked in the preview is always looked up in
  // the exact tree that produced it.
  const { sessionId } = useChatState();
```

to:

```tsx
import { useProjectState } from "../../stores/projectStore";
```
```tsx
  // Session comes from the active project tab — the same single source the
  // agent preview rail uses — so a run clicked in the preview is always
  // looked up in the exact tree that produced it.
  const { activeTabId: sessionId } = useProjectState();
```

- [ ] **Step 3: Update both tests' mocks**

In `AgentPreview.test.tsx`, change:

```tsx
vi.mock("../../stores/chatStore", () => ({
  useChatState: () => ({ sessionId: "session-1" }),
}));
```

to:

```tsx
vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({ activeTabId: "session-1" }),
}));
```

In `AgentsPanel.test.tsx`, change:

```tsx
// The panel resolves its session from the chat store, like AgentPreview does.
vi.mock("../../stores/chatStore", () => ({
  useChatState: () => ({ sessionId: "session-1" }),
}));
```

to:

```tsx
// The panel resolves its session from the active project tab, like
// AgentPreview does.
vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({ activeTabId: "session-1" }),
}));
```

- [ ] **Step 4: Run the updated tests**

Run: `cd web && npx vitest run src/components/Chat/AgentPreview.test.tsx src/components/Agents/AgentsPanel.test.tsx`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd web && git add src/components/Chat/AgentPreview.tsx src/components/Chat/AgentPreview.test.tsx \
  src/components/Agents/AgentsPanel.tsx src/components/Agents/AgentsPanel.test.tsx
git commit -m "refactor(web): AgentPreview/AgentsPanel read session id from projectStore"
```

---

### Task 9: `StatusBar` + `CoworkSidebar` — active-tab-scoped `isStreaming`/`error`/`sessionId`

**Files:**
- Modify: `web/src/components/common/StatusBar.tsx`
- Modify: `web/src/components/Layout/CoworkSidebar.tsx`

**Interfaces:**
- Consumes: `getSessionSlice` from `../../stores/chatStore`, `useProjectState` from `../../stores/projectStore`.

- [ ] **Step 1: `StatusBar.tsx`**

Change the import and destructure:

```tsx
import { useChatState } from "../../stores/chatStore";
```
```tsx
  const {
    model,
    smallModel,
    smallModelEnabled,
    advisorModel,
    advisorEnabled,
    isStreaming,
    error,
    tuiStatus,
    sessionContext,
    spendingUSD,
  } = useChatState();
```

to:

```tsx
import { useChatState, getSessionSlice } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
```
```tsx
  const {
    model,
    smallModel,
    smallModelEnabled,
    advisorModel,
    advisorEnabled,
    tuiStatus,
    sessionContext,
    spendingUSD,
  } = useChatState();
  const chatState = useChatState();
  const { activeTabId } = useProjectState();
  const { isStreaming, error } = getSessionSlice(chatState, activeTabId);
```

(Two `useChatState()` calls is redundant — collapse to one. Final version:)

```tsx
  const chatState = useChatState();
  const {
    model,
    smallModel,
    smallModelEnabled,
    advisorModel,
    advisorEnabled,
    tuiStatus,
    sessionContext,
    spendingUSD,
  } = chatState;
  const { activeTabId } = useProjectState();
  const { isStreaming, error } = getSessionSlice(chatState, activeTabId);
```

Everything else in the file (lines using `isStreaming`/`error`) is unchanged — they're still local variables with the same names, just now sourced from the active tab's slice.

- [ ] **Step 2: `CoworkSidebar.tsx`**

Change:

```tsx
  const {
    model,
    smallModel,
    smallModelEnabled,
    advisorModel,
    advisorEnabled,
    ocrModel,
    ocrEnabled,
    ocrBackend,
    sessionId,
    tuiStatus,
  } = useChatState();
  const dispatch = useChatDispatch();
```

to:

```tsx
  const {
    model,
    smallModel,
    smallModelEnabled,
    advisorModel,
    advisorEnabled,
    ocrModel,
    ocrEnabled,
    ocrBackend,
    tuiStatus,
  } = useChatState();
  const dispatch = useChatDispatch();
  const { activeTabId: sessionId } = useProjectState();
```

Add the import: `import { useProjectState } from "../../stores/projectStore";` (check the file doesn't already import it under a different local name first — it doesn't, per the earlier grep).

- [ ] **Step 3: Typecheck**

Run: `cd web && npx tsgo --noEmit 2>&1 | grep -E "StatusBar.tsx|CoworkSidebar.tsx"`
Expected: No errors.

- [ ] **Step 4: Manual verification**

Run the dev server, send a message, confirm the status bar's streaming indicator still appears/disappears correctly and the Cowork sidebar's "Session: ..." line still shows the active tab's id.

- [ ] **Step 5: Commit**

```bash
cd web && git add src/components/common/StatusBar.tsx src/components/Layout/CoworkSidebar.tsx
git commit -m "refactor(web): StatusBar/CoworkSidebar read the active tab's session state"
```

---

### Task 10: `SessionSidebar` — compile-safety fix (unused component)

**Context:** `SessionSidebar.tsx` is not imported anywhere in the app (verified via `grep -rn "from.*SessionSidebar" src/` — no hits; it's dead code, superseded by `OpenSessionBar`/`ProjectSidebar`). It reads `chatState.sessionId` and dispatches the now-removed `SET_SESSION` action, so it needs a minimal fix purely to keep the build green — not a functional update, since nothing renders it. Per project convention, dead code found incidentally is flagged, not deleted, unless asked.

**Files:**
- Modify: `web/src/components/Layout/SessionSidebar.tsx:146,182-185,195`

- [ ] **Step 1: Swap the session source and drop the `SET_SESSION` dispatch**

Change:

```tsx
  const dispatch = useChatDispatch();
  const { sessionId } = useChatState();
```

to:

```tsx
  const { activeTabId: sessionId } = useProjectState();
```

(Add `useProjectState` to this file's imports; if `useChatDispatch`/`useChatState` become unused after this and the next change, remove those imports too.)

Find the dispatch at line 195 (`dispatch({ type: "SET_SESSION", sessionId: id });`) — inspect its surrounding function to see what it's meant to do (select a session from the sidebar's list). Since this component is unreferenced, replace it with the modern equivalent used by `OpenSessionBar`/`SessionDialog` — `openSessionTab(id, title)` from `useProjectState()` — so the file stays internally consistent with the rest of the app even though nothing currently mounts it:

```tsx
  const { activeTabId: sessionId, openSessionTab } = useProjectState();
```

and at the call site, replace `dispatch({ type: "SET_SESSION", sessionId: id });` with `openSessionTab(id, id);` (check the surrounding code for a title variable in scope — use it if present instead of `id`).

- [ ] **Step 2: Typecheck**

Run: `cd web && npx tsgo --noEmit 2>&1 | grep SessionSidebar.tsx`
Expected: No errors.

- [ ] **Step 3: Commit**

```bash
cd web && git add src/components/Layout/SessionSidebar.tsx
git commit -m "chore(web): fix unused SessionSidebar for the chatStore session-id removal"
```

---

### Task 11: Full verification pass

**Files:** none (verification only)

- [ ] **Step 1: Full typecheck**

Run: `cd web && npx tsgo --noEmit` (or `bun run typecheck`, per the project's configured script)
Expected: Zero errors.

- [ ] **Step 2: Full test suite**

Run: `cd web && npx vitest run`
Expected: All tests pass, including the new `chatStore.test.tsx` and `projectStore.test.tsx` and the updated `AgentPreview.test.tsx`/`AgentsPanel.test.tsx`.

- [ ] **Step 3: Manual — same-project background streaming**

Start the dev server. Open two session tabs in the same project. Send a message in tab A. Before it finishes responding, switch to tab B. Wait for tab A's response to complete in the background (watch for it in a later switch). Switch back to tab A.
Expected: Tab A's full response is present, scroll position is at the bottom (or wherever it was left), and no network request was made to re-fetch tab A's history on switching back (check the browser Network tab for a `GET /api/sessions/<id>` firing on the tab-switch click — there should be none).

- [ ] **Step 4: Manual — cross-project background streaming**

Same as Step 3, but tab B is in a different project. Confirm switching projects and back doesn't lose tab A's in-progress or completed response either.

- [ ] **Step 5: Manual — scroll position retention**

In a tab with enough messages to scroll, scroll up partway, switch to another tab, switch back.
Expected: scroll position is exactly where it was left, not snapped back to the bottom.

- [ ] **Step 6: Manual — background permission/question prompt badge**

Trigger a tool permission prompt (or a question prompt) while a tab is in the background (send a message from a different tab than the one currently active — or start a turn, then switch away before the prompt arrives).
Expected: no dialog interrupts the tab you're currently viewing; the background tab shows an amber dot badge in the tab bar; switching to that tab immediately shows the permission/question dialog (no delay/refetch).

- [ ] **Step 7: Manual — new tab → first message → rename**

Open a new session tab (empty, `new-*` id). Type and send a first message. Immediately switch to a different tab before the response starts streaming, then switch back.
Expected: the tab's id/title updates to the real session once created (visible in the tab bar), and the first turn's content (user message + streamed response) is fully present with nothing dropped — this is the specific race Task 2/5/6's `REKEY_SESSION` handling exists to cover.

- [ ] **Step 8: Manual — closing a tab frees its state**

Open several tabs, send messages in a couple, close one that has an in-progress or completed response.
Expected: no console errors; reopening the same session later via "All sessions" re-fetches cleanly (since its slice was dropped on close).

- [ ] **Step 9: If any manual check fails, return to the relevant task above, fix, re-run that task's own tests, then re-run this full pass from Step 1.**
