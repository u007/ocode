# Unified Sessions + Terminal Tab Bar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge the "Sessions" and "Terminal" top-level tabs in the web/desktop UI into one tab bar that mixes chat-session and terminal pills (with 💬/⌨️ emoji prefixes), is drag-reorderable, and has two separate `+` buttons (new chat, new terminal) instead of one.

**Architecture:** Lift terminal tab state (today private to `TerminalTabs.tsx`) into a new `terminalStore` module shaped like the existing `projectStore`, so a new `UnifiedTabBar` component can read both chat tabs (`projectStore`) and terminal tabs (`terminalStore`) and render them as one merged, orderable row. `TerminalTabs.tsx` shrinks to content-only (just the panels). A new `focusedKind` flag in `App.tsx` says whether the chat or terminal content region is currently visible, replacing the old `activeView === "terminal"` top-level switch.

**Tech Stack:** React + TypeScript, `@tanstack/react-store` (Store/useSelector — same primitive `projectStore.tsx` already uses), `@dnd-kit/core` + `@dnd-kit/sortable` (already a dependency, used today in `TerminalTabs.tsx`), Vitest + Testing Library.

**Spec:** `docs/superpowers/specs/2026-08-29-unified-session-terminal-tabs-design.md`

## Global Constraints

- Terminal ptys must keep surviving tab/project switches exactly as today (always-mounted per open project, `display:none` when inactive) — never change `TerminalPanel`'s mount lifecycle.
- Preserve today's lazy pty spawning: a project's terminals only ever spawn (go "live") the first time that project's terminal region is genuinely used (a terminal pill clicked, ⌨️+ clicked, or Cmd/Ctrl+T pressed while that project is active) — never merely from the project becoming active or from `UnifiedTabBar` wanting to list pills. Pill *metadata* (id + title) for a not-yet-live project comes from a cheap direct `loadProjectTerminals()` read ("peek"), never through store activation.
- `projectStore.activeTabId` keeps meaning exactly what it means today (the active/last-active chat session) — never overload it with terminal ids.
- Run `pnpm exec vitest run <file>` after every task's test file changes; run the full suite (`pnpm test`) and `pnpm exec tsc --noEmit` at the end.
- All new/modified `.ts`/`.tsx` files use the project's existing code style (2-space indent, no semicolon-free style — match surrounding files exactly).

---

### Task 1: `terminalStore` module

**Files:**
- Create: `web/src/stores/terminalStore.tsx`
- Create: `web/src/stores/terminalStore.test.tsx`

**Interfaces:**
- Produces: `export const PROCESSES_TAB_ID = "processes"`; `export interface TerminalInstance { id: string; title: string }`; `export interface TerminalStoreState { byProject: Record<string, { terminals: TerminalInstance[]; activeId: string; live: boolean }> }`; `export function getProjectTerminals(state: TerminalStoreState, projectPath: string): { terminals: TerminalInstance[]; activeId: string; live: boolean }`; `export function TerminalProvider({ children }: { children: ReactNode })`; `export function useTerminalState(): { state: TerminalStoreState; activate: (projectPath: string) => void; openTerminal: (projectPath: string) => void; closeTerminal: (projectPath: string, id: string) => void; setActiveId: (projectPath: string, id: string) => void; renameTerminal: (projectPath: string, id: string, title: string) => void }`.
- Consumes: `loadProjectTerminals`, `saveProjectTerminals`, `type PersistedTerminal` from `../components/Terminal/terminalPersistence` (unchanged, existing module).

- [ ] **Step 1: Write the store module**

```tsx
import { createContext, useContext, useCallback, useEffect, useRef, type ReactNode } from "react";
import { Store, useSelector } from "@tanstack/react-store";
import { loadProjectTerminals, saveProjectTerminals } from "../components/Terminal/terminalPersistence";

export const PROCESSES_TAB_ID = "processes";

export interface TerminalInstance {
  id: string;
  title: string;
}

interface ProjectTerminalState {
  terminals: TerminalInstance[];
  activeId: string;
  live: boolean;
}

export interface TerminalStoreState {
  byProject: Record<string, ProjectTerminalState>;
}

type TerminalAction =
  | { type: "SET_PROJECT_TERMINALS"; projectPath: string; terminals: TerminalInstance[]; activeId: string }
  | { type: "CLOSE_TERMINAL"; projectPath: string; id: string }
  | { type: "SET_ACTIVE_ID"; projectPath: string; id: string }
  | { type: "RENAME_TERMINAL"; projectPath: string; id: string; title: string };

const initialState: TerminalStoreState = { byProject: {} };

let nextTerminalSeq = 1;

function newTerminal(): TerminalInstance {
  const n = nextTerminalSeq++;
  return { id: `term-${n}-${Date.now()}`, title: `Terminal ${n}` };
}

/** Keeps the module-level title counter ahead of any restored terminal
 *  numbers, so a newly opened terminal after a restore doesn't reuse a
 *  "Terminal N" title that's already on screen. */
function bumpSeqPast(titles: string[]) {
  for (const title of titles) {
    const m = /^Terminal (\d+)$/.exec(title);
    if (m) nextTerminalSeq = Math.max(nextTerminalSeq, Number(m[1]) + 1);
  }
}

function terminalReducer(state: TerminalStoreState, action: TerminalAction): TerminalStoreState {
  switch (action.type) {
    case "SET_PROJECT_TERMINALS":
      return {
        byProject: {
          ...state.byProject,
          [action.projectPath]: { terminals: action.terminals, activeId: action.activeId, live: true },
        },
      };
    case "CLOSE_TERMINAL": {
      const cur = state.byProject[action.projectPath];
      if (!cur) return state;
      const terminals = cur.terminals.filter((t) => t.id !== action.id);
      const activeId =
        cur.activeId === action.id
          ? terminals.length > 0
            ? terminals[terminals.length - 1].id
            : ""
          : cur.activeId;
      return { byProject: { ...state.byProject, [action.projectPath]: { ...cur, terminals, activeId } } };
    }
    case "SET_ACTIVE_ID": {
      const cur = state.byProject[action.projectPath];
      if (!cur) return state;
      return { byProject: { ...state.byProject, [action.projectPath]: { ...cur, activeId: action.id } } };
    }
    case "RENAME_TERMINAL": {
      const cur = state.byProject[action.projectPath];
      if (!cur) return state;
      const terminals = cur.terminals.map((t) => (t.id === action.id ? { ...t, title: action.title } : t));
      return { byProject: { ...state.byProject, [action.projectPath]: { ...cur, terminals } } };
    }
    default:
      return state;
  }
}

/** Live state if this project's terminal region has actually been used this
 *  session, else a cheap "peek" of what's persisted on disk (id + title
 *  only) — so a not-yet-live project's pills can still be listed and
 *  clicked without spawning any pty. See the design spec's "peek vs live"
 *  section for why this split exists. */
export function getProjectTerminals(
  state: TerminalStoreState,
  projectPath: string,
): { terminals: TerminalInstance[]; activeId: string; live: boolean } {
  const entry = state.byProject[projectPath];
  if (entry?.live) return entry;
  const saved = loadProjectTerminals(projectPath);
  return { terminals: saved?.terminals ?? [], activeId: saved?.activeId ?? "", live: false };
}

interface TerminalContextType {
  state: TerminalStoreState;
  /** Idempotent: restores this project's persisted terminals (or spawns one
   *  fresh terminal if none were persisted) and marks it live. No-op if
   *  already live. */
  activate: (projectPath: string) => void;
  /** Ensures the project is live (seeding from disk first if it wasn't yet
   *  live), then appends and activates one new terminal. */
  openTerminal: (projectPath: string) => void;
  closeTerminal: (projectPath: string, id: string) => void;
  /** Ensures the project is live, then sets its active id (a terminal id or
   *  PROCESSES_TAB_ID). */
  setActiveId: (projectPath: string, id: string) => void;
  renameTerminal: (projectPath: string, id: string, title: string) => void;
}

const TerminalContext = createContext<TerminalContextType | null>(null);

export function TerminalProvider({ children }: { children: ReactNode }) {
  const storeRef = useRef<Store<TerminalStoreState> | null>(null);
  if (!storeRef.current) storeRef.current = new Store(initialState);
  const store = storeRef.current;
  const state = useSelector(store);

  const dispatch = useCallback(
    (action: TerminalAction) => store.setState((prev) => terminalReducer(prev, action)),
    [store],
  );

  const activate = useCallback(
    (projectPath: string) => {
      if (store.state.byProject[projectPath]?.live) return;
      const saved = loadProjectTerminals(projectPath);
      if (saved) {
        bumpSeqPast(saved.terminals.map((t) => t.title));
        const activeId =
          saved.activeId &&
          (saved.activeId === PROCESSES_TAB_ID || saved.terminals.some((t) => t.id === saved.activeId))
            ? saved.activeId
            : saved.terminals[saved.terminals.length - 1].id;
        dispatch({ type: "SET_PROJECT_TERMINALS", projectPath, terminals: saved.terminals, activeId });
        return;
      }
      const term = newTerminal();
      dispatch({ type: "SET_PROJECT_TERMINALS", projectPath, terminals: [term], activeId: term.id });
    },
    [store, dispatch],
  );

  const openTerminal = useCallback(
    (projectPath: string) => {
      const cur = store.state.byProject[projectPath];
      const baseTerminals = cur?.live ? cur.terminals : loadProjectTerminals(projectPath)?.terminals ?? [];
      bumpSeqPast(baseTerminals.map((t) => t.title));
      const term = newTerminal();
      dispatch({
        type: "SET_PROJECT_TERMINALS",
        projectPath,
        terminals: [...baseTerminals, term],
        activeId: term.id,
      });
    },
    [store, dispatch],
  );

  const closeTerminal = useCallback(
    (projectPath: string, id: string) => dispatch({ type: "CLOSE_TERMINAL", projectPath, id }),
    [dispatch],
  );

  const setActiveId = useCallback(
    (projectPath: string, id: string) => {
      activate(projectPath);
      dispatch({ type: "SET_ACTIVE_ID", projectPath, id });
    },
    [activate, dispatch],
  );

  const renameTerminal = useCallback(
    (projectPath: string, id: string, title: string) =>
      dispatch({ type: "RENAME_TERMINAL", projectPath, id, title }),
    [dispatch],
  );

  // Persist every live project's terminals/activeId (debounced), mirroring
  // TerminalTabs.tsx's original per-project persistence effect.
  const skipNextSaveRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    const timers: ReturnType<typeof setTimeout>[] = [];
    for (const [projectPath, entry] of Object.entries(state.byProject)) {
      if (!entry.live) continue;
      if (skipNextSaveRef.current.has(projectPath)) {
        skipNextSaveRef.current.delete(projectPath);
        continue;
      }
      timers.push(setTimeout(() => saveProjectTerminals(projectPath, entry.terminals, entry.activeId), 200));
    }
    return () => timers.forEach(clearTimeout);
  }, [state.byProject]);

  // Cross-window sync: another window's terminal open/close/rename updates
  // this window's already-live projects too. A project this window never
  // activated stays a cheap peek and picks up the change next render.
  useEffect(() => {
    const handler = (e: StorageEvent) => {
      if (e.key !== "ocode.ui.terminals.project.v1") return;
      for (const projectPath of Object.keys(store.state.byProject)) {
        const cur = store.state.byProject[projectPath];
        if (!cur.live) continue;
        const saved = loadProjectTerminals(projectPath);
        if (!saved) {
          if (cur.terminals.length !== 0) {
            skipNextSaveRef.current.add(projectPath);
            dispatch({ type: "SET_PROJECT_TERMINALS", projectPath, terminals: [], activeId: "" });
          }
          continue;
        }
        const same =
          saved.terminals.length === cur.terminals.length &&
          saved.terminals.every((t, i) => t.id === cur.terminals[i]?.id && t.title === cur.terminals[i]?.title) &&
          saved.activeId === cur.activeId;
        if (same) continue;
        bumpSeqPast(saved.terminals.map((t) => t.title));
        skipNextSaveRef.current.add(projectPath);
        const activeId =
          saved.activeId &&
          (saved.activeId === PROCESSES_TAB_ID || saved.terminals.some((t) => t.id === saved.activeId))
            ? saved.activeId
            : (saved.terminals[saved.terminals.length - 1]?.id ?? "");
        dispatch({ type: "SET_PROJECT_TERMINALS", projectPath, terminals: saved.terminals, activeId });
      }
    };
    window.addEventListener("storage", handler);
    return () => window.removeEventListener("storage", handler);
  }, [store, dispatch]);

  return (
    <TerminalContext.Provider value={{ state, activate, openTerminal, closeTerminal, setActiveId, renameTerminal }}>
      {children}
    </TerminalContext.Provider>
  );
}

export function useTerminalState() {
  const ctx = useContext(TerminalContext);
  if (!ctx) throw new Error("useTerminalState must be used within TerminalProvider");
  return ctx;
}
```

- [ ] **Step 2: Write the store's tests**

```tsx
import { render, screen, act } from "@testing-library/react";
import { describe, it, expect, beforeEach } from "vitest";
import { TerminalProvider, useTerminalState, getProjectTerminals, PROCESSES_TAB_ID } from "./terminalStore";

function Harness({ projectPath }: { projectPath: string }) {
  const { state, activate, openTerminal, closeTerminal, setActiveId, renameTerminal } = useTerminalState();
  const { terminals, activeId, live } = getProjectTerminals(state, projectPath);
  return (
    <div>
      <div data-testid="live">{String(live)}</div>
      <div data-testid="active-id">{activeId}</div>
      <div data-testid="count">{terminals.length}</div>
      <div data-testid="titles">{terminals.map((t) => t.title).join(",")}</div>
      <button onClick={() => activate(projectPath)}>activate</button>
      <button onClick={() => openTerminal(projectPath)}>open</button>
      <button onClick={() => activeId && closeTerminal(projectPath, activeId)}>close-active</button>
      <button onClick={() => setActiveId(projectPath, PROCESSES_TAB_ID)}>focus-processes</button>
    </div>
  );
}

beforeEach(() => {
  window.localStorage.clear();
});

function seedPersisted(projectPath: string, terminals: { id: string; title: string }[], activeId: string) {
  window.localStorage.setItem(
    "ocode.ui.terminals.project.v1",
    JSON.stringify({ version: 1, projects: { [projectPath]: { terminals, activeId } } }),
  );
}

describe("terminalStore", () => {
  it("getProjectTerminals peeks persisted metadata without going live", () => {
    seedPersisted("/proj", [{ id: "term-1-1", title: "Terminal 1" }], "term-1-1");
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    expect(screen.getByTestId("live").textContent).toBe("false");
    expect(screen.getByTestId("count").textContent).toBe("1");
    expect(screen.getByTestId("active-id").textContent).toBe("term-1-1");
  });

  it("activate() with nothing persisted creates one fresh terminal and goes live", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("count").textContent).toBe("1");
  });

  it("activate() with persisted terminals restores them and goes live", () => {
    seedPersisted("/proj", [{ id: "term-9-1", title: "Terminal 9" }], "term-9-1");
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("titles").textContent).toBe("Terminal 9");
  });

  it("openTerminal() on a never-activated project seeds from persisted terminals then appends a new one", () => {
    seedPersisted("/proj", [{ id: "term-9-1", title: "Terminal 9" }], "term-9-1");
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("open").click());
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("count").textContent).toBe("2");
  });

  it("closeTerminal() falls back the active id to the last remaining terminal", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    act(() => screen.getByText("open").click());
    expect(screen.getByTestId("count").textContent).toBe("2");
    act(() => screen.getByText("close-active").click());
    expect(screen.getByTestId("count").textContent).toBe("1");
  });

  it("setActiveId() to the Processes sentinel activates a never-activated project first", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("focus-processes").click());
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("active-id").textContent).toBe(PROCESSES_TAB_ID);
  });
});
```

- [ ] **Step 3: Run the new tests**

Run: `pnpm exec vitest run src/stores/terminalStore.test.tsx`
Expected: all 6 tests PASS.

- [ ] **Step 4: Commit**

```bash
git add web/src/stores/terminalStore.tsx web/src/stores/terminalStore.test.tsx
git commit -m "feat: add terminalStore with peek/live terminal state per project"
```

---

### Task 2: Unified tab-order persistence

**Files:**
- Create: `web/src/components/Layout/tabOrderPersistence.ts`
- Create: `web/src/components/Layout/tabOrderPersistence.test.ts`

**Interfaces:**
- Produces: `export type UnifiedTabKey = \`chat:${string}\` | \`term:${string}\`;`, `export function loadTabOrder(projectPath: string): UnifiedTabKey[]`, `export function saveTabOrder(projectPath: string, order: UnifiedTabKey[]): void`, `export function reconcileTabOrder(saved: UnifiedTabKey[], chatIds: string[], terminalIds: string[]): UnifiedTabKey[]`.
- Consumes: nothing beyond `window.localStorage`.

- [ ] **Step 1: Write the module**

```ts
// Persists the merged (chat session + terminal) tab order per project, so
// drag-reordering the unified tab bar survives reloads. Only stores an
// ordering of composite keys — never the tabs' own data (title, etc.),
// which stays owned by projectStore/terminalStore.

export type UnifiedTabKey = `chat:${string}` | `term:${string}`;

const ORDER_KEY = "ocode.ui.tabOrder.v1";

interface OrderFile {
  version: 1;
  projects: Record<string, UnifiedTabKey[]>;
}

function readOrderFile(): OrderFile {
  try {
    const raw = window.localStorage.getItem(ORDER_KEY);
    if (!raw) return { version: 1, projects: {} };
    const parsed = JSON.parse(raw) as OrderFile;
    if (!parsed || parsed.version !== 1 || typeof parsed.projects !== "object") {
      return { version: 1, projects: {} };
    }
    return parsed;
  } catch (err) {
    console.error("Failed to load tab order:", err);
    return { version: 1, projects: {} };
  }
}

export function loadTabOrder(projectPath: string): UnifiedTabKey[] {
  return readOrderFile().projects[projectPath] ?? [];
}

export function saveTabOrder(projectPath: string, order: UnifiedTabKey[]) {
  const file = readOrderFile();
  if (order.length === 0) {
    delete file.projects[projectPath];
  } else {
    file.projects[projectPath] = order;
  }
  try {
    window.localStorage.setItem(ORDER_KEY, JSON.stringify(file));
  } catch (err) {
    console.error("Failed to persist tab order:", err);
  }
}

/** Reconciles a saved order against the live id sets: drops stale keys, and
 *  appends any live id missing from the saved order (new tabs created since
 *  last save) at the end, in the order they appear in chatIds/terminalIds. */
export function reconcileTabOrder(
  saved: UnifiedTabKey[],
  chatIds: string[],
  terminalIds: string[],
): UnifiedTabKey[] {
  const liveKeys: UnifiedTabKey[] = [
    ...chatIds.map((id): UnifiedTabKey => `chat:${id}`),
    ...terminalIds.map((id): UnifiedTabKey => `term:${id}`),
  ];
  const liveKeySet = new Set(liveKeys);
  const kept = saved.filter((k) => liveKeySet.has(k));
  const keptSet = new Set(kept);
  const appended = liveKeys.filter((k) => !keptSet.has(k));
  return [...kept, ...appended];
}
```

- [ ] **Step 2: Write the tests**

```ts
import { describe, it, expect, beforeEach } from "vitest";
import { loadTabOrder, saveTabOrder, reconcileTabOrder } from "./tabOrderPersistence";

beforeEach(() => {
  window.localStorage.clear();
});

describe("tabOrderPersistence", () => {
  it("returns an empty order for a project with nothing saved", () => {
    expect(loadTabOrder("/proj")).toEqual([]);
  });

  it("round-trips a saved order", () => {
    saveTabOrder("/proj", ["term:t1", "chat:c1"]);
    expect(loadTabOrder("/proj")).toEqual(["term:t1", "chat:c1"]);
  });

  it("removes the project entry when saved with an empty order", () => {
    saveTabOrder("/proj", ["chat:c1"]);
    saveTabOrder("/proj", []);
    expect(loadTabOrder("/proj")).toEqual([]);
  });
});

describe("reconcileTabOrder", () => {
  it("keeps the saved order for ids that are still live", () => {
    expect(reconcileTabOrder(["term:t1", "chat:c1"], ["c1"], ["t1"])).toEqual(["term:t1", "chat:c1"]);
  });

  it("drops stale keys no longer present in either live id set", () => {
    expect(reconcileTabOrder(["term:t1", "chat:c1", "chat:closed"], ["c1"], ["t1"])).toEqual([
      "term:t1",
      "chat:c1",
    ]);
  });

  it("appends new live ids missing from the saved order, at the end", () => {
    expect(reconcileTabOrder(["chat:c1"], ["c1", "c2"], ["t1"])).toEqual(["chat:c1", "chat:c2", "term:t1"]);
  });
});
```

- [ ] **Step 3: Run the new tests**

Run: `pnpm exec vitest run src/components/Layout/tabOrderPersistence.test.ts`
Expected: all 6 tests PASS.

- [ ] **Step 4: Commit**

```bash
git add web/src/components/Layout/tabOrderPersistence.ts web/src/components/Layout/tabOrderPersistence.test.ts
git commit -m "feat: add unified chat+terminal tab order persistence"
```

---

### Task 3: Shrink `TerminalTabs.tsx` to content-only

**Files:**
- Modify: `web/src/components/Terminal/TerminalTabs.tsx` (full rewrite of the file's body — same exported names)
- Modify: `web/src/components/Terminal/TerminalTabs.test.tsx` (full rewrite — same `describe` block)

**Interfaces:**
- Consumes: `useTerminalState`, `getProjectTerminals`, `PROCESSES_TAB_ID` from `../../stores/terminalStore` (Task 1).
- Produces (unchanged from today): `export interface TerminalTabsHandle { openTerminal: () => void; closeActiveTerminal: () => boolean }`; `export default TerminalTabs` — a `forwardRef<TerminalTabsHandle, { active: boolean; projectPath: string }>` component. App.tsx's usage (`ref`, `active`, `projectPath` props) is unchanged.

- [ ] **Step 1: Replace the component body**

Replace the full contents of `web/src/components/Terminal/TerminalTabs.tsx` with:

```tsx
import { useEffect, useImperativeHandle, forwardRef } from "react";
import TerminalPanel from "./TerminalPanel";
import ProcessesPanel from "./ProcessesPanel";
import { useTerminalConfig } from "@/hooks/useTerminalConfig";
import { useTerminalState, getProjectTerminals, PROCESSES_TAB_ID } from "../../stores/terminalStore";

export interface TerminalTabsHandle {
  openTerminal: () => void;
  /** Close the active terminal instance. Returns false when there is none,
   *  so the caller (Cmd/Ctrl+W) can fall through to closing the session tab. */
  closeActiveTerminal: () => boolean;
}

/**
 * Content-only: renders the active terminal/Processes panel for one project.
 * The tab strip (open/close/rename/reorder/+) lives in UnifiedTabBar, which
 * shares this project's terminal state via terminalStore. This component
 * still triggers activation (restoring persisted terminals, or spawning one
 * fresh, the first time this project's terminal region is actually shown —
 * never just from the project becoming active) and stays always-mounted per
 * open project so ptys survive tab/project switches (see App.tsx).
 */
const TerminalTabs = forwardRef<TerminalTabsHandle, { active: boolean; projectPath: string }>(
  function TerminalTabs({ active, projectPath }, ref) {
    const { available, loading, error, scrollbackLines, fontFamily, fontSize } = useTerminalConfig();
    const { state: terminalState, activate, openTerminal, closeTerminal } = useTerminalState();
    const { terminals, activeId } = getProjectTerminals(terminalState, projectPath);

    useEffect(() => {
      if (!active || !available) return;
      activate(projectPath);
    }, [active, available, projectPath, activate]);

    useImperativeHandle(ref, () => ({
      openTerminal: () => openTerminal(projectPath),
      closeActiveTerminal: () => {
        if (!activeId || !terminals.some((t) => t.id === activeId)) return false;
        closeTerminal(projectPath, activeId);
        return true;
      },
    }));

    if (loading || (available && scrollbackLines <= 0)) {
      return <div className="p-4 text-sm text-zinc-500">Checking terminal availability…</div>;
    }

    if (error) {
      return <div className="p-4 text-sm text-red-400">Failed to read terminal setting: {error}</div>;
    }

    if (!available) {
      return (
        <div className="p-4 text-sm text-zinc-400">
          The interactive terminal is unavailable on this server: it requires server
          authentication or a loopback bind address.
        </div>
      );
    }

    return (
      <div className="relative h-full bg-zinc-900">
        {active && (
          <div className={activeId === PROCESSES_TAB_ID ? "absolute inset-0" : "absolute inset-0 hidden"}>
            <ProcessesPanel projectPath={projectPath} />
          </div>
        )}

        {terminals.map((t) => (
          <div key={t.id} className={t.id === activeId ? "absolute inset-0" : "absolute inset-0 hidden"}>
            <TerminalPanel
              id={t.id}
              active={active && t.id === activeId}
              scrollbackLines={scrollbackLines}
              fontFamily={fontFamily}
              fontSize={fontSize}
              projectPath={projectPath}
            />
          </div>
        ))}
        {terminals.length === 0 && activeId !== PROCESSES_TAB_ID && (
          <div className="p-4 text-sm text-zinc-500">No terminals open. Use ⌨️+ in the tab bar to start one.</div>
        )}
      </div>
    );
  },
);

export default TerminalTabs;
```

- [ ] **Step 2: Replace the test file**

Replace the full contents of `web/src/components/Terminal/TerminalTabs.test.tsx` with:

```tsx
import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createRef } from "react";
import { TerminalProvider } from "../../stores/terminalStore";
import type { TerminalTabsHandle } from "./TerminalTabs";

// Real xterm needs canvas/layout that jsdom does not have, so the terminal
// itself is stubbed: this test covers activation/open/close behaviour and
// that closing a terminal tears down that terminal's socket (which is what
// kills the pty server-side).
const disposeSpy = vi.fn();
vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    options: Record<string, unknown> = {};
    loadAddon = vi.fn();
    open = vi.fn();
    write = vi.fn();
    focus = vi.fn();
    onData = vi.fn(() => ({ dispose: vi.fn() }));
    dispose = disposeSpy;
    attachCustomKeyEventHandler = vi.fn(() => true);
  },
}));
vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit = vi.fn();
  },
}));
vi.mock("@xterm/addon-serialize", () => ({
  SerializeAddon: class {
    serialize = vi.fn(() => "");
  },
}));
vi.mock("@xterm/xterm/css/xterm.css", () => ({}));
vi.mock("@xterm/addon-webgl", () => ({
  WebglAddon: class {
    dispose = vi.fn();
  },
}));
vi.mock("@/api/client", () => ({
  api: {
    getTerminalConfig: () => Promise.resolve({ available: true, scrollback_lines: 9999, work_dir: "/project" }),
    getTerminalProcesses: () => Promise.resolve([]),
  },
  apiPath: (p: string) => p,
  authToken: () => "tok",
}));

const sockets: MockSocket[] = [];

class MockSocket {
  static OPEN = 1;
  readyState = 1;
  binaryType = "arraybuffer";
  onopen: (() => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: ((e: { wasClean: boolean; code: number; reason: string }) => void) | null = null;
  send = vi.fn();
  close = vi.fn();
  constructor(public url: string) {
    sockets.push(this);
  }
}

beforeEach(() => {
  sockets.length = 0;
  disposeSpy.mockClear();
  window.localStorage.clear();
  vi.stubGlobal("WebSocket", MockSocket as unknown as typeof WebSocket);
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
});

async function renderTabs(projectPath = "/project") {
  const { default: TerminalTabs } = await import("./TerminalTabs");
  const ref = createRef<TerminalTabsHandle>();
  const utils = render(
    <TerminalProvider>
      <TerminalTabs ref={ref} active projectPath={projectPath} />
    </TerminalProvider>,
  );
  // First terminal is opened lazily once the panel becomes active.
  await waitFor(() => expect(sockets.length).toBe(1));
  return { ref, ...utils };
}

describe("TerminalTabs", () => {
  it("connects each terminal socket with the tab's project_path", async () => {
    await renderTabs();
    expect(sockets[0].url).toContain(`project_path=${encodeURIComponent("/project")}`);
  });

  it("opens an additional terminal (and socket) via the imperative openTerminal() handle", async () => {
    const { ref } = await renderTabs();
    expect(sockets.length).toBe(1);

    ref.current?.openTerminal();

    await waitFor(() => expect(sockets.length).toBe(2));
  });

  it("closes the active terminal and its websocket via closeActiveTerminal()", async () => {
    const { ref } = await renderTabs();
    ref.current?.openTerminal();
    await waitFor(() => expect(sockets.length).toBe(2));
    const secondSocket = sockets[1]; // newest terminal becomes active

    const closed = ref.current?.closeActiveTerminal();

    expect(closed).toBe(true);
    await waitFor(() => expect(secondSocket.close).toHaveBeenCalled());
  });

  it("closeActiveTerminal() returns false once no terminal remains", async () => {
    const { ref } = await renderTabs();

    expect(ref.current?.closeActiveTerminal()).toBe(true);
    expect(ref.current?.closeActiveTerminal()).toBe(false);
  });

  it("restores the same number of terminal tabs (fresh sockets) after remount", async () => {
    const { default: TerminalTabs } = await import("./TerminalTabs");
    const ref = createRef<TerminalTabsHandle>();
    const { unmount } = render(
      <TerminalProvider>
        <TerminalTabs ref={ref} active projectPath="/project" />
      </TerminalProvider>,
    );
    await waitFor(() => expect(sockets.length).toBe(1));
    ref.current?.openTerminal();
    await waitFor(() => expect(sockets.length).toBe(2));
    // A fresh <TerminalProvider> below means a brand-new (empty) store, so
    // this only passes if the second terminal was actually persisted to
    // localStorage and restored from there — not carried over in memory.
    unmount();

    sockets.length = 0;
    render(
      <TerminalProvider>
        <TerminalTabs active projectPath="/project" />
      </TerminalProvider>,
    );

    await waitFor(() => expect(sockets.length).toBe(2));
  });
});
```

- [ ] **Step 3: Run the tests**

Run: `pnpm exec vitest run src/components/Terminal/TerminalTabs.test.tsx`
Expected: all 5 tests PASS.

- [ ] **Step 4: Commit**

```bash
git add web/src/components/Terminal/TerminalTabs.tsx web/src/components/Terminal/TerminalTabs.test.tsx
git commit -m "refactor: shrink TerminalTabs to content-only, backed by terminalStore"
```

---

### Task 4: `UnifiedTabBar` component (replaces `OpenSessionBar`)

**Files:**
- Create: `web/src/components/Layout/UnifiedTabBar.tsx`
- Create: `web/src/components/Layout/UnifiedTabBar.test.tsx`
- Delete: `web/src/components/Layout/OpenSessionBar.tsx`

**Interfaces:**
- Consumes: `useProjectState` from `../../stores/projectStore`; `useChatDispatch`, `useChatSelector`, `getSessionSlice`, `type ChatState`, `type SessionSlice` from `../../stores/chatStore`; `useTerminalConfig` from `@/hooks/useTerminalConfig`; `useTerminalState`, `getProjectTerminals`, `PROCESSES_TAB_ID` from `../../stores/terminalStore` (Task 1); `loadTabOrder`, `saveTabOrder`, `reconcileTabOrder`, `type UnifiedTabKey` from `./tabOrderPersistence` (Task 2); `isNewSessionTabEmpty` from `../../lib/tabDrafts`; `clearQueue` from `../../lib/tabQueue`; `cancelLiveDeltas` from `../../lib/sessionEvents`; `api` from `../../api/client`.
- Produces: `export default function UnifiedTabBar({ focusedKind, onFocusKindChange }: { focusedKind: "chat" | "terminal"; onFocusKindChange: (kind: "chat" | "terminal") => void })`.

- [ ] **Step 1: Write the component**

```tsx
import { useCallback, useMemo, useRef, useState } from "react";
import { X, List, Plus, Loader2 } from "lucide-react";
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  horizontalListSortingStrategy,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useChatDispatch, useChatSelector, getSessionSlice, type ChatState, type SessionSlice } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { useTerminalConfig } from "@/hooks/useTerminalConfig";
import { useTerminalState, getProjectTerminals, PROCESSES_TAB_ID } from "../../stores/terminalStore";
import { isNewSessionTabEmpty } from "../../lib/tabDrafts";
import { clearQueue } from "../../lib/tabQueue";
import { cancelLiveDeltas } from "../../lib/sessionEvents";
import { api } from "../../api/client";
import { loadTabOrder, saveTabOrder, reconcileTabOrder, type UnifiedTabKey } from "./tabOrderPersistence";

// While a tab's session has an in-flight turn, show what it's doing as a
// badge alongside its title (never replacing the title). Reverts once idle.
function activeProcessLabel(slice: SessionSlice): string | null {
  if (!slice.turnActive) return null;
  for (let i = slice.live.length - 1; i >= 0; i--) {
    const part = slice.live[i];
    if (part.kind === "tool") return shortCommandLabel(part.command, part.tool);
  }
  return "Running…";
}

function shortCommandLabel(command: string | undefined, tool: string): string {
  if (!command) return tool;
  let candidate: string = tool;
  try {
    const parsed = JSON.parse(command) as Record<string, unknown>;
    if (typeof parsed.command === "string") candidate = parsed.command;
    else if (typeof parsed.description === "string") candidate = parsed.description;
    else {
      const firstString = Object.values(parsed).find((v): v is string => typeof v === "string");
      if (firstString) candidate = firstString;
    }
  } catch {
    // Not valid JSON — fall back to the tool name.
  }
  return candidate.length > 40 ? `${candidate.slice(0, 40)}…` : candidate;
}

interface ChatDerived {
  id: string;
  initialized: boolean;
  hasPending: boolean;
  processLabel: string | null;
}

function chatDerivedEqual(a: ChatDerived[], b: ChatDerived[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    const x = a[i];
    const y = b[i];
    if (x.id !== y.id || x.initialized !== y.initialized || x.hasPending !== y.hasPending || x.processLabel !== y.processLabel) {
      return false;
    }
  }
  return true;
}

interface TabPillProps {
  sortId: string;
  emoji: string;
  title: string;
  isActive: boolean;
  isLoading?: boolean;
  hasPending?: boolean;
  processLabel?: string | null;
  isEditing: boolean;
  editValue: string;
  onEditValueChange: (v: string) => void;
  onClick: () => void;
  onStartRename: () => void;
  onCommitRename: () => void;
  onCancelRename: () => void;
  onClose: (e: React.MouseEvent) => void;
}

function TabPill({
  sortId,
  emoji,
  title,
  isActive,
  isLoading,
  hasPending,
  processLabel,
  isEditing,
  editValue,
  onEditValueChange,
  onClick,
  onStartRename,
  onCommitRename,
  onCancelRename,
  onClose,
}: TabPillProps) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: sortId });
  const style = { transform: CSS.Transform.toString(transform), transition, opacity: isDragging ? 0.5 : 1 };
  const displayTitle = title || sortId;

  return (
    <div
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
      onClick={onClick}
      onAuxClick={(e) => {
        if (e.button === 1) onClose(e);
      }}
      className={`flex items-center gap-1 px-2 py-1 rounded text-xs cursor-pointer shrink-0 touch-none transition-colors ${
        isActive ? "bg-zinc-800 text-zinc-100 border-t border-t-blue-500" : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800/60"
      }`}
    >
      <span aria-hidden className="shrink-0">
        {emoji}
      </span>
      {isLoading && <Loader2 className="w-3 h-3 animate-spin shrink-0" />}
      {hasPending && (
        <span className="h-1.5 w-1.5 rounded-full bg-amber-400 shrink-0" title="Waiting for a response in this tab" />
      )}
      {isEditing ? (
        <input
          autoFocus
          value={editValue}
          onChange={(e) => onEditValueChange(e.target.value)}
          onClick={(e) => e.stopPropagation()}
          onBlur={onCommitRename}
          onKeyDown={(e) => {
            if (e.key === "Enter") onCommitRename();
            else if (e.key === "Escape") onCancelRename();
          }}
          className="max-w-28 w-24 bg-zinc-950 text-zinc-100 rounded px-1 outline-none border border-blue-500"
        />
      ) : (
        <span
          className="max-w-28 truncate shrink-0"
          title="Double-click to rename, drag to reorder"
          onDoubleClick={(e) => {
            e.stopPropagation();
            onStartRename();
          }}
        >
          {displayTitle}
        </span>
      )}
      {processLabel && (
        <span
          className="max-w-24 truncate text-[10px] leading-none px-1 py-0.5 rounded bg-amber-500/20 text-amber-300 border border-amber-500/30 shrink-0"
          title={processLabel}
        >
          {processLabel}
        </span>
      )}
      <span
        role="button"
        tabIndex={0}
        aria-label={`Close ${displayTitle}`}
        title={`Close ${displayTitle}`}
        className="p-0.5 rounded hover:bg-zinc-700 text-zinc-500 hover:text-zinc-300 transition-colors shrink-0"
        onClick={onClose}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onClose(e as unknown as React.MouseEvent);
          }
        }}
      >
        <X className="w-3 h-3" />
      </span>
    </div>
  );
}

interface Props {
  focusedKind: "chat" | "terminal";
  onFocusKindChange: (kind: "chat" | "terminal") => void;
}

export default function UnifiedTabBar({ focusedKind, onFocusKindChange }: Props) {
  const {
    state: projectState,
    tabs: chatTabs,
    activeTabId: activeChatId,
    openSessionTab,
    closeSessionTab,
    openNewSessionTab,
    toggleSessionPicker,
    dispatch: projectDispatch,
  } = useProjectState();
  const activeProjectPath = projectState.activeProject?.path ?? "";
  const chatDispatch = useChatDispatch();
  const { available: terminalAvailable } = useTerminalConfig();
  const { state: terminalState, openTerminal, closeTerminal, setActiveId: setActiveTerminalId, renameTerminal } =
    useTerminalState();
  const { terminals, activeId: activeTerminalId } = getProjectTerminals(terminalState, activeProjectPath);

  const chatDerived = useChatSelector(
    (s: ChatState): ChatDerived[] =>
      chatTabs.map((tab) => {
        const slice = getSessionSlice(s, tab.id);
        return {
          id: tab.id,
          initialized: slice.initialized,
          hasPending: activeChatId !== tab.id && (slice.pendingPermission !== null || slice.pendingQuestion !== null),
          processLabel: activeProcessLabel(slice),
        };
      }),
    chatDerivedEqual,
  );

  const [editing, setEditing] = useState<{ kind: "chat" | "terminal"; id: string } | null>(null);
  const [editValue, setEditValue] = useState("");

  const chatIds = useMemo(() => chatTabs.map((t) => t.id), [chatTabs]);
  const terminalIds = useMemo(() => terminals.map((t) => t.id), [terminals]);
  const order = useMemo(
    () => reconcileTabOrder(loadTabOrder(activeProjectPath), chatIds, terminalIds),
    [activeProjectPath, chatIds, terminalIds],
  );

  const dndSensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      if (!over || active.id === over.id) return;
      const oldIndex = order.indexOf(active.id as UnifiedTabKey);
      const newIndex = order.indexOf(over.id as UnifiedTabKey);
      if (oldIndex === -1 || newIndex === -1) return;
      saveTabOrder(activeProjectPath, arrayMove(order, oldIndex, newIndex));
    },
    [order, activeProjectPath],
  );

  const scrollRef = useRef<HTMLDivElement>(null);
  const handleWheel = (e: React.WheelEvent<HTMLDivElement>) => {
    const el = scrollRef.current;
    if (!el || el.scrollWidth <= el.clientWidth + 1) return;
    const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY;
    if (delta === 0) return;
    const atLeft = el.scrollLeft <= 0;
    const atRight = el.scrollLeft + el.clientWidth >= el.scrollWidth - 1;
    if ((delta < 0 && atLeft) || (delta > 0 && atRight)) return;
    e.preventDefault();
    el.scrollLeft += delta;
  };

  const handleChatClick = useCallback(
    (id: string, title: string) => {
      onFocusKindChange("chat");
      if (activeChatId === id) return;
      openSessionTab(id, title);
    },
    [activeChatId, openSessionTab, onFocusKindChange],
  );

  const handleTerminalClick = useCallback(
    (id: string) => {
      onFocusKindChange("terminal");
      setActiveTerminalId(activeProjectPath, id);
    },
    [activeProjectPath, setActiveTerminalId, onFocusKindChange],
  );

  const startRename = useCallback((kind: "chat" | "terminal", id: string, currentTitle: string) => {
    setEditing({ kind, id });
    setEditValue(currentTitle);
  }, []);

  const commitRename = useCallback(() => {
    const target = editing;
    setEditing(null);
    if (!target) return;
    const title = editValue.trim();
    if (!title) return;
    if (target.kind === "chat") {
      projectDispatch({ type: "UPDATE_TAB_TITLE", id: target.id, title, manual: true });
      if (!target.id.startsWith("new-")) {
        api.setSessionTitle(target.id, title).catch((err) => {
          console.error("failed to save renamed tab title", err);
        });
      }
    } else {
      renameTerminal(activeProjectPath, target.id, title);
    }
  }, [editing, editValue, projectDispatch, renameTerminal, activeProjectPath]);

  const handleCloseChat = useCallback(
    (e: React.MouseEvent, id: string) => {
      e.stopPropagation();
      closeSessionTab(id);
      chatDispatch({ type: "RESET", sessionId: id });
      cancelLiveDeltas(id);
      clearQueue(id);
    },
    [closeSessionTab, chatDispatch],
  );

  const handleCloseTerminal = useCallback(
    (e: React.MouseEvent, id: string) => {
      e.stopPropagation();
      closeTerminal(activeProjectPath, id);
    },
    [closeTerminal, activeProjectPath],
  );

  const handleNewChat = useCallback(() => {
    onFocusKindChange("chat");
    openNewSessionTab(isNewSessionTabEmpty(activeChatId));
  }, [activeChatId, openNewSessionTab, onFocusKindChange]);

  const handleNewTerminal = useCallback(() => {
    onFocusKindChange("terminal");
    openTerminal(activeProjectPath);
  }, [activeProjectPath, openTerminal, onFocusKindChange]);

  const isLoadingChatTab = (tabId: string, initialized: boolean) => !tabId.startsWith("new-") && !initialized;

  if (!projectState.activeProject) return null;

  const chatById = new Map(chatTabs.map((t) => [t.id, t]));
  const terminalById = new Map(terminals.map((t) => [t.id, t]));

  return (
    <div
      ref={scrollRef}
      onWheel={handleWheel}
      className="flex items-center h-8 px-2 gap-0.5 bg-zinc-900 border-b border-zinc-700 overflow-x-auto overflow-y-hidden scrollbar-hide flex-nowrap min-w-0 w-full touch-pan-x overscroll-x-contain"
      style={{ WebkitOverflowScrolling: "touch" } as React.CSSProperties}
    >
      <DndContext sensors={dndSensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
        <SortableContext items={order} strategy={horizontalListSortingStrategy}>
          {order.map((key) => {
            if (key.startsWith("chat:")) {
              const id = key.slice("chat:".length);
              const tab = chatById.get(id);
              if (!tab) return null;
              const derived = chatDerived.find((d) => d.id === id);
              return (
                <TabPill
                  key={key}
                  sortId={key}
                  emoji="💬"
                  title={tab.title}
                  isActive={focusedKind === "chat" && activeChatId === id}
                  isLoading={isLoadingChatTab(id, derived?.initialized ?? false)}
                  hasPending={derived?.hasPending ?? false}
                  processLabel={derived?.processLabel ?? null}
                  isEditing={editing?.kind === "chat" && editing.id === id}
                  editValue={editValue}
                  onEditValueChange={setEditValue}
                  onClick={() => handleChatClick(id, tab.title)}
                  onStartRename={() => startRename("chat", id, tab.title || "")}
                  onCommitRename={commitRename}
                  onCancelRename={() => setEditing(null)}
                  onClose={(e) => handleCloseChat(e, id)}
                />
              );
            }
            const id = key.slice("term:".length);
            const term = terminalById.get(id);
            if (!term) return null;
            return (
              <TabPill
                key={key}
                sortId={key}
                emoji="⌨️"
                title={term.title}
                isActive={focusedKind === "terminal" && activeTerminalId === id}
                isEditing={editing?.kind === "terminal" && editing.id === id}
                editValue={editValue}
                onEditValueChange={setEditValue}
                onClick={() => handleTerminalClick(id)}
                onStartRename={() => startRename("terminal", id, term.title)}
                onCommitRename={commitRename}
                onCancelRename={() => setEditing(null)}
                onClose={(e) => handleCloseTerminal(e, id)}
              />
            );
          })}
        </SortableContext>
      </DndContext>

      <button
        onClick={handleNewChat}
        aria-label="New chat session"
        title="New chat session"
        className="flex shrink-0 items-center gap-0.5 px-2 py-1 rounded text-xs text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition-colors"
      >
        <span aria-hidden>💬</span>
        <Plus className="w-3 h-3" />
      </button>

      {terminalAvailable && (
        <button
          onClick={handleNewTerminal}
          aria-label="New terminal"
          title="New terminal"
          className="flex shrink-0 items-center gap-0.5 px-2 py-1 rounded text-xs text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition-colors"
        >
          <span aria-hidden>⌨️</span>
          <Plus className="w-3 h-3" />
        </button>
      )}

      {terminalAvailable && (
        <button
          onClick={() => {
            onFocusKindChange("terminal");
            setActiveTerminalId(activeProjectPath, PROCESSES_TAB_ID);
          }}
          className={`flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-xs transition-colors ${
            focusedKind === "terminal" && activeTerminalId === PROCESSES_TAB_ID
              ? "bg-zinc-700 text-white"
              : "text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
          }`}
        >
          Processes
        </button>
      )}

      <button
        onClick={toggleSessionPicker}
        className="flex items-center gap-1 px-2 py-1 rounded text-xs text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition-colors shrink-0"
        title="Browse all sessions"
      >
        <List className="w-3.5 h-3.5" />
        <span className="hidden sm:inline">All sessions</span>
      </button>
    </div>
  );
}
```

- [ ] **Step 2: Write the component's tests**

```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { ChatProvider } from "../../stores/chatStore";
import { TerminalProvider } from "../../stores/terminalStore";
import UnifiedTabBar from "./UnifiedTabBar";

vi.mock("@/hooks/useTerminalConfig", () => ({
  useTerminalConfig: () => ({ available: true }),
}));

vi.mock("../../api/client", () => ({
  api: { setSessionTitle: vi.fn().mockResolvedValue(undefined) },
}));

let projectFake: {
  state: { activeProject: { path: string; name: string } | null };
  tabs: { id: string; projectPath: string; title: string; activeSubTab: "chat" }[];
  activeTabId: string | null;
};
const openSessionTab = vi.fn();
const closeSessionTab = vi.fn();
const openNewSessionTab = vi.fn(() => "new-1");
const toggleSessionPicker = vi.fn();
const projectDispatch = vi.fn();

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    state: projectFake.state,
    tabs: projectFake.tabs,
    activeTabId: projectFake.activeTabId,
    openSessionTab,
    closeSessionTab,
    openNewSessionTab,
    toggleSessionPicker,
    dispatch: projectDispatch,
  }),
}));

function renderBar(focusedKind: "chat" | "terminal" = "chat") {
  const onFocusKindChange = vi.fn();
  const utils = render(
    <ChatProvider>
      <TerminalProvider>
        <UnifiedTabBar focusedKind={focusedKind} onFocusKindChange={onFocusKindChange} />
      </TerminalProvider>
    </ChatProvider>,
  );
  return { onFocusKindChange, ...utils };
}

beforeEach(() => {
  window.localStorage.clear();
  openSessionTab.mockClear();
  closeSessionTab.mockClear();
  openNewSessionTab.mockClear();
  toggleSessionPicker.mockClear();
  projectDispatch.mockClear();
  projectFake = {
    state: { activeProject: { path: "/proj", name: "proj" } },
    tabs: [{ id: "s1", projectPath: "/proj", title: "Chat One", activeSubTab: "chat" }],
    activeTabId: "s1",
  };
});

describe("UnifiedTabBar", () => {
  it("renders a chat pill with the chat emoji", () => {
    renderBar();
    expect(screen.getByText("Chat One")).toBeTruthy();
  });

  it("lists a persisted-but-never-activated terminal as a pill (peek, no pty)", () => {
    window.localStorage.setItem(
      "ocode.ui.terminals.project.v1",
      JSON.stringify({ version: 1, projects: { "/proj": { terminals: [{ id: "term-1-1", title: "Terminal 1" }], activeId: "term-1-1" } } }),
    );
    renderBar();
    expect(screen.getByText("Terminal 1")).toBeTruthy();
  });

  it("💬+ creates a new chat session and switches focus to chat", () => {
    const { onFocusKindChange } = renderBar("terminal");
    fireEvent.click(screen.getByLabelText("New chat session"));
    expect(openNewSessionTab).toHaveBeenCalledTimes(1);
    expect(onFocusKindChange).toHaveBeenCalledWith("chat");
  });

  it("⌨️+ creates a new terminal (visible as a pill) and switches focus to terminal", () => {
    // Terminal titles come from a module-level counter shared across this
    // whole test file (see terminalStore.tsx's bumpSeqPast) — assert a
    // "Terminal N" pill appeared, not a specific number.
    const { onFocusKindChange } = renderBar("chat");
    fireEvent.click(screen.getByLabelText("New terminal"));
    expect(onFocusKindChange).toHaveBeenCalledWith("terminal");
    expect(screen.getByText(/^Terminal \d+$/)).toBeTruthy();
  });

  it("clicking a peeked terminal pill switches focus to terminal", () => {
    window.localStorage.setItem(
      "ocode.ui.terminals.project.v1",
      JSON.stringify({ version: 1, projects: { "/proj": { terminals: [{ id: "term-1-1", title: "Terminal 1" }], activeId: "term-1-1" } } }),
    );
    const { onFocusKindChange } = renderBar("chat");
    fireEvent.click(screen.getByText("Terminal 1"));
    expect(onFocusKindChange).toHaveBeenCalledWith("terminal");
  });

  it("respects a previously saved merged tab order on render", () => {
    window.localStorage.setItem(
      "ocode.ui.terminals.project.v1",
      JSON.stringify({ version: 1, projects: { "/proj": { terminals: [{ id: "term-1-1", title: "Terminal 1" }], activeId: "term-1-1" } } }),
    );
    window.localStorage.setItem(
      "ocode.ui.tabOrder.v1",
      JSON.stringify({ version: 1, projects: { "/proj": ["term:term-1-1", "chat:s1"] } }),
    );
    renderBar();
    const labels = screen.getAllByText(/Chat One|Terminal 1/).map((el) => el.textContent);
    expect(labels).toEqual(["Terminal 1", "Chat One"]);
  });

  it("shows the Processes pinned tab and All sessions button", () => {
    renderBar();
    expect(screen.getByText("Processes")).toBeTruthy();
    expect(screen.getByText("All sessions")).toBeTruthy();
  });
});
```

- [ ] **Step 3: Delete `OpenSessionBar.tsx`**

```bash
git rm web/src/components/Layout/OpenSessionBar.tsx
```

- [ ] **Step 4: Run the new tests**

Run: `pnpm exec vitest run src/components/Layout/UnifiedTabBar.test.tsx`
Expected: all 7 tests PASS.

Note: `App.tsx` still imports the now-deleted `OpenSessionBar` at this point in the plan — that's fixed in Task 6. `pnpm build`/`tsc` will fail until then; don't run it yet.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/Layout/UnifiedTabBar.tsx web/src/components/Layout/UnifiedTabBar.test.tsx
git commit -m "feat: add UnifiedTabBar merging chat and terminal tabs into one row"
```

---

### Task 5: Merge the top-level tab in `TopTabs.tsx`

**Files:**
- Modify: `web/src/components/Layout/TopTabs.tsx`

**Interfaces:**
- No exported signature changes (`Props { activeTab: string; onTabSelect: (value: string) => void }` unchanged) — `App.tsx` needs no prop-level changes for this file.

- [ ] **Step 1: Drop the `terminal` icon import and `mainTabs` entry**

In `web/src/components/Layout/TopTabs.tsx`, change:

```ts
import { FolderGit2, GitBranch, Paperclip, CalendarClock, MessageSquare, MoreHorizontal, Settings, Terminal } from "lucide-react";
```

to:

```ts
import { FolderGit2, GitBranch, Paperclip, CalendarClock, MessageSquare, MoreHorizontal, Settings } from "lucide-react";
```

and change:

```ts
const mainTabs = [
  { id: "sessions", label: "Sessions", icon: MessageSquare },
  { id: "terminal", label: "Terminal", icon: Terminal },
  { id: "files", label: "Files", icon: FolderGit2 },
  { id: "git", label: "Git", icon: GitBranch },
  { id: "cron", label: "Cron", icon: CalendarClock },
  { id: "assets", label: "Assets", icon: Paperclip },
  { id: "settings", label: "Settings", icon: Settings },
];
```

to:

```ts
const mainTabs = [
  { id: "sessions", label: "Sessions", icon: MessageSquare },
  { id: "files", label: "Files", icon: FolderGit2 },
  { id: "git", label: "Git", icon: GitBranch },
  { id: "cron", label: "Cron", icon: CalendarClock },
  { id: "assets", label: "Assets", icon: Paperclip },
  { id: "settings", label: "Settings", icon: Settings },
];
```

- [ ] **Step 2: Combine the badge count**

Both occurrences of this line (main tab strip render and the overflow "More" menu render) change identically — use a single `replace_all` edit:

```ts
const count = tab.id === "terminal" ? terminalCount : tab.id === "sessions" ? sessionsCount : undefined;
```

to:

```ts
const count = tab.id === "sessions" ? sessionsCount + terminalCount : undefined;
```

- [ ] **Step 3: Typecheck this file compiles standalone**

Run: `pnpm exec tsc --noEmit -p . 2>&1 | grep TopTabs || echo "no TopTabs errors"`
Expected: `no TopTabs errors` (other pre-existing cross-file errors from Task 4's `OpenSessionBar` deletion, fixed in Task 6, are expected and not this file's concern).

- [ ] **Step 4: Commit**

```bash
git add web/src/components/Layout/TopTabs.tsx
git commit -m "refactor: merge Terminal into the Sessions top-level tab"
```

---

### Task 6: Wire `App.tsx`

**Files:**
- Modify: `web/src/App.tsx`

- [ ] **Step 1: Swap the `OpenSessionBar` import for `UnifiedTabBar`, add `TerminalProvider`**

Change:

```ts
import OpenSessionBar from "./components/Layout/OpenSessionBar";
```

to:

```ts
import UnifiedTabBar from "./components/Layout/UnifiedTabBar";
```

Change:

```ts
import { ProjectProvider, findProjectPathForTab, useProjectState } from "./stores/projectStore";
```

to:

```ts
import { ProjectProvider, findProjectPathForTab, useProjectState } from "./stores/projectStore";
import { TerminalProvider } from "./stores/terminalStore";
```

- [ ] **Step 2: Drop `"terminal"` from the `activeView` union and add `focusedKind` state**

Change:

```ts
  const [activeView, setActiveView] = useState<
    "files" | "git" | "cron" | "assets" | "sessions" | "settings" | "terminal"
  >("sessions");
```

to:

```ts
  const [activeView, setActiveView] = useState<
    "files" | "git" | "cron" | "assets" | "sessions" | "settings"
  >("sessions");
  // Which half of the merged Sessions tab (chat vs terminal) is currently
  // shown. Resets to chat on project switch — no per-project memory (v1).
  const [focusedKind, setFocusedKind] = useState<"chat" | "terminal">("chat");
  useEffect(() => {
    setFocusedKind("chat");
  }, [projectState.activeProject?.path]);
```

- [ ] **Step 3: Update the Cmd/Ctrl+T handler**

Change:

```ts
    onNewTerminal: () => {
      // Ctrl/Cmd+T: if on terminal top-level tab, open a new terminal instance;
      // otherwise create a new session tab (same as Ctrl/Cmd+N). Terminal is
      // project-scoped so the handle is keyed by project path.
      if (activeView === "terminal") {
        const proj = projectState.activeProject?.path ?? "";
        terminalRefs.current.get(proj)?.openTerminal();
      } else {
        openNewSessionTab(isNewSessionTabEmpty(activeTabId));
      }
    },
```

to:

```ts
    onNewTerminal: () => {
      // Ctrl/Cmd+T: on the merged sessions tab, always opens a new terminal
      // for the active project (terminal pills are reachable there
      // regardless of whether a chat or terminal tab currently has focus);
      // elsewhere it creates a new chat session (same as Ctrl/Cmd+N).
      // Terminal is project-scoped so the handle is keyed by project path.
      if (activeView === "sessions") {
        setFocusedKind("terminal");
        const proj = projectState.activeProject?.path ?? "";
        terminalRefs.current.get(proj)?.openTerminal();
      } else {
        openNewSessionTab(isNewSessionTabEmpty(activeTabId));
      }
    },
```

- [ ] **Step 4: Update the Cmd/Ctrl+W handler**

Change:

```ts
    onCloseSession: () => {
      // Cmd/Ctrl+W: close whatever is frontmost. On the Files view that is
      // the active editor tab; on the terminal top-level tab it is the active
      // terminal instance; otherwise the session tab itself. Mirrors each tab bar's
      // X button.
      if (activeView === "terminal") {
        const proj = projectState.activeProject?.path ?? "";
        if (terminalRefs.current.get(proj)?.closeActiveTerminal()) return;
        return;
      }
      if (activeView === "files") {
        if (activeEditorTabId) {
          requestCloseTab(activeEditorTabId);
        }
        return;
      }
      if (activeView !== "sessions" || !activeTabId) return;
      closeSessionTab(activeTabId);
      cancelLiveDeltas(activeTabId);
      clearQueue(activeTabId);
      dispatch({ type: "RESET", sessionId: activeTabId });
    },
```

to:

```ts
    onCloseSession: () => {
      // Cmd/Ctrl+W: close whatever is frontmost. On the Files view that is
      // the active editor tab; on the merged sessions tab it is the active
      // terminal instance or the active chat tab, depending on which
      // currently has focus. Mirrors each tab bar's X button.
      if (activeView === "sessions" && focusedKind === "terminal") {
        const proj = projectState.activeProject?.path ?? "";
        if (terminalRefs.current.get(proj)?.closeActiveTerminal()) return;
        return;
      }
      if (activeView === "files") {
        if (activeEditorTabId) {
          requestCloseTab(activeEditorTabId);
        }
        return;
      }
      if (activeView !== "sessions" || focusedKind !== "chat" || !activeTabId) return;
      closeSessionTab(activeTabId);
      cancelLiveDeltas(activeTabId);
      clearQueue(activeTabId);
      dispatch({ type: "RESET", sessionId: activeTabId });
    },
```

- [ ] **Step 5: Update the terminal content block's visibility**

Change:

```tsx
                return (
                  <div className={activeView === "terminal" ? "flex flex-1 overflow-hidden m-0 flex-col" : "hidden"}>
                    <div className="relative flex-1 min-h-0 overflow-hidden">
                      {projectPaths.map((pp) => (
                        <div
                          key={`${pp}:terminal`}
                          className={
                            pp === activeProjectPath ? "absolute inset-0" : "absolute inset-0 hidden"
                          }
                        >
                          <TerminalTabs
                            ref={(handle) => {
                              if (handle) terminalRefs.current.set(pp, handle);
                              else terminalRefs.current.delete(pp);
                            }}
                            active={pp === activeProjectPath && activeView === "terminal"}
                            projectPath={pp}
                          />
                        </div>
                      ))}
                    </div>
                  </div>
                );
              })()}

              <div className={activeView === "terminal" ? "hidden" : "flex flex-1 overflow-hidden flex-col"}>
```

to:

```tsx
                const terminalFocused = activeView === "sessions" && focusedKind === "terminal";
                return (
                  <div className={terminalFocused ? "flex flex-1 overflow-hidden m-0 flex-col" : "hidden"}>
                    <div className="relative flex-1 min-h-0 overflow-hidden">
                      {projectPaths.map((pp) => (
                        <div
                          key={`${pp}:terminal`}
                          className={
                            pp === activeProjectPath ? "absolute inset-0" : "absolute inset-0 hidden"
                          }
                        >
                          <TerminalTabs
                            ref={(handle) => {
                              if (handle) terminalRefs.current.set(pp, handle);
                              else terminalRefs.current.delete(pp);
                            }}
                            active={pp === activeProjectPath && terminalFocused}
                            projectPath={pp}
                          />
                        </div>
                      ))}
                    </div>
                  </div>
                );
              })()}

              <div className={activeView === "sessions" && focusedKind === "terminal" ? "hidden" : "flex flex-1 overflow-hidden flex-col"}>
```

- [ ] **Step 6: Replace `OpenSessionBar` with `UnifiedTabBar` and gate chat content on `focusedKind`**

Change:

```tsx
              <TabsContent value="sessions" forceMount className="flex-1 overflow-hidden m-0">
                <div className="flex flex-col h-full">
                  <OpenSessionBar />
                  <SessionSubTabs />
                  <div className="relative flex-1 min-h-0 overflow-hidden">
                    {tabs.length === 0 && (
```

to:

```tsx
              <TabsContent value="sessions" forceMount className="flex-1 overflow-hidden m-0">
                <div className="flex flex-col h-full">
                  <UnifiedTabBar focusedKind={focusedKind} onFocusKindChange={setFocusedKind} />
                  <div className={focusedKind === "chat" ? "flex flex-col flex-1 min-h-0" : "hidden"}>
                  <SessionSubTabs />
                  <div className="relative flex-1 min-h-0 overflow-hidden">
                    {tabs.length === 0 && (
```

Then, further down, the block ending the `sessions` `TabsContent` (immediately after the `status` sub-tab's `allChatTabs.map(...)` — anchor on the `StatusPanel` line so this match is unique):

```tsx
                          <StatusPanel onClose={() => projectDispatch({ type: "SET_TAB_SUB_TAB", id: tab.id, subTab: "chat" })} />
                        </div>
                      );
                    })}
                  </div>
                </div>
              </TabsContent>
```

change to (one added `</div>` closing the new `focusedKind === "chat"` wrapper from the edit above):

```tsx
                          <StatusPanel onClose={() => projectDispatch({ type: "SET_TAB_SUB_TAB", id: tab.id, subTab: "chat" })} />
                        </div>
                      );
                    })}
                  </div>
                  </div>
                </div>
              </TabsContent>
```

- [ ] **Step 7: Wrap `HomeApp`/`SessionPage` in `TerminalProvider`**

Change:

```tsx
export default function App() {
  // Applies the server (terminal) theme to the CSS variables once on load.
  useTheme();
  return (
    <ErrorBoundary>
      <ChatProvider>
        <ProjectProvider>
          <FrontendMemoryReporter />
          <StatusMetricsHydrator />
          <Routes>
            <Route path="/session/:id" element={<SessionPage />} />
            <Route path="*" element={<HomeApp />} />
          </Routes>
        </ProjectProvider>
      </ChatProvider>
    </ErrorBoundary>
  );
```

to:

```tsx
export default function App() {
  // Applies the server (terminal) theme to the CSS variables once on load.
  useTheme();
  return (
    <ErrorBoundary>
      <ChatProvider>
        <ProjectProvider>
          <TerminalProvider>
            <FrontendMemoryReporter />
            <StatusMetricsHydrator />
            <Routes>
              <Route path="/session/:id" element={<SessionPage />} />
              <Route path="*" element={<HomeApp />} />
            </Routes>
          </TerminalProvider>
        </ProjectProvider>
      </ChatProvider>
    </ErrorBoundary>
  );
```

- [ ] **Step 8: Typecheck and run the full web test suite**

Run: `cd web && pnpm exec tsc --noEmit`
Expected: no errors.

Run: `cd web && pnpm test`
Expected: all tests PASS (including the new/modified ones from Tasks 1–5).

- [ ] **Step 9: Commit**

```bash
git add web/src/App.tsx
git commit -m "feat: wire the merged sessions+terminal tab bar into App.tsx"
```

---

### Task 7: Manual QA pass

Per this project's UI-testing convention (`AGENTS.md`), verify the merged bar in a real browser before calling this done — automated tests don't cover visual layout, drag gestures, or true pty behavior over a real WebSocket.

**Files:** none (verification only).

- [ ] **Step 1: Start the dev server**

Run: `cd web && pnpm dev` (leave running)

- [ ] **Step 2: Walk through the golden path**

In a browser, open the app against a running `ocode` server and, for one project:
1. Confirm the top bar shows one "Sessions" tab (no separate "Terminal" tab), badge = chat count + terminal count.
2. Confirm the merged row shows 💬-prefixed chat pills and (if any terminals were previously open for this project) ⌨️-prefixed terminal pills.
3. Click 💬+ — a new chat pill appears and becomes active; chat content shows.
4. Click ⌨️+ — a new terminal pill appears and becomes active; a real shell prompt appears (confirms the pty actually spawned, not just UI state).
5. Drag-reorder a terminal pill before a chat pill; reload the page — the order persists.
6. Click the Processes pinned tab — process telemetry shows; it stays pinned at the end after reordering other tabs.
7. Double-click a chat pill and a terminal pill to rename each; confirm both persist (terminal rename survives reload; chat rename calls the rename API without erroring).
8. With a terminal focused, press Cmd/Ctrl+W — the active terminal closes (not the chat tab). Switch focus to a chat pill, press Cmd/Ctrl+W again — the chat tab closes.
9. Press Cmd/Ctrl+T while on the Sessions tab — a new terminal opens and gets focus, regardless of whether a chat or terminal pill was previously active.
10. Switch to a different open project, then back — confirm no terminal ptys were spawned for the project you didn't visit (check server-side process list or the "No terminals open" empty state if that project never had one activated), and the original project's terminal(s) are still running (scrollback intact).

- [ ] **Step 2: Report results**

If any step fails, stop and report which one before considering this plan complete — do not mark Task 7 done on an assumption.

---

## Self-Review Notes

- **Spec coverage:** §1 (merged row, emoji, two `+` buttons, reorder, pinned Processes, combined badge) → Tasks 4–5. §3 (terminalStore, content-only TerminalTabs, App.tsx wiring) → Tasks 1, 3, 6. §4 (activeTabId untouched, focusedKind, order persistence, peek/live split) → Tasks 1, 2, 6. §5 (click routing, `+` buttons, Processes, close, drag, keyboard shortcuts) → Tasks 4, 6. §6 (testing) → every task's own test steps plus Task 7 manual QA.
- **Placeholder scan:** no TBD/TODO markers; every step has literal code or an exact run command.
- **Type consistency:** `TerminalInstance`, `getProjectTerminals`, `useTerminalState`, `PROCESSES_TAB_ID`, `UnifiedTabKey` are defined once (Tasks 1–2) and referenced with identical names/shapes in Tasks 3, 4, 6 — no renames across tasks.
