# Part 10 — UnifiedTabBar Browser Kind + App.tsx Wiring + Keyboard + Side Panel

**Goal:** Make "browser" a first-class tab kind next to chat sessions and terminals, wire the full-width browser tab and the resizable/collapsible side panel into `App.tsx`, and extend `Cmd/Ctrl+W` to close a focused browser tab. This is the final integration part: after it, the feature is reachable in the UI.

**Prerequisites (from earlier parts — consumed here):**
- Part 08 `web/src/lib/browserStore.ts` exports `useBrowserStore` with actions `open(key)`, `close(key)`, `setCollapsed(key, bool)`, and selectors; `StateKey` is `` `side:${"chat"|"term"}:${string}` `` | `` `tab:${string}` ``.
- Part 09 `web/src/components/Browser/BrowserPanel.tsx` default-exports `BrowserPanel` with props `{ stateKey: string; mode: "side" | "full" }`. It internally gates the live iframe to only the active/visible panel; App.tsx only controls CSS visibility.

**Files:**
- Modify: `web/src/components/Layout/tabOrderPersistence.ts`
- Modify: `web/src/components/Layout/tabOrderPersistence.test.ts`
- Create: `web/src/stores/browserTabsStore.tsx`
- Create: `web/src/stores/browserTabsStore.test.tsx`
- Modify: `web/src/lib/viewPersistence.ts` (extend `FocusedKind`)
- Modify: `web/src/components/Layout/UnifiedTabBar.tsx`
- Modify: `web/src/components/Layout/UnifiedTabBar.test.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/hooks/useKeyboard.ts`
- Modify: `TODO.md`

**Interfaces:**
- Consumes: `useBrowserStore` (Part 08), `BrowserPanel` (Part 09), existing `useResizableSidebar` hook.
- Produces: `browserTabsStore` (`useBrowserTabs`), `FocusedKind` now includes `"browser"`, `reconcileTabOrder(saved, chatIds, terminalIds, browserIds)`.

---

## Task 10a: Extend tab-order persistence with a `browser:` kind

**Verified current state:** `web/src/components/Layout/tabOrderPersistence.ts` defines `export type UnifiedTabKey = \`chat:${string}\` | \`term:${string}\`` and `reconcileTabOrder(saved, chatIds, terminalIds)`. Its only caller is `UnifiedTabBar.tsx:284,289` (via `loadTabOrder`). Old persisted arrays under `localStorage["ocode.ui.tabOrder.v1"]` contain only `chat:`/`term:` keys; the reconcile already drops unknown keys and appends missing live keys, so adding `browser:` is backward-compatible with no version bump (a v1 file simply has browser tabs appended on first reconcile).

- [ ] **Step 1: Write the failing test**

Add to `web/src/components/Layout/tabOrderPersistence.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { reconcileTabOrder, type UnifiedTabKey } from "./tabOrderPersistence";

describe("reconcileTabOrder with browser tabs", () => {
  it("keeps saved interleaving of chat, term, and browser keys", () => {
    const saved: UnifiedTabKey[] = ["term:t1", "browser:b1", "chat:c1"];
    const out = reconcileTabOrder(saved, ["c1"], ["t1"], ["b1"]);
    expect(out).toEqual(["term:t1", "browser:b1", "chat:c1"]);
  });

  it("appends new browser ids missing from a legacy saved order", () => {
    const saved: UnifiedTabKey[] = ["chat:c1"]; // legacy file, no browser keys
    const out = reconcileTabOrder(saved, ["c1"], [], ["b1", "b2"]);
    expect(out).toEqual(["chat:c1", "browser:b1", "browser:b2"]);
  });

  it("drops stale browser keys whose id is gone", () => {
    const saved: UnifiedTabKey[] = ["browser:gone", "chat:c1"];
    const out = reconcileTabOrder(saved, ["c1"], [], []);
    expect(out).toEqual(["chat:c1"]);
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/components/Layout/tabOrderPersistence.test.ts`
Expected: FAIL — `reconcileTabOrder` takes 3 args / `browser:` not assignable to `UnifiedTabKey`.

- [ ] **Step 3: Implement**

In `web/src/components/Layout/tabOrderPersistence.ts`, change the type and the reconcile signature:

```ts
export type UnifiedTabKey = `chat:${string}` | `term:${string}` | `browser:${string}`;
```

```ts
/** Reconciles a saved order against the live id sets: drops stale keys, and
 *  appends any live id missing from the saved order (new tabs created since
 *  last save) at the end, in the order they appear in
 *  chatIds/terminalIds/browserIds. Legacy saved orders (pre-browser) reconcile
 *  cleanly — unknown/absent kinds are simply appended, so no version bump. */
export function reconcileTabOrder(
  saved: UnifiedTabKey[],
  chatIds: string[],
  terminalIds: string[],
  browserIds: string[],
): UnifiedTabKey[] {
  const liveKeys: UnifiedTabKey[] = [
    ...chatIds.map((id): UnifiedTabKey => `chat:${id}`),
    ...terminalIds.map((id): UnifiedTabKey => `term:${id}`),
    ...browserIds.map((id): UnifiedTabKey => `browser:${id}`),
  ];
  const liveKeySet = new Set(liveKeys);
  const kept = saved.filter((k) => liveKeySet.has(k));
  const keptSet = new Set(kept);
  const appended = liveKeys.filter((k) => !keptSet.has(k));
  return [...kept, ...appended];
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd web && npx vitest run src/components/Layout/tabOrderPersistence.test.ts`
Expected: PASS. (The two existing 3-arg call sites in `UnifiedTabBar.tsx` will now fail typecheck — fixed in Task 10c; that is expected here.)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/Layout/tabOrderPersistence.ts web/src/components/Layout/tabOrderPersistence.test.ts
git commit -m "feat(browser): add browser: kind to unified tab order reconcile"
```

---

## Task 10b: Add a minimal `browserTabsStore`

**Rationale:** chat tabs live in the `projectStore` reducer (`web/src/stores/projectStore.tsx`, `Tab` interface, `ADD_TAB`/`REMOVE_TAB` actions) and terminals in `terminalStore`. A browser *tab* only needs `{ id, title }` per project plus an active-id pointer — far less than a chat session. Rather than thread a new entity through the large projectStore reducer, add a small companion context store that mirrors the projectStore shape (per-project map + active pointer). Live browser page state (URL, history, console) is owned by Part 08's `useBrowserStore`, keyed `tab:{id}`; this store owns only the tab-strip identity.

- [ ] **Step 1: Write the failing test**

Create `web/src/stores/browserTabsStore.test.tsx`:

```tsx
import { describe, it, expect } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { BrowserTabsProvider, useBrowserTabs } from "./browserTabsStore";

function wrap({ children }: { children: React.ReactNode }) {
  return <BrowserTabsProvider>{children}</BrowserTabsProvider>;
}

describe("browserTabsStore", () => {
  it("opens, lists, renames, and closes browser tabs per project", () => {
    const { result } = renderHook(() => useBrowserTabs("/proj/a"), { wrapper: wrap });

    let id = "";
    act(() => { id = result.current.openBrowserTab(); });
    expect(result.current.tabs).toHaveLength(1);
    expect(result.current.activeId).toBe(id);
    expect(result.current.tabs[0].title).toBe("New tab");

    act(() => { result.current.renameBrowserTab(id, "example.com"); });
    expect(result.current.tabs[0].title).toBe("example.com");

    act(() => { result.current.closeBrowserTab(id); });
    expect(result.current.tabs).toHaveLength(0);
    expect(result.current.activeId).toBeNull();
  });

  it("isolates tabs by project path", () => {
    const a = renderHook(() => useBrowserTabs("/proj/a"), { wrapper: wrap });
    act(() => { a.result.current.openBrowserTab(); });
    const b = renderHook(() => useBrowserTabs("/proj/b"), { wrapper: wrap });
    expect(b.result.current.tabs).toHaveLength(0);
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/stores/browserTabsStore.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement**

Create `web/src/stores/browserTabsStore.tsx`:

```tsx
import { createContext, useContext, useReducer, useCallback, useMemo, type ReactNode } from "react";

export interface BrowserTab {
  id: string;
  title: string;
}

interface State {
  tabsByProject: Record<string, BrowserTab[]>;
  activeByProject: Record<string, string | null>;
}

type Action =
  | { type: "OPEN"; project: string; id: string }
  | { type: "CLOSE"; project: string; id: string }
  | { type: "RENAME"; project: string; id: string; title: string }
  | { type: "ACTIVATE"; project: string; id: string };

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case "OPEN": {
      const tabs = state.tabsByProject[action.project] ?? [];
      return {
        tabsByProject: { ...state.tabsByProject, [action.project]: [...tabs, { id: action.id, title: "New tab" }] },
        activeByProject: { ...state.activeByProject, [action.project]: action.id },
      };
    }
    case "CLOSE": {
      const tabs = (state.tabsByProject[action.project] ?? []).filter((t) => t.id !== action.id);
      const wasActive = state.activeByProject[action.project] === action.id;
      const nextActive = wasActive ? (tabs.length ? tabs[tabs.length - 1].id : null) : state.activeByProject[action.project] ?? null;
      return {
        tabsByProject: { ...state.tabsByProject, [action.project]: tabs },
        activeByProject: { ...state.activeByProject, [action.project]: nextActive },
      };
    }
    case "RENAME": {
      const tabs = (state.tabsByProject[action.project] ?? []).map((t) =>
        t.id === action.id ? { ...t, title: action.title } : t,
      );
      return { ...state, tabsByProject: { ...state.tabsByProject, [action.project]: tabs } };
    }
    case "ACTIVATE":
      return { ...state, activeByProject: { ...state.activeByProject, [action.project]: action.id } };
    default:
      return state;
  }
}

interface Ctx {
  state: State;
  dispatch: React.Dispatch<Action>;
}
const BrowserTabsContext = createContext<Ctx | null>(null);

export function BrowserTabsProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, { tabsByProject: {}, activeByProject: {} });
  const value = useMemo(() => ({ state, dispatch }), [state]);
  return <BrowserTabsContext.Provider value={value}>{children}</BrowserTabsContext.Provider>;
}

let seq = 0;
function newId(): string {
  seq += 1;
  return `b${Date.now().toString(36)}${seq}`;
}

export function useBrowserTabs(projectPath: string) {
  const ctx = useContext(BrowserTabsContext);
  if (!ctx) throw new Error("useBrowserTabs must be used within BrowserTabsProvider");
  const { state, dispatch } = ctx;

  const tabs = state.tabsByProject[projectPath] ?? [];
  const activeId = state.activeByProject[projectPath] ?? null;

  const openBrowserTab = useCallback(() => {
    const id = newId();
    dispatch({ type: "OPEN", project: projectPath, id });
    return id;
  }, [dispatch, projectPath]);

  const closeBrowserTab = useCallback((id: string) => dispatch({ type: "CLOSE", project: projectPath, id }), [dispatch, projectPath]);
  const renameBrowserTab = useCallback((id: string, title: string) => dispatch({ type: "RENAME", project: projectPath, id, title }), [dispatch, projectPath]);
  const activateBrowserTab = useCallback((id: string) => dispatch({ type: "ACTIVATE", project: projectPath, id }), [dispatch, projectPath]);

  return { tabs, activeId, openBrowserTab, closeBrowserTab, renameBrowserTab, activateBrowserTab };
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd web && npx vitest run src/stores/browserTabsStore.test.tsx`
Expected: PASS.

- [ ] **Step 5: Mount the provider**

In `web/src/App.tsx`, wrap the existing provider tree (find where `ProjectProvider`/`TerminalProvider` are composed — the app root or `main.tsx`) so `BrowserTabsProvider` sits alongside them. Add the import `import { BrowserTabsProvider } from "./stores/browserTabsStore";` and place `<BrowserTabsProvider>` around the same subtree the other stores wrap. (If providers are composed in `web/src/main.tsx`, add it there instead — match the existing composition site.)

- [ ] **Step 6: Commit**

```bash
git add web/src/stores/browserTabsStore.tsx web/src/stores/browserTabsStore.test.tsx web/src/App.tsx
git commit -m "feat(browser): browserTabsStore for browser tab strip identity"
```

---

## Task 10c: Extend `FocusedKind` and render browser pills in `UnifiedTabBar`

**Verified current state:** `web/src/lib/viewPersistence.ts:2` `export type FocusedKind = "chat" | "terminal"` with a `validKinds` Set used by `isValidKind`. `UnifiedTabBar.tsx:223-227` types props `focusedKind: "chat" | "terminal"` and calls `reconcileTabOrder(loadTabOrder(activeProjectPath), chatIds, terminalIds)` at :284,289. Chat add button calls `openNewSessionTab` + `onFocusKindChange("chat")` (:399), terminal add calls `openTerminal` + `onFocusKindChange("terminal")` (:404).

- [ ] **Step 1: Write the failing test**

Add to `web/src/components/Layout/UnifiedTabBar.test.tsx` (mirror the existing render harness/providers used by the other tests in that file — reuse its `renderWithProviders` helper if present, and wrap with `BrowserTabsProvider`):

```tsx
it("renders a Browser add button and opens a browser pill", async () => {
  const onFocusKindChange = vi.fn();
  renderUnifiedTabBar({ focusedKind: "chat", onFocusKindChange }); // existing helper
  const addBrowser = screen.getByRole("button", { name: /new browser tab/i });
  await userEvent.click(addBrowser);
  expect(onFocusKindChange).toHaveBeenCalledWith("browser");
  expect(screen.getByRole("tab", { name: /new tab/i })).toBeInTheDocument();
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/components/Layout/UnifiedTabBar.test.tsx`
Expected: FAIL — no "new browser tab" button; type error on `"browser"`.

- [ ] **Step 3: Extend `FocusedKind`**

In `web/src/lib/viewPersistence.ts`:

```ts
export type FocusedKind = "chat" | "terminal" | "browser";
```

and add `"browser"` to the `validKinds` set:

```ts
const validKinds: Set<FocusedKind> = new Set(["chat", "terminal", "browser"]);
```

- [ ] **Step 4: Implement in `UnifiedTabBar.tsx`**

1. Widen the prop types (:223-224) to the shared type — import and use `FocusedKind`:

```ts
import type { FocusedKind } from "../../lib/viewPersistence";
// ...
  focusedKind: FocusedKind;
  onFocusKindChange: (kind: FocusedKind) => void;
```

2. Pull browser tabs and update the order source. Near the terminal ids (`const terminalIds = useMemo(...)` ~:275):

```ts
import { useBrowserTabs } from "../../stores/browserTabsStore";
// inside the component:
const { tabs: browserTabs, activeId: activeBrowserId, openBrowserTab, closeBrowserTab, renameBrowserTab, activateBrowserTab } = useBrowserTabs(activeProjectPath);
const browserIds = useMemo(() => browserTabs.map((t) => t.id), [browserTabs]);
```

3. Update BOTH `reconcileTabOrder` calls (:284, :289) to pass `browserIds`:

```ts
reconcileTabOrder(loadTabOrder(activeProjectPath), chatIds, terminalIds, browserIds)
```

(and add `browserIds` to that `useMemo`/effect dependency array).

4. Render browser pills. Where the ordered keys are mapped to `TabPill`s, add a branch for keys starting `browser:` producing a pill with a globe indicator (reuse the existing `TabPill` component — pass an emoji/icon prop consistent with how terminal/chat pills set theirs; if `TabPill` has no icon prop, add an optional `icon?: ReactNode` prop defaulting to the current behavior). Wire:
   - click → `activateBrowserTab(id)` + `onFocusKindChange("browser")`
   - `isActive={focusedKind === "browser" && activeBrowserId === id}`
   - close (X) → `closeBrowserTab(id)`
   - rename (inline edit commit) → `renameBrowserTab(id, value)`

5. Add the add-control next to the existing new-session / new-terminal buttons:

```tsx
<button
  type="button"
  aria-label="New browser tab"
  title="New browser tab"
  className={/* match the sibling add buttons' classes */}
  onClick={() => { openBrowserTab(); onFocusKindChange("browser"); }}
>
  {/* globe glyph, matching sibling icon sizing */}
  🌐
</button>
```

- [ ] **Step 5: Run to verify it passes**

Run: `cd web && npx vitest run src/components/Layout/UnifiedTabBar.test.tsx src/components/Layout/UnifiedTabBar.drag.test.tsx`
Expected: PASS (drag test still green — browser keys participate in the same ordered list). If the drag test enumerates expected keys, update its fixture to include browser ids.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/viewPersistence.ts web/src/components/Layout/UnifiedTabBar.tsx web/src/components/Layout/UnifiedTabBar.test.tsx
git commit -m "feat(browser): browser tab kind + add control in UnifiedTabBar"
```

---

## Task 10d: Render the full-width browser tab in `App.tsx`

**Verified current state:** `App.tsx:157` derives `focusedKind` (default `"chat"`), `:173` `setFocusedKind`, `:713` renders `<UnifiedTabBar focusedKind={focusedKind} onFocusKindChange={setFocusedKind} />`. Center region uses CSS-visibility (`className={... ? "hidden" : ...}`) at :755, :839 to keep chat/terminal mounted-hidden. The terminal region is toggled at :729,:755.

- [ ] **Step 1: Write the failing test**

Add `web/src/App.browser.test.tsx` (mock the heavy children — mirror any existing App test's mocks; if none, mock `BrowserPanel`, `ChatPanel`, `TerminalTabs`, `ProjectSidebar` to trivial stubs):

```tsx
import { describe, it, expect, vi } from "vitest";
vi.mock("./components/Browser/BrowserPanel", () => ({
  default: ({ stateKey, mode }: { stateKey: string; mode: string }) => (
    <div data-testid="browser-panel" data-key={stateKey} data-mode={mode} />
  ),
}));
// ...mock other heavy children as the existing suite does...

// Render App, drive it to focusedKind "browser" (open a browser tab via the
// UnifiedTabBar's "New browser tab" button), then assert a full-mode panel shows.
it("shows a full-width BrowserPanel when a browser tab is focused", async () => {
  renderApp(); // existing helper or a local wrapper with all providers
  await userEvent.click(await screen.findByRole("button", { name: /new browser tab/i }));
  const panel = await screen.findByTestId("browser-panel");
  expect(panel).toHaveAttribute("data-mode", "full");
  expect(panel.getAttribute("data-key")).toMatch(/^tab:/);
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/App.browser.test.tsx`
Expected: FAIL — no browser-panel testid (App does not render it yet).

- [ ] **Step 3: Implement**

In `App.tsx`:

1. Import: `import BrowserPanel from "./components/Browser/BrowserPanel";` and `import { useBrowserTabs } from "./stores/browserTabsStore";`. Read the active browser tab: `const { activeId: activeBrowserId } = useBrowserTabs(activeProjectPath);` (use the same `activeProjectPath` the rest of App uses).

2. In the center `<main>` region (the sessions/terminal container around :755–:870), add a sibling block for the browser view, mounted-hidden by CSS like the others:

```tsx
<div className={activeView === "sessions" && focusedKind === "browser" ? "flex flex-1 overflow-hidden" : "hidden"}>
  {activeBrowserId && (
    <BrowserPanel stateKey={`tab:${activeBrowserId}`} mode="full" />
  )}
</div>
```

3. Ensure the existing chat and terminal regions also hide when `focusedKind === "browser"`. They currently test `focusedKind === "terminal"` / `=== "chat"`; since browser is a third value, verify each visibility expression hides its region when browser is focused. Concretely, the terminal block (:755) `focusedKind === "terminal" ? "hidden" : ...` must become `focusedKind === "chat" ? <shown> : "hidden"`-style so it is hidden for both terminal and browser — audit :729, :755, :839 and make each region visible only for its own kind. Simplest safe rewrite: compute once near :157:

```ts
const showChat = activeView === "sessions" && focusedKind === "chat";
const showTerminal = activeView === "sessions" && focusedKind === "terminal";
const showBrowserTab = activeView === "sessions" && focusedKind === "browser";
```

and drive each region's `className` off the matching flag.

- [ ] **Step 4: Run to verify it passes**

Run: `cd web && npx vitest run src/App.browser.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/App.tsx web/src/App.browser.test.tsx
git commit -m "feat(browser): render full-width browser tab in App center region"
```

---

## Task 10e: Side panel (resizable, collapsible) beside chat/terminal

**Verified current state:** `useResizableSidebar` is used at `App.tsx:116,117`; its resize handle pattern is at `:672-683` (`ref={sidebar.handleRef}`, `role="separator"`, `cursor-col-resize`, `onDoubleClick={sidebar.resetToDefault}`, `onKeyDown` for arrow/Home). The hook accepts `{ storageKey, defaultWidth, minWidth, maxWidth, collapsible }`.

- [ ] **Step 1: Write the failing test**

Add to `web/src/App.browser.test.tsx`:

```tsx
it("toggles the side browser panel open beside a chat session", async () => {
  renderApp();
  // ensure a chat session is focused (default), then click the side-panel toggle
  await userEvent.click(await screen.findByRole("button", { name: /toggle browser panel/i }));
  const panels = await screen.findAllByTestId("browser-panel");
  const side = panels.find((p) => p.getAttribute("data-mode") === "side");
  expect(side).toBeTruthy();
  expect(side!.getAttribute("data-key")).toMatch(/^side:chat:/);
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/App.browser.test.tsx -t "side browser panel"`
Expected: FAIL — no toggle button / no side-mode panel.

- [ ] **Step 3: Implement the side panel + hook + toggle**

In `App.tsx`:

1. Add the resize hook near the existing ones (:116):

```ts
const browserPane = useResizableSidebar({
  storageKey: "ocode.ui.browser_width",
  defaultWidth: 480,
  minWidth: 320,
  maxWidth: 1400,
  collapsible: true,
});
```

2. Add local open state and the side stateKey. The side panel accompanies whichever of chat/terminal is focused (not the browser *tab*, which is full-width):

```ts
import { useBrowserStore } from "./lib/browserStore";
// ...
const sidePanelKind = focusedKind === "terminal" ? "term" : "chat";
const sideStateKey = activeTabId ? `side:${sidePanelKind}:${activeTabId}` : null;
const browserOpen = useBrowserStore((s) => (sideStateKey ? s.isOpen(sideStateKey) : false));
const openBrowser = useBrowserStore((s) => s.open);
const closeBrowser = useBrowserStore((s) => s.close);
```

(Use whatever selector shape Part 08 exposes — if `useBrowserStore` returns the whole store, adapt to `const { open, close, isOpen } = useBrowserStore();`. Match Part 08's actual API: `open(key)`, `close(key)`, `setCollapsed(key,bool)`.)

3. Add a toggle button in the UnifiedTabBar row. The bar is rendered at `:713`; place the toggle just after it inside the same flex row:

```tsx
<button
  type="button"
  aria-label="Toggle browser panel"
  title="Toggle browser panel"
  className={/* match sibling toolbar button classes */}
  disabled={focusedKind === "browser" || !sideStateKey}
  onClick={() => {
    if (!sideStateKey) return;
    browserOpen ? closeBrowser(sideStateKey) : openBrowser(sideStateKey);
  }}
>
  🌐
</button>
```

(The side panel is meaningless when a full-width browser tab is focused, hence `disabled` there.)

4. Render the split. Wrap the center region and the side panel in a flex row. Beside the sessions/terminal container, add:

```tsx
{sideStateKey && browserOpen && (
  <>
    <div
      ref={browserPane.handleRef}
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize browser panel"
      tabIndex={0}
      className="w-1 flex-shrink-0 cursor-col-resize bg-transparent hover:bg-primary/40 active:bg-primary/60 transition-colors"
      onDoubleClick={browserPane.resetToDefault}
      onKeyDown={(e) => {
        // mirror the existing sidebar handle keyboard logic at App.tsx:682
        if (e.key === "ArrowLeft") browserPane.nudge(-16);
        else if (e.key === "ArrowRight") browserPane.nudge(16);
        else if (e.key === "Home") browserPane.resetToDefault();
      }}
    />
    <div style={{ width: browserPane.width }} className="flex-shrink-0 h-full">
      <BrowserPanel stateKey={sideStateKey} mode="side" />
    </div>
  </>
)}
```

(Match `nudge`/`resetToDefault`/`width`/`handleRef` to the hook's actual returned API — copy the exact handler shape from the existing handle at :672-683 rather than inventing method names.)

- [ ] **Step 4: Run to verify it passes**

Run: `cd web && npx vitest run src/App.browser.test.tsx`
Expected: PASS (both full-tab and side-panel tests).

- [ ] **Step 5: Commit**

```bash
git add web/src/App.tsx web/src/App.browser.test.tsx
git commit -m "feat(browser): resizable collapsible side browser panel beside chat/terminal"
```

---

## Task 10f: `Cmd/Ctrl+W` closes a focused browser tab

**Verified current state:** `web/src/hooks/useKeyboard.ts:47` intercepts `Cmd/Ctrl+W` in the desktop shell and closes the active session tab. It needs to branch on the focused kind so a focused browser tab closes instead.

- [ ] **Step 1: Write the failing test**

Add to the existing `useKeyboard` test file (create `web/src/hooks/useKeyboard.browser.test.ts` if the hook has no test harness, mocking `isDesktopShell` → true and the close callbacks):

```ts
it("closes the active browser tab on Cmd+W when a browser tab is focused", () => {
  const closeBrowserTab = vi.fn();
  const closeSessionTab = vi.fn();
  renderUseKeyboard({ focusedKind: "browser", activeBrowserId: "b1", closeBrowserTab, closeSessionTab });
  fireEvent.keyDown(window, { key: "w", metaKey: true });
  expect(closeBrowserTab).toHaveBeenCalledWith("b1");
  expect(closeSessionTab).not.toHaveBeenCalled();
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/hooks/useKeyboard.browser.test.ts`
Expected: FAIL — hook does not accept/branch on browser kind.

- [ ] **Step 3: Implement**

In `web/src/hooks/useKeyboard.ts`, extend the Cmd/Ctrl+W handler (:47) to check `focusedKind`. Thread the browser close action and active id into the hook's params (match how it currently receives the session-close callback), then:

```ts
if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "w") {
  e.preventDefault();
  if (focusedKind === "browser" && activeBrowserId) {
    closeBrowserTab(activeBrowserId);
  } else {
    closeActiveSessionTab(); // existing behavior
  }
  return;
}
```

Wire the new params where App.tsx calls `useKeyboard` (pass `focusedKind`, `activeBrowserId`, and the `closeBrowserTab` from `useBrowserTabs`).

- [ ] **Step 4: Run to verify it passes**

Run: `cd web && npx vitest run src/hooks/useKeyboard.browser.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/hooks/useKeyboard.ts web/src/hooks/useKeyboard.browser.test.ts web/src/App.tsx
git commit -m "feat(browser): Cmd/Ctrl+W closes focused browser tab"
```

---

## Task 10g: Document v1 non-goals + final full verification

- [ ] **Step 1: Record deferred scope in `TODO.md`**

Append to `TODO.md` (project root):

```markdown
## Embedded browser panel — v1 non-goals (deferred)

Shipped in the 2026-08-30 embedded-browser-panel plan; these are explicitly NOT built in v1:
- Agent/tool access to the embedded browser (reading pages, driving clicks).
- Screenshots / recordings of the browser panel.
- Cookie/auth session persistence across ocode server restarts (the browse
  cookie jar is in-memory; restart drops sessions).
- Multiple browser sub-tabs inside a single panel.
- Promoting a side panel to a standalone browser tab (side panel and browser
  tab keep independent state).
```

- [ ] **Step 2: Run the full verification suite**

```bash
go build ./... && go test ./...
cd web && npm test && npm run typecheck
```

Expected: all green. If the `UnifiedTabBar.drag.test.tsx` fixture or any App snapshot enumerates tab keys, update the fixtures to include browser ids (these are expected, in-scope updates — not test removals).

- [ ] **Step 3: Tell the user what shipped and what was deferred**

Report: browser side panel + browser tab are wired end-to-end; the five items above are deferred and recorded in `TODO.md`.

- [ ] **Step 4: Commit**

```bash
git add TODO.md
git commit -m "docs(browser): record embedded browser panel v1 non-goals in TODO.md"
```

---

## Notes for the executor

- **`useBrowserStore` / `BrowserPanel` API is authoritative from Parts 08/09.** Where this part guesses a selector shape (`isOpen`, `open`, `close`, `nudge`, `width`, `handleRef`, `resetToDefault`), open the real Part 08/09 exports and the real `useResizableSidebar` return value and match them exactly — do not invent method names.
- **Do not remove existing tests.** The `UnifiedTabBar` drag/order tests and any App test must keep passing; fixture updates to include browser ids are additive.
- **Provider order:** `BrowserTabsProvider` must wrap any component calling `useBrowserTabs` (UnifiedTabBar, App center). Mount it at the same composition site as `ProjectProvider`/`TerminalProvider`.
