# Web/Desktop Session Tab Hierarchy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restructure the web/desktop UI so the top-level nav is
`[Files] [Git] [Cron] [Assets] [Status] [Logs] [Sessions]`, and Chat/Agents/
Changes become sub-tabs nested under each individually-open session tab
(each session remembers its own sub-tab), instead of one global sub-tab
shared across every session.

**Architecture:** `App.tsx`'s `activeTab` state (currently the Radix `Tabs`
value shared by every main tab *and* every file-editor tab) splits into two
independent pieces of state: a top-level `activeView` selector
(`files|git|cron|assets|status|logs|sessions`), and a per-session
`activeSubTab` field living on `projectStore`'s `Tab` (`chat|agents|
changes`). File-editor tabs move under the `'files'` view via a new
`EditorTabBar` component. `AgentsPanel`/`ChangesPanel` get stacked one
instance per open session (mirroring the existing `ChatPanel` pattern) so
each session's sub-tab state and panel content are independent and stay
alive in the background.

**Tech Stack:** React 18, TypeScript, Vite, Vitest + @testing-library/react,
Radix UI (`@radix-ui/react-tabs`), Tailwind CSS.

## Global Constraints

- Use `tsgo` for type checking: `bun run typecheck` (not `tsc`/`typecheck:legacy`).
- Test runner: `vitest run` (`bun run test` from `web/`).
- Match existing code style (no comments beyond what's already there; Tailwind
  utility classes inline; `zinc`-based dark palette).
- Do not touch backend/server code — this is a frontend-only restructure
  (see spec §7, out of scope: backend session-scoping for logs/status).
- Never remove an existing test case unless the behavior it covers genuinely
  changed (several tasks below do this deliberately — call it out in the
  commit).

**Spec:** `docs/superpowers/specs/2026-08-09-web-session-tab-hierarchy-design.md`

---

### Task 1: Per-session sub-tab state in `projectStore`

**Files:**
- Modify: `web/src/stores/projectStore.tsx`
- Modify: `web/src/stores/projectStore.test.tsx`

**Interfaces:**
- Produces: `export type SessionSubTabId = "chat" | "agents" | "changes";`
  and `Tab.activeSubTab: SessionSubTabId`, both consumed by Task 4
  (`SessionSubTabs.tsx`) and Task 5 (`App.tsx` stacking).
- Produces: new dispatchable action
  `{ type: "SET_TAB_SUB_TAB"; id: string; subTab: SessionSubTabId }`.

- [ ] **Step 1: Write the failing test**

Add to `web/src/stores/projectStore.test.tsx` (new `describe` block, same
file, same `setup()` helper already defined there):

```tsx
describe("projectStore session sub-tabs", () => {
  it("ADD_TAB defaults a new tab's activeSubTab to chat", async () => {
    const { result } = setup();
    await act(async () => {
      result.current.dispatch({ type: "SET_ACTIVE_PROJECT", project: testProjectA });
      result.current.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-1", projectPath: "/proj-a", title: "s1", activeSubTab: "chat" },
      });
    });
    await act(async () => {});
    expect(result.current.tabs.find((t) => t.id === "sess-1")?.activeSubTab).toBe("chat");
  });

  it("SET_TAB_SUB_TAB updates a tab that belongs to a non-active project", async () => {
    const { result } = setup();
    await act(async () => {
      result.current.dispatch({ type: "SET_ACTIVE_PROJECT", project: testProjectA });
      result.current.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-b", projectPath: "/proj-b", title: "b", activeSubTab: "chat" },
      });
      result.current.dispatch({
        type: "SET_ACTIVE_PROJECT",
        project: testProjectA,
      });
      result.current.dispatch({ type: "SET_TAB_SUB_TAB", id: "sess-b", subTab: "agents" });
    });
    await act(async () => {});
    expect(result.current.state.tabsByProject["/proj-b"][0].activeSubTab).toBe("agents");
  });

  it("SET_TAB_SUB_TAB is a no-op for an unknown tab id", async () => {
    const { result } = setup();
    await act(async () => {
      result.current.dispatch({ type: "SET_ACTIVE_PROJECT", project: testProjectA });
      result.current.dispatch({ type: "SET_TAB_SUB_TAB", id: "does-not-exist", subTab: "agents" });
    });
    await act(async () => {});
    expect(result.current.state.tabsByProject["/proj-a"]).toBeUndefined();
  });
});
```

Also update the two pre-existing `ADD_TAB` dispatch literals in this file
(`UPDATE_TAB_TITLE`/`UPDATE_TAB_ID` tests) to include `activeSubTab: "chat"`,
since `Tab` is about to require the field:

```tsx
tab: { id: "sess-b", projectPath: "/proj-b", title: "old", activeSubTab: "chat" },
```
```tsx
tab: { id: "new-1", projectPath: "/proj-b", title: "New session", activeSubTab: "chat" },
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && bun run test -- projectStore` (or `npx vitest run src/stores/projectStore.test.tsx`)
Expected: FAIL — `activeSubTab` doesn't exist on `Tab`, `SET_TAB_SUB_TAB` is not a recognized action (TS error and/or runtime no-op assertion failures).

- [ ] **Step 3: Implement**

In `web/src/stores/projectStore.tsx`:

Add the exported type and extend `Tab` (replace lines 5-9):

```tsx
export type SessionSubTabId = "chat" | "agents" | "changes";

export interface Tab {
  id: string; // session ID (or `new-<ts>` temp ID before first message)
  projectPath: string;
  title: string;
  activeSubTab: SessionSubTabId;
}
```

Add the action to `ProjectAction` (after `SET_ACTIVE_TAB`, line 35):

```tsx
  | { type: "SET_ACTIVE_TAB"; id: string | null }
  | { type: "SET_TAB_SUB_TAB"; id: string; subTab: SessionSubTabId }
```

Add the reducer case (after the `SET_ACTIVE_TAB` case, line 117-118):

```tsx
    case "SET_TAB_SUB_TAB": {
      const ownerPath = findProjectPathForTab(state, action.id);
      if (!ownerPath) return state;
      const list = state.tabsByProject[ownerPath].map((t) =>
        t.id === action.id ? { ...t, activeSubTab: action.subTab } : t
      );
      return { ...state, tabsByProject: { ...state.tabsByProject, [ownerPath]: list } };
    }
```

Set `activeSubTab: "chat"` at every place a `Tab` is constructed:

`openSessionTab` (line 314-322):

```tsx
  const openSessionTab = useCallback((sessionId: string, sessionTitle: string) => {
    const path = state.activeProject?.path || "";
    const tab: Tab = {
      id: sessionId,
      projectPath: path,
      title: sessionTitle || sessionId,
      activeSubTab: "chat",
    };
    dispatch({ type: "ADD_TAB", tab });
  }, [state.activeProject]);
```

`ENSURE_NEW_TAB` case (line 163-167):

```tsx
      const newTab: Tab = {
        id: `new-${Date.now()}`,
        projectPath: action.path,
        title: "New session",
        activeSubTab: "chat",
      };
```

`openNewSessionTab` (line 383-384):

```tsx
    const tempId = `new-${Date.now()}`;
    dispatch({ type: "ADD_TAB", tab: { id: tempId, projectPath: path, title: "New session", activeSubTab: "chat" } });
```

Persist `activeSubTab` (round-trips through `localStorage`, no version bump
— the persisted shape just gains an optional field). Update `PersistedTabs`
(line 188-191):

```tsx
interface PersistedTabs {
  version: 1;
  projects: Record<string, { tabs: { id: string; title: string; subTab?: SessionSubTabId }[]; active: string | null }>;
}
```

Update `loadPersistedTabs`'s tab mapping (line 205-207):

```tsx
      const tabs = entry.tabs
        .filter((t) => t && typeof t.id === "string" && !t.id.startsWith("new-"))
        .map((t) => ({
          id: t.id,
          projectPath: path,
          title: typeof t.title === "string" ? t.title : t.id,
          activeSubTab: (t.subTab === "agents" || t.subTab === "changes" ? t.subTab : "chat") as SessionSubTabId,
        }));
```

Update `persistTabs`'s serialization (line 226):

```tsx
      tabs: real.map((t) => ({ id: t.id, title: t.title, subTab: t.activeSubTab })),
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && bun run test -- projectStore`
Expected: PASS (all `projectStore.test.tsx` tests, including the 3 new ones).

- [ ] **Step 5: Type check**

Run: `cd web && bun run typecheck`
Expected: no errors. (This will surface any other `Tab` literal construction
site missed above — fix any that appear before moving on.)

- [ ] **Step 6: Commit**

```bash
git add web/src/stores/projectStore.tsx web/src/stores/projectStore.test.tsx
git commit -m "feat(web): add per-session activeSubTab state to projectStore"
```

---

### Task 2: Decouple `AgentsPanel` from the global active session

**Files:**
- Modify: `web/src/components/Agents/AgentsPanel.tsx`
- Modify: `web/src/components/Agents/AgentsPanel.test.tsx`

**Interfaces:**
- Consumes: nothing new from Task 1.
- Produces: `AgentsPanel` now takes `sessionId: string | null` as an
  explicit prop instead of reading `useProjectState().activeTabId`
  internally — required so Task 5 can mount one `AgentsPanel` per open
  session tab (all resolving `useProjectState().activeTabId` internally
  would make every stacked instance show the *same* — currently active —
  session, defeating the point of stacking).

- [ ] **Step 1: Write the failing test**

In `web/src/components/Agents/AgentsPanel.test.tsx`, remove the
`vi.mock("../../stores/projectStore", ...)` block (lines 34-36) — the panel
no longer reads that store — and pass `sessionId="session-1"` to every
`render(<AgentsPanel .../>)` call (8 call sites). Example for the first:

```tsx
describe("AgentsPanel", () => {
  it("shows the empty state when there are no runs", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [], loaded: true });
    render(<AgentsPanel sessionId="session-1" selectedRunId={null} onSelectRun={vi.fn()} />);

    expect(screen.getByText("No agent runs yet in this session.")).toBeInTheDocument();
  });
```

Apply the same `sessionId="session-1"` addition to the other 7
`render(<AgentsPanel ... />)` calls in the file, leaving every other prop
and assertion unchanged.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun run test -- AgentsPanel`
Expected: FAIL — TS error (`sessionId` is not a valid prop on the current
`AgentsPanelProps`) and/or the mocked `useProjectState` no longer being
called, since it doesn't exist as an import target after the mock removal
until the component itself is updated.

- [ ] **Step 3: Implement**

In `web/src/components/Agents/AgentsPanel.tsx`, drop the `useProjectState`
import (line 3) and change the component:

```tsx
interface AgentsPanelProps {
  sessionId: string | null;
  selectedRunId: string | null;
  onSelectRun: (runId: string | null) => void;
}
```

```tsx
export default function AgentsPanel({ sessionId, selectedRunId, onSelectRun }: AgentsPanelProps) {
  const { runs, loaded } = useAgentRuns(sessionId);
  const selected = selectedRunId ? findRun(runs, selectedRunId) : undefined;
```

(Remove the old comment above the deleted `useProjectState()` line — it no
longer applies once the session comes from a prop.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && bun run test -- AgentsPanel`
Expected: PASS (all 8 tests).

- [ ] **Step 5: Type check**

Run: `cd web && bun run typecheck`
Expected: no errors from this file (the only other consumer, `App.tsx`, is
updated in Task 5 — a transient type error there until Task 5 lands is
expected and fine).

- [ ] **Step 6: Commit**

```bash
git add web/src/components/Agents/AgentsPanel.tsx web/src/components/Agents/AgentsPanel.test.tsx
git commit -m "refactor(web): AgentsPanel takes sessionId as an explicit prop"
```

---

### Task 3: Split editor tabs out of `useEditorTabs`/`TopTabs` into `EditorTabBar`

**Files:**
- Modify: `web/src/hooks/useEditorTabs.ts`
- Create: `web/src/hooks/useEditorTabs.test.ts`
- Modify: `web/src/components/Layout/TopTabs.tsx`
- Create: `web/src/components/Layout/EditorTabBar.tsx`

**Interfaces:**
- Produces: `useEditorTabs()` (no `initialTab` param) returns
  `activeEditorTabId: string | null` and `setActiveEditorTabId: (id: string | null) => void`
  in place of the old `activeTab: string` / `setActiveTab: (tab: string) => void`.
  `null` means "show the file tree", any other value is an editor tab id.
- Produces: `EditorTabBar` component —
  `{ editorTabs: EditorTabInfo[]; activeEditorTabId: string | null; onSelectTree: () => void; onSelectTab: (id: string) => void; onCloseTab: (id: string) => void }`
  — consumed by Task 5. `EditorTabInfo` (id/path/isDirty) moves here from
  `TopTabs.tsx`.
- Produces: `TopTabs` props shrink to
  `{ activeTab: string; onTabSelect: (value: string) => void }` (drops
  `editorTabs`/`onEditorTabClose`) — consumed by Task 5, which passes
  `activeView`/`setActiveView`.

- [ ] **Step 1: Write the failing test**

Create `web/src/hooks/useEditorTabs.test.ts`:

```ts
import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { useEditorTabs } from "./useEditorTabs";

vi.mock("../api/client", () => ({
  api: { saveFileContent: vi.fn() },
  apiPath: (p: string) => p,
  authHeaders: () => ({}),
}));

const originalFetch = global.fetch;

beforeEach(() => {
  global.fetch = vi.fn().mockResolvedValue({
    ok: true,
    json: async () => ({ content: "hello" }),
  }) as unknown as typeof fetch;
});

afterEach(() => {
  global.fetch = originalFetch;
});

describe("useEditorTabs", () => {
  it("starts with no active editor tab (file tree shown)", () => {
    const { result } = renderHook(() => useEditorTabs());
    expect(result.current.activeEditorTabId).toBeNull();
  });

  it("opening a file sets it as the active editor tab", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("/a/b.txt");
    });
    await waitFor(() => expect(result.current.activeEditorTabId).toBe("editor-/a/b.txt"));
    expect(result.current.editorTabs).toHaveLength(1);
  });

  it("closing the active tab falls back to null, not a string sentinel", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("/a/b.txt");
    });
    await waitFor(() => expect(result.current.activeEditorTabId).toBe("editor-/a/b.txt"));
    act(() => {
      result.current.requestCloseTab("editor-/a/b.txt");
    });
    expect(result.current.activeEditorTabId).toBeNull();
    expect(result.current.editorTabs).toHaveLength(0);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun run test -- useEditorTabs`
Expected: FAIL — `activeEditorTabId` is `undefined` (the hook still exposes
`activeTab`/`setActiveTab`).

- [ ] **Step 3: Implement `useEditorTabs.ts`**

In `web/src/hooks/useEditorTabs.ts`:

Replace the `UseEditorTabsResult` interface's tab-selector fields (lines
19-21):

```ts
export interface UseEditorTabsResult {
  editorTabs: EditorTab[];
  activeEditorTabId: string | null;
  setActiveEditorTabId: (id: string | null) => void;
```

Change the hook signature and state (line 35-37) — drop the `initialTab`
parameter entirely, since there's no longer a shared "default view" concept
for this hook to seed:

```ts
export function useEditorTabs(): UseEditorTabsResult {
  const [editorTabs, setEditorTabs] = useState<EditorTab[]>([]);
  const [activeEditorTabId, setActiveEditorTabId] = useState<string | null>(null);
```

Update `handleOpenFile` (lines 43-64) — replace both `setActiveTab(id)`
calls with `setActiveEditorTabId(id)`:

```ts
  const handleOpenFile = useCallback(async (path: string) => {
    const id = `editor-${path}`;
    if (openFileIdsRef.current.has(id)) {
      setActiveEditorTabId(id);
      return;
    }
    try {
      const res = await fetch(apiPath(`/api/files/content?path=${encodeURIComponent(path)}`), {
        headers: authHeaders(),
      });
      if (!res.ok) throw new Error("Failed to load file");
      const data = await res.json();
      openFileIdsRef.current.add(id);
      setEditorTabs((prev) => [
        ...prev,
        { id, path, content: data.content, originalContent: data.content, isDirty: false, diffVersion: 0 },
      ]);
      setActiveEditorTabId(id);
    } catch (err) {
      console.error("Failed to open file:", err);
    }
  }, []);
```

Update `handleSelectionChange` (lines 72-89) — drop the `.startsWith("editor-")`
check, since every non-null `activeEditorTabId` now *is* an editor tab:

```ts
  const handleSelectionChange = useCallback(
    (sel: { startLine: number; endLine: number } | null) => {
      setActiveEditorContext((prev) => {
        if (prev) {
          return { ...prev, selection: sel ?? undefined };
        }

        if (!activeEditorTabId) return null;
        const tab = editorTabs.find((t) => t.id === activeEditorTabId);
        if (!tab) return null;
        return {
          path: tab.path,
          selection: sel ?? undefined,
        };
      });
    },
    [activeEditorTabId, editorTabs],
  );
```

Update the effect right below it (lines 91-112):

```ts
  useEffect(() => {
    if (editorTabs.length === 0) {
      setActiveEditorContext(null);
      return;
    }

    if (!activeEditorTabId) {
      setActiveEditorContext(null);
      return;
    }

    const tab = editorTabs.find((t) => t.id === activeEditorTabId);
    if (!tab) {
      setActiveEditorContext(null);
      return;
    }

    setActiveEditorContext((prev) => {
      if (prev?.path === tab.path) return prev;
      return { path: tab.path };
    });
  }, [activeEditorTabId, editorTabs]);
```

Update `closeTabNow` (lines 114-122) — fall back to `null` (file tree)
instead of the string `"files"`:

```ts
  const closeTabNow = useCallback((id: string) => {
    openFileIdsRef.current.delete(id);
    setEditorTabs((prev) => prev.filter((t) => t.id !== id));
    setActiveEditorTabId((prev) => {
      if (prev !== id) return prev;
      const remaining = editorTabs.filter((t) => t.id !== id);
      return remaining[0]?.id ?? null;
    });
  }, [editorTabs]);
```

Update the final `return` (lines 183-198):

```ts
  return {
    editorTabs,
    activeEditorTabId,
    setActiveEditorTabId,
    handleOpenFile,
    handleEditorChange,
    handleSelectionChange,
    activeEditorContext,
    requestCloseTab,
    saveEditorTab,
    pendingClose,
    confirmSaveAndClose,
    confirmDiscardAndClose,
    cancelClose,
    saveError,
  };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && bun run test -- useEditorTabs`
Expected: PASS (all 3 new tests).

- [ ] **Step 5: Create `EditorTabBar.tsx`**

Create `web/src/components/Layout/EditorTabBar.tsx` (lifts the editor-tab
rendering block currently at `TopTabs.tsx:113-156`, adds a "file tree"
entry, and switches from mutating a shared `activeTab` string to explicit
callback props):

```tsx
import { FileCode, Files as FilesIcon, X } from "lucide-react";

export interface EditorTabInfo {
  id: string;
  path: string;
  isDirty?: boolean;
}

interface Props {
  editorTabs: EditorTabInfo[];
  activeEditorTabId: string | null;
  onSelectTree: () => void;
  onSelectTab: (id: string) => void;
  onCloseTab: (id: string) => void;
}

function fileNameFromPath(path: string): string {
  return path.split("/").pop() || path;
}

export default function EditorTabBar({
  editorTabs,
  activeEditorTabId,
  onSelectTree,
  onSelectTab,
  onCloseTab,
}: Props) {
  if (editorTabs.length === 0) return null;

  return (
    <div className="flex items-center h-8 px-2 gap-1 bg-zinc-900 border-b border-zinc-700 overflow-x-auto scrollbar-hide">
      <button
        onClick={onSelectTree}
        className={`flex items-center gap-1.5 px-2 py-1 rounded text-xs font-medium transition-colors whitespace-nowrap shrink-0 ${
          activeEditorTabId === null
            ? "bg-zinc-700 text-white"
            : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800"
        }`}
      >
        <FilesIcon className="w-3.5 h-3.5" />
        File tree
      </button>
      {editorTabs.map((et) => {
        const isActive = activeEditorTabId === et.id;
        return (
          <div
            key={et.id}
            className="flex items-center gap-1 shrink-0"
            onMouseDown={(e) => {
              if (e.button === 1) {
                e.preventDefault();
                e.stopPropagation();
                onCloseTab(et.id);
              }
            }}
          >
            <button
              onClick={() => onSelectTab(et.id)}
              className={`flex items-center gap-1.5 px-2 py-1 rounded-md text-xs font-medium transition-colors whitespace-nowrap shrink-0 ${
                isActive
                  ? "bg-blue-600/20 text-blue-400"
                  : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800"
              }`}
              title={et.path}
            >
              <FileCode className="w-3.5 h-3.5" />
              <span className="max-w-[120px] truncate">{fileNameFromPath(et.path)}</span>
              {et.isDirty && (
                <span className="w-1.5 h-1.5 rounded-full bg-zinc-300 shrink-0" title="Unsaved changes" />
              )}
            </button>
            <button
              onClick={(e) => {
                e.stopPropagation();
                onCloseTab(et.id);
              }}
              className="p-0.5 rounded hover:bg-zinc-700 text-zinc-500 hover:text-zinc-300 transition-colors"
              title="Close"
            >
              <X className="w-3 h-3" />
            </button>
          </div>
        );
      })}
    </div>
  );
}
```

- [ ] **Step 6: Trim `TopTabs.tsx`**

Replace `web/src/components/Layout/TopTabs.tsx` in full with:

```tsx
import { useLayoutEffect, useRef, useState } from "react";
import { FolderGit2, GitBranch, ScrollText, Paperclip, Activity, CalendarClock, MessageSquare, MoreHorizontal } from "lucide-react";
import { TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select";
import SyncStatusWidget from "./SyncStatusWidget";

interface Props {
  activeTab: string;
  onTabSelect: (value: string) => void;
}

const mainTabs = [
  { id: "files", label: "Files", icon: FolderGit2 },
  { id: "git", label: "Git", icon: GitBranch },
  { id: "cron", label: "Cron", icon: CalendarClock },
  { id: "assets", label: "Assets", icon: Paperclip },
  { id: "status", label: "Status", icon: Activity },
  { id: "logs", label: "Logs", icon: ScrollText },
  { id: "sessions", label: "Sessions", icon: MessageSquare },
];

export default function TopTabs({ activeTab, onTabSelect }: Props) {
  const tabsRef = useRef<HTMLDivElement | null>(null);
  const activeRef = useRef<HTMLButtonElement | null>(null);
  const [overflowing, setOverflowing] = useState(false);

  // Detect whether the tab strip overflows its container so the "More" menu
  // can be shown. Re-measured on resize.
  useLayoutEffect(() => {
    const el = tabsRef.current;
    if (!el) return;
    const measure = () => setOverflowing(el.scrollWidth > el.clientWidth + 1);
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    window.addEventListener("resize", measure);
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, []);

  // Keep the active tab in view (Chrome-style) when it changes.
  useLayoutEffect(() => {
    const el = tabsRef.current;
    const tab = activeRef.current;
    if (!el || !tab) return;
    const left = tab.getBoundingClientRect().left - el.getBoundingClientRect().left;
    const right = left + tab.getBoundingClientRect().width;
    if (left < el.scrollLeft) {
      el.scrollTo({ left });
    } else if (right > el.scrollLeft + el.clientWidth) {
      el.scrollTo({ left: right - el.clientWidth });
    }
  }, [activeTab]);

  // Translate vertical mouse-wheel input into horizontal scrolling on the
  // strip. Trackpad horizontal gestures scroll natively.
  const handleWheel = (e: React.WheelEvent<HTMLDivElement>) => {
    const el = tabsRef.current;
    if (!el || el.scrollWidth <= el.clientWidth + 1) return;
    const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY;
    el.scrollLeft += delta;
  };

  return (
    <header className="flex items-center border-b border-zinc-700 bg-zinc-900 h-12 px-4 overflow-hidden">
      {/* Left: Logo */}
      <div className="flex items-center gap-2 mr-6 shrink-0">
        <div className="w-6 h-6 rounded bg-blue-600 flex items-center justify-center text-xs font-bold">
          o
        </div>
        <span className="font-semibold text-sm hidden sm:inline">ocode</span>
      </div>

      {/* Main tabs — single row, horizontally scrollable */}
      <TabsList
        ref={tabsRef}
        onWheel={handleWheel}
        className="bg-transparent p-0 h-auto gap-1 justify-start flex-1 min-w-0 overflow-x-auto scrollbar-hide flex-nowrap"
      >
        {mainTabs.map((tab) => {
          const Icon = tab.icon;
          const isActive = activeTab === tab.id;
          return (
            <TabsTrigger
              key={tab.id}
              value={tab.id}
              ref={isActive ? activeRef : undefined}
              className="flex items-center gap-2 px-3 py-1.5 rounded-md text-sm font-medium transition-colors whitespace-nowrap data-[state=active]:bg-zinc-700 data-[state=active]:text-white data-[state=active]:shadow-none text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 shrink-0"
            >
              <Icon className="w-4 h-4" />
              <span className="hidden sm:inline">{tab.label}</span>
            </TabsTrigger>
          );
        })}
      </TabsList>

      {/* Overflow "More" menu — appears only when the tabs don't fit */}
      {overflowing && (
        <div className="ml-1 shrink-0">
          <Select value={activeTab} onValueChange={onTabSelect}>
            <SelectTrigger
              aria-label="More tabs"
              className="h-8 w-8 justify-center border-0 bg-transparent p-0 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200 [&>svg:last-child]:hidden"
            >
              <MoreHorizontal className="h-4 w-4" />
            </SelectTrigger>
            <SelectContent align="end" className="max-h-80">
              {mainTabs.map((tab) => {
                const Icon = tab.icon;
                return (
                  <SelectItem key={tab.id} value={tab.id}>
                    <span className="flex items-center gap-2">
                      <Icon className="w-3.5 h-3.5" />
                      {tab.label}
                    </span>
                  </SelectItem>
                );
              })}
            </SelectContent>
          </Select>
        </div>
      )}

      <div className="ml-auto flex items-center shrink-0">
        <SyncStatusWidget />
      </div>
    </header>
  );
}
```

- [ ] **Step 7: Type check**

Run: `cd web && bun run typecheck`
Expected: errors only in `App.tsx` (still calling `useEditorTabs("chat")`
and referencing the old `TopTabs`/`activeTab` shape) — fixed in Task 5.
Confirm no errors in `useEditorTabs.ts`, `EditorTabBar.tsx`, or `TopTabs.tsx`
themselves.

- [ ] **Step 8: Commit**

```bash
git add web/src/hooks/useEditorTabs.ts web/src/hooks/useEditorTabs.test.ts \
        web/src/components/Layout/TopTabs.tsx web/src/components/Layout/EditorTabBar.tsx
git commit -m "refactor(web): split file-editor tabs out of TopTabs into EditorTabBar"
```

---

### Task 4: `SessionSubTabs` component

**Files:**
- Create: `web/src/components/Layout/SessionSubTabs.tsx`

**Interfaces:**
- Consumes: `useProjectState()` (`tabs`, `activeTabId`, `dispatch`) from
  `projectStore.tsx` (Task 1's `SET_TAB_SUB_TAB` action, `Tab.activeSubTab`).
- Produces: renders nothing (`null`) when there's no active session tab;
  otherwise a 3-button strip (Chat/Agents/Changes) that dispatches
  `SET_TAB_SUB_TAB` for the active session tab. No props — reads everything
  from `useProjectState()`, mirroring `OpenSessionBar`'s pattern.

- [ ] **Step 1: Implement**

No dedicated test — this is a thin presentational wrapper over
already-tested reducer state (Task 1) and is exercised via the App-level
manual verification in Task 5. Create
`web/src/components/Layout/SessionSubTabs.tsx`:

```tsx
import { MessageSquare, Bot, History } from "lucide-react";
import { useProjectState, type SessionSubTabId } from "../../stores/projectStore";

const subTabs: { id: SessionSubTabId; label: string; icon: typeof MessageSquare }[] = [
  { id: "chat", label: "Chat", icon: MessageSquare },
  { id: "agents", label: "Agents", icon: Bot },
  { id: "changes", label: "Changes", icon: History },
];

export default function SessionSubTabs() {
  const { tabs, activeTabId, dispatch } = useProjectState();
  const activeSessionTab = tabs.find((t) => t.id === activeTabId);

  if (!activeSessionTab) return null;

  return (
    <div className="flex items-center h-9 px-2 gap-1 bg-zinc-900 border-b border-zinc-700">
      {subTabs.map((tab) => {
        const Icon = tab.icon;
        const isActive = activeSessionTab.activeSubTab === tab.id;
        return (
          <button
            key={tab.id}
            onClick={() => dispatch({ type: "SET_TAB_SUB_TAB", id: activeSessionTab.id, subTab: tab.id })}
            className={`flex items-center gap-2 px-3 py-1.5 rounded-md text-sm font-medium transition-colors whitespace-nowrap ${
              isActive
                ? "bg-zinc-700 text-white"
                : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800"
            }`}
          >
            <Icon className="w-4 h-4" />
            {tab.label}
          </button>
        );
      })}
    </div>
  );
}
```

- [ ] **Step 2: Type check**

Run: `cd web && bun run typecheck`
Expected: no new errors from this file.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Layout/SessionSubTabs.tsx
git commit -m "feat(web): add SessionSubTabs component for per-session Chat/Agents/Changes"
```

---

### Task 5: Wire it all together in `App.tsx`; delete dead `SessionSidebar`

**Files:**
- Modify: `web/src/App.tsx`
- Delete: `web/src/components/Layout/SessionSidebar.tsx` (unused since
  commit `b25ab73` — confirmed dead via repo-wide grep during the design
  phase)

**Interfaces:**
- Consumes: `SessionSubTabId`/`SET_TAB_SUB_TAB` (Task 1), `AgentsPanel`'s
  new `sessionId` prop (Task 2), `useEditorTabs()`'s `activeEditorTabId`/
  `setActiveEditorTabId` + trimmed `TopTabs` props + `EditorTabBar` (Task
  3), `SessionSubTabs` (Task 4).
- Produces: nothing further consumes `App.tsx`.

No dedicated test file exists for `App.tsx` today (confirmed: no
`App.test.tsx` in the repo) — this task is verified via the full existing
test suite (nothing here should break other components' tests) plus manual
browser verification (Step 3), consistent with how the rest of the app's
top-level wiring is validated.

- [ ] **Step 1: Update imports and hook usage**

In `web/src/App.tsx`:

Replace the `EditorTabInfo` type dependency — `TopTabs` no longer exports
it. Add the `EditorTabBar` and `SessionSubTabs` imports, and drop the now
unused `Tabs`/`TabsContent` import if no longer needed (it's still needed —
keep it). Update the import block (lines 22-29):

```tsx
import { Tabs, TabsContent } from "@/components/ui/tabs";
import TopTabs from "./components/Layout/TopTabs";
import EditorTabBar from "./components/Layout/EditorTabBar";
import ProjectSidebar from "./components/Layout/ProjectSidebar";
import SessionDialog from "./components/Layout/SessionDialog";
import OpenSessionBar from "./components/Layout/OpenSessionBar";
import SessionSubTabs from "./components/Layout/SessionSubTabs";
import SessionTabSync from "./components/Layout/SessionTabSync";
import CoworkSidebar from "./components/Layout/CoworkSidebar";
import ModelDialog from "./components/Layout/ModelDialog";
```

Replace the `useEditorTabs` destructure (lines 103-118) and add
`activeView` state:

```tsx
  const [activeView, setActiveView] = useState<
    "files" | "git" | "cron" | "assets" | "status" | "logs" | "sessions"
  >("sessions");
  const {
    editorTabs,
    activeEditorTabId,
    setActiveEditorTabId,
    handleOpenFile,
    handleEditorChange,
    handleSelectionChange,
    activeEditorContext,
    requestCloseTab,
    pendingClose,
    confirmSaveAndClose,
    confirmDiscardAndClose,
    cancelClose,
    saveError,
    saveEditorTab,
  } = useEditorTabs();
```

Add a wrapper that opens a file *and* switches to the Files view (used by
every `onOpenFile` caller below), placed right after the destructure above:

```tsx
  const openFileAndShow = useCallback(
    async (path: string) => {
      await handleOpenFile(path);
      setActiveView("files");
    },
    [handleOpenFile],
  );
```

This needs `useCallback` imported — add it to the React import at the top
of the file (line 1):

```tsx
import { useCallback, useEffect, useRef, useState } from "react";
```

- [ ] **Step 2: Update `openAgentDetail`, `useKeyboard`, and derive the active session tab**

Replace `openAgentDetail` (lines 124-127) — it now needs to flip the
*active session's* sub-tab, not the old global tab:

```tsx
  const openAgentDetail = (runId: string) => {
    setSelectedAgentRunId(runId);
    if (activeTabId) {
      projectDispatch({ type: "SET_TAB_SUB_TAB", id: activeTabId, subTab: "agents" });
    }
  };
```

Update the `useKeyboard` `onSave` handler (lines 190-195) — no more
`.startsWith("editor-")` check, since `activeEditorTabId` is either `null`
or a real editor tab id:

```tsx
    onSave: () => {
      if (activeEditorTabId) {
        saveEditorTab(activeEditorTabId);
      }
    },
```

Add a derived active-session-tab lookup right after `allChatTabs` (line
281), used by the CoworkSidebar condition in Step 4:

```tsx
  const allChatTabs = Object.values(projectState.tabsByProject).flat();
  const activeSessionTab = projectState.tabs.find((t) => t.id === activeTabId);
```

(`projectState.tabs` — the provider's derived "active project's tabs" list,
already returned by `useProjectState()`, `projectStore.tsx:441`.)

- [ ] **Step 3: Replace the main content JSX**

Replace the whole block from `<Tabs value={activeTab} ...>` through its
closing `</Tabs>` (lines 298-382) with:

```tsx
          <Tabs value={activeView} onValueChange={(v) => setActiveView(v as typeof activeView)} className="flex flex-col flex-1 overflow-hidden">
            <TopTabs activeTab={activeView} onTabSelect={(v) => setActiveView(v as typeof activeView)} />

            <div className="flex-1 overflow-hidden flex flex-col">
              <TabsContent value="files" forceMount className="flex-1 overflow-hidden m-0 flex flex-col">
                <EditorTabBar
                  editorTabs={editorTabs.map((t) => ({ id: t.id, path: t.path, isDirty: t.isDirty }))}
                  activeEditorTabId={activeEditorTabId}
                  onSelectTree={() => setActiveEditorTabId(null)}
                  onSelectTab={setActiveEditorTabId}
                  onCloseTab={requestCloseTab}
                />
                <div className="relative flex-1 overflow-hidden">
                  <div className={activeEditorTabId === null ? "absolute inset-0" : "absolute inset-0 hidden"}>
                    <FileTree onOpenFile={openFileAndShow} />
                  </div>
                  {editorTabs.map((et) => (
                    <div
                      key={et.id}
                      className={et.id === activeEditorTabId ? "absolute inset-0" : "absolute inset-0 hidden"}
                    >
                      <FileEditor
                        path={et.path}
                        content={et.content}
                        onChange={(value) => handleEditorChange(et.id, value)}
                        readOnly={false}
                        session={activeTabId ?? undefined}
                        diffVersion={et.diffVersion}
                        onSelectionChange={handleSelectionChange}
                      />
                    </div>
                  ))}
                </div>
              </TabsContent>

              <TabsContent value="git" forceMount className="flex-1 overflow-hidden m-0">
                <GitPanel onOpenFile={openFileAndShow} />
              </TabsContent>
              <TabsContent value="cron" forceMount className="flex-1 overflow-hidden m-0">
                <CronPanel />
              </TabsContent>
              <TabsContent value="assets" forceMount className="flex-1 overflow-hidden m-0">
                <AssetsPanel />
              </TabsContent>
              <TabsContent value="status" forceMount className="flex-1 overflow-hidden m-0">
                <StatusPanel onClose={() => setActiveView("sessions")} />
              </TabsContent>
              <TabsContent value="logs" forceMount className="flex-1 overflow-hidden m-0">
                <LogPanel active={activeView === "logs"} />
              </TabsContent>

              <TabsContent value="sessions" forceMount className="flex-1 overflow-hidden m-0">
                <div className="flex flex-col h-full">
                  <OpenSessionBar />
                  <SessionSubTabs />
                  <div className="relative flex-1 min-h-0 overflow-hidden">
                    {allChatTabs.map((tab) => (
                      <div
                        key={`${tab.id}:chat`}
                        className={
                          tab.projectPath === projectState.activeProject?.path &&
                          tab.id === activeTabId &&
                          tab.activeSubTab === "chat"
                            ? "absolute inset-0 flex flex-col"
                            : "absolute inset-0 hidden"
                        }
                      >
                        <div className="relative flex-1 min-h-0 overflow-hidden">
                          <ChatPanel sessionId={tab.id} />
                        </div>
                        <AgentPreview onOpenDetail={openAgentDetail} />
                        <ChatInput
                          onSlashCommand={handleCommand}
                          activeEditorContext={activeEditorContext}
                          sessionTabId={tab.id}
                          onSessionCreated={handleSessionCreated}
                        />
                      </div>
                    ))}
                    {allChatTabs.map((tab) => (
                      <div
                        key={`${tab.id}:agents`}
                        className={
                          tab.projectPath === projectState.activeProject?.path &&
                          tab.id === activeTabId &&
                          tab.activeSubTab === "agents"
                            ? "absolute inset-0"
                            : "absolute inset-0 hidden"
                        }
                      >
                        <AgentsPanel
                          sessionId={tab.id}
                          selectedRunId={selectedAgentRunId}
                          onSelectRun={setSelectedAgentRunId}
                        />
                      </div>
                    ))}
                    {allChatTabs.map((tab) => (
                      <div
                        key={`${tab.id}:changes`}
                        className={
                          tab.projectPath === projectState.activeProject?.path &&
                          tab.id === activeTabId &&
                          tab.activeSubTab === "changes"
                            ? "absolute inset-0"
                            : "absolute inset-0 hidden"
                        }
                      >
                        <ChangesPanel session={tab.id} />
                      </div>
                    ))}
                  </div>
                </div>
              </TabsContent>
            </div>
          </Tabs>
```

Note `GitPanel`'s `onOpenFile={openFileAndShow}` (was `handleOpenFile`) —
opening a file from the Git diff view must also switch the top-level view to
Files, same as opening one from the File tree.

- [ ] **Step 4: Update `StatusBar` and `CoworkSidebar` wiring**

Update the `StatusBar` `onStatusClick` prop (lines 385-391):

```tsx
          <StatusBar
            onCoworkToggle={() => setCoworkOpen(!coworkOpen)}
            onModelClick={() => openModelDialog("main")}
            onStatusClick={() => setActiveView("status")}
          />
```

Update the `CoworkSidebar` mount condition (lines 394-403) — it was
`activeTab === "chat"`; it becomes "on the Sessions view, and the active
session's sub-tab is Chat":

```tsx
        {activeView === "sessions" && activeSessionTab?.activeSubTab === "chat" && (
          <CoworkSidebar
            isOpen={coworkOpen}
            onClose={() => setCoworkOpen(false)}
            activeAgent="build"
            onModelClick={openModelDialog}
            isMobile={isMobile}
          />
        )}
```

- [ ] **Step 5: Update the `FilePicker` open-file callback**

`FilePicker`'s `onOpenFile` prop (near the end of the file, `onOpenFile={handleOpenFile}`)
must also use the wrapper so opening a file from the command-palette file
picker switches to the Files view:

```tsx
      <FilePicker
        open={filePickerOpen}
        onClose={() => setFilePickerOpen(false)}
        onOpenFile={openFileAndShow}
      />
```

- [ ] **Step 6: Delete dead code**

```bash
git rm web/src/components/Layout/SessionSidebar.tsx
```

Confirm no remaining references:

Run: `cd web && grep -rn "SessionSidebar" src`
Expected: no output.

- [ ] **Step 7: Type check**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [ ] **Step 8: Run the full test suite**

Run: `cd web && bun run test`
Expected: PASS — every test file (including the ones touched in Tasks 1-2)
passes, and nothing else regresses (e.g. `chatStore.test.tsx`,
`AgentPreview.test.tsx`, `LogPanel.test.tsx`, `tabDrafts.test.ts`,
`smoke.test.tsx` are all untouched by this refactor and must still be
green).

- [ ] **Step 9: Manual browser verification**

Run: `cd web && bun run dev` (and separately start the ocode backend server
per the project's normal dev workflow, if not already running).

Walk through, in the browser:
1. App loads on the Sessions view by default, showing the open session tabs
   and Chat/Agents/Changes sub-tabs for the active session.
2. Open two session tabs (A and B). On A, click the Agents sub-tab; on B,
   stay on Chat. Switch back and forth — confirm A stays on Agents and B
   stays on Chat (per-session sub-tab memory).
3. Start an agent run on session A while viewing session B's Chat — switch
   to A's Agents sub-tab and confirm the run is visible (background panel
   keep-alive).
4. Click Files in the top row, open a file from the tree — confirm the view
   switches to Files and the editor tab appears in `EditorTabBar`. Click
   "File tree" to go back; click the editor tab to return to it.
5. Click Git, Cron, Assets, Status, Logs in the top row — confirm each
   renders its existing content unchanged.
6. Reload the page — confirm the previously-open session tabs, their
   sub-tab selections, and editor tabs restore as expected (per
   `ocode.ui.tabs.v1` persistence).
7. Confirm the Cowork sidebar only appears on the Sessions view while the
   active session's sub-tab is Chat.

If any step fails, fix it before proceeding — do not mark this task
complete on a red manual walkthrough.

- [ ] **Step 10: Commit**

```bash
git add -A web/src/App.tsx
git rm web/src/components/Layout/SessionSidebar.tsx 2>/dev/null || true
git commit -m "feat(web): wire session-tab hierarchy into App.tsx, drop dead SessionSidebar"
```

---

## Self-review notes

- **Spec coverage:** §2 decisions table — sub-tab scope (Task 1/4/5), keep-alive
  (Task 5 stacking), tab scope split incl. the Status/Logs correction (Task
  3/5), layout (Task 3/5), dead code (Task 5), editor tabs (Task 3/5). §4
  state model (Task 1/3). §5 component changes — TopTabs/EditorTabBar (Task
  3), SessionSubTabs (Task 4), AgentsPanel/ChangesPanel stacking (Task 5),
  StatusPanel/LogPanel staying single-instance (Task 5), CoworkSidebar (Task
  5), StatusBar (Task 5), SessionSidebar deletion (Task 5). All covered.
- **Type consistency:** `SessionSubTabId` (Task 1) is the single source of
  truth used by `Tab.activeSubTab`, `SET_TAB_SUB_TAB`, and `SessionSubTabs`'
  `subTabs` array (Task 4) — no duplicate/divergent type definitions.
  `activeEditorTabId`/`setActiveEditorTabId` names are consistent between
  `useEditorTabs.ts` (Task 3) and `App.tsx` (Task 5). `EditorTabInfo` is
  defined once, in `EditorTabBar.tsx` (Task 3), and only ever constructed
  inline via `.map()` in `App.tsx` (Task 5) — no other file imports it.
