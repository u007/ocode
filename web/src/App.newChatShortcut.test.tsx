// App-level wiring for the new-chat shortcut (Ctrl/Cmd+N). The key handler in
// useKeyboard.ts is unit-tested separately; this file covers what the app does
// with it — reveal the merged Sessions view on the chat half and open a new
// chat tab, exactly like the tab bar's "new chat" button, from any view.
// Heavy children are stubbed (mirroring App.tabFocus.test.tsx); the real
// Chat/Project providers stay live so the store → view-state wiring is genuine.
import { act, render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeAll, beforeEach } from "vitest";
import { MemoryRouter } from "react-router-dom";

const appApi = vi.hoisted(() => ({
  listProjects: vi.fn(),
  getCurrentProject: vi.fn(),
  listProjectSessions: vi.fn(),
  listGroups: vi.fn(),
  getSpending: vi.fn(),
}));

beforeAll(() => {
  // jsdom ships no matchMedia; useIsMobile reads it on first render.
  window.matchMedia = ((media: string) => ({
    matches: false,
    media,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});

vi.mock("./api/client", () => {
  // The project-store boot path needs a coherent trio; everything else may
  // return a generic empty object (all consumers guard or the call is inert).
  const impl: Record<string, unknown> = {
    listProjects: appApi.listProjects,
    getCurrentProject: appApi.getCurrentProject,
    listProjectSessions: appApi.listProjectSessions,
    listGroups: appApi.listGroups,
    getSpending: appApi.getSpending,
  };
  const api = new Proxy({} as Record<string, unknown>, {
    get: (target, prop: string) => {
      if (!(prop in target)) {
        target[prop] = impl[prop] ?? vi.fn(async () => ({}));
      }
      return target[prop];
    },
  });
  return {
    api,
    authHeaders: () => ({}),
    authToken: () => null,
    isRemoteSession: () => false,
    apiPath: (p: string) => p,
    apiWsPath: (p: string) => `ws://localhost${p}`,
    authedFetch: vi.fn(async () => new Response("{}")),
    getBrowseBase: vi.fn(async () => "http://browse.test"),
    mintBrowseGrant: vi.fn(async () => "G1"),
    revokeBrowseSession: vi.fn(async () => {}),
    browseSrc: (base: string, grant: string | null, key: string) =>
      `${base}/b/${key}/${grant ? "g" : "n"}`,
  };
});

vi.mock("./lib/eventBus", () => ({
  eventBus: {
    on: () => () => {},
    onReconnect: () => () => {},
    emit: () => {},
    start: () => {},
    stop: () => {},
    setProjects: () => {},
    setHosts: () => {},
  },
}));

vi.mock("./components/Browser/BrowserPanel", () => ({
  BrowserPanel: ({ stateKey, mode, active }: { stateKey: string; mode: string; active?: boolean }) => (
    <div data-testid="browser-panel" data-key={stateKey} data-mode={mode} data-active={String(active ?? true)} />
  ),
}));

vi.mock("./components/Chat/ChatPanel", () => ({ default: () => <div data-testid="chat-panel" /> }));
vi.mock("./components/Chat/AgentPreview", () => ({ default: () => null }));
vi.mock("./components/Agents/AgentsPanel", () => ({ default: () => null }));
vi.mock("./components/Chat/ChatInput", () => ({ default: () => null }));
vi.mock("./components/common/StatusBar", () => ({ default: () => null }));
vi.mock("./components/Status/StatusPanel", () => ({ default: () => null }));
vi.mock("./components/common/CommandPalette", () => ({ default: () => null }));
vi.mock("./components/Git/GitPanel", () => ({ default: () => null }));
vi.mock("./components/Changes/ChangesPanel", () => ({ default: () => null }));
vi.mock("./components/Files/FileTree", () => ({ default: () => null }));
vi.mock("./components/Files/FileEditor", () => ({ default: () => null }));
vi.mock("./components/Logs/LogPanel", () => ({ default: () => null }));
vi.mock("./components/Terminal/TerminalTabs", () => ({ default: () => null }));
vi.mock("./components/Assets/AssetsPanel", () => ({ default: () => null }));
vi.mock("./components/Cron/CronPanel", () => ({ default: () => null }));
vi.mock("./components/Layout/SessionDialog", () => ({ default: () => null }));
vi.mock("./components/Layout/ModelDialog", () => ({ default: () => null }));
vi.mock("./components/Chat/PermissionDialog", () => ({ default: () => null }));
vi.mock("./components/Chat/QuestionDialog", () => ({ default: () => null }));
vi.mock("./components/Settings/SettingsPanel", () => ({ default: () => null }));
vi.mock("./components/Layout/EditorTabBar", () => ({ default: () => null }));
vi.mock("./components/Layout/ProjectSidebar", () => ({ default: () => null }));
vi.mock("./components/Layout/SessionSubTabs", () => ({ default: () => null }));
vi.mock("./components/Layout/SessionTabSync", () => ({ default: () => null }));
vi.mock("./components/Layout/CoworkSidebar", () => ({ default: () => null }));
vi.mock("./components/Files/FilePicker", () => ({ default: () => null }));
vi.mock("./components/Files/ConfirmCloseDialog", () => ({ default: () => null }));
vi.mock("./components/Layout/TopTabs", () => ({ default: () => null }));
vi.mock("./pages/SessionPage", () => ({ default: () => null }));
vi.mock("./lib/debug/frontendMemoryReporter", () => ({ default: () => null }));
vi.mock("./components/common/ErrorBoundary", () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

import App from "./App";

function renderApp() {
  return render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  );
}

/** Per-project persisted view state, read by HomeApp's restore layout effect. */
const viewState = (view: string, focusedKind: string) =>
  JSON.stringify({ version: 1, projects: { "/proj": { view, focusedKind } } });

const mainAttr = (attr: string) => document.querySelector("main")?.getAttribute(attr);

/** dispatchKey mirrors web/src/hooks/useKeyboard.test.ts. */
function dispatchKey(key: string, meta = false, ctrl = false) {
  const ev = new KeyboardEvent("keydown", {
    key,
    metaKey: meta,
    ctrlKey: ctrl,
    bubbles: true,
    cancelable: true,
  });
  window.dispatchEvent(ev);
}

beforeEach(() => {
  window.localStorage.clear();
  appApi.listProjects.mockReset().mockResolvedValue([{ path: "/proj", name: "proj" }]);
  appApi.getCurrentProject.mockReset().mockResolvedValue({ project: { path: "/proj", name: "proj" } });
  appApi.listProjectSessions.mockReset().mockResolvedValue([]);
  appApi.listGroups.mockReset().mockResolvedValue([]);
  appApi.getSpending.mockReset().mockResolvedValue({ spending_usd: 0 });
});

describe("App Ctrl/Cmd+N new chat shortcut", () => {
  it("reveals the chat half and opens a new chat tab from the Files view", async () => {
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("files", "chat"));
    renderApp();
    // Restore lands on Files first — the shortcut has to override it.
    await waitFor(() => expect(mainAttr("data-active-view")).toBe("files"));
    // No tabs open for this project yet.
    expect(screen.queryByText("No open sessions for this project")).not.toBeNull();

    act(() => dispatchKey("n", true));

    await waitFor(() => expect(mainAttr("data-active-view")).toBe("sessions"));
    expect(mainAttr("data-focused-kind")).toBe("chat");
    // A brand-new chat tab now exists, so the empty state is gone.
    await waitFor(() => expect(screen.queryByText("No open sessions for this project")).toBeNull());
  });

  it("reuses the blank chat tab on a second Ctrl/Cmd+N", async () => {
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("files", "chat"));
    renderApp();
    await waitFor(() => expect(mainAttr("data-active-view")).toBe("files"));

    act(() => dispatchKey("n", true));
    await waitFor(() => expect(mainAttr("data-active-view")).toBe("sessions"));
    // Still exactly one chat pane: the empty-tab reuse path, not a stack.
    expect(screen.getAllByTestId("chat-panel")).toHaveLength(1);

    act(() => dispatchKey("n", false, true));
    await waitFor(() => expect(mainAttr("data-focused-kind")).toBe("chat"));
    expect(screen.getAllByTestId("chat-panel")).toHaveLength(1);
  });

  it("moves focus back to the chat half when the terminal half is active", async () => {
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("sessions", "terminal"));
    renderApp();
    await waitFor(() => expect(mainAttr("data-active-view")).toBe("sessions"));
    await waitFor(() => expect(mainAttr("data-focused-kind")).toBe("terminal"));

    act(() => dispatchKey("n", true));

    await waitFor(() => expect(mainAttr("data-focused-kind")).toBe("chat"));
  });

  it("Ctrl/Cmd+T outside Sessions is the same new-chat action", async () => {
    // The non-Sessions fallback of Ctrl/Cmd+T must reveal too — otherwise the
    // tab is added invisibly, which is the bug this change set fixes.
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("files", "chat"));
    renderApp();
    await waitFor(() => expect(mainAttr("data-active-view")).toBe("files"));

    act(() => dispatchKey("t", true));

    await waitFor(() => expect(mainAttr("data-active-view")).toBe("sessions"));
    expect(mainAttr("data-focused-kind")).toBe("chat");
    await waitFor(() => expect(screen.queryByText("No open sessions for this project")).toBeNull());
  });
});
