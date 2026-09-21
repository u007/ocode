// App-level integration coverage for tab-focus requests coming from outside
// HomeApp's tree (the remote project's chat/terminal inventory in the
// sidebar): the request must switch the top view to Sessions and show the
// chat or terminal half, and it must not apply against the wrong project.
// All heavy children are stubbed; the real providers (Chat/Project/Terminal)
// stay live so the store → effect → view-state wiring under test is genuine.
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
vi.mock("./components/Terminal/TerminalTabs", () => ({
  default: ({ projectPath, host }: { projectPath: string; host?: string }) => {
    // Reads the real terminal store so the test can observe the active id the
    // focus request selected (TerminalTabs would otherwise be an opaque stub).
    const { state } = useTerminalState();
    const { activeId, terminals } = getProjectTerminals(state, projectPath, host);
    return (
      <div
        data-testid="terminal-tabs"
        data-project-path={projectPath}
        data-host={host ?? ""}
        data-active-id={activeId}
        data-terminal-count={terminals.length}
      />
    );
  },
}));
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

import { getProjectTerminals, useTerminalState } from "./stores/terminalStore";
import { tabFocusActions, tabFocusStore } from "./lib/tabFocus";
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

beforeEach(() => {
  window.localStorage.clear();
  // The focus queue is module-level, so it must not leak between tests.
  tabFocusActions.clear();
  appApi.listProjects.mockReset().mockResolvedValue([{ path: "/proj", name: "proj" }]);
  appApi.getCurrentProject.mockReset().mockResolvedValue({ project: { path: "/proj", name: "proj" } });
  appApi.listProjectSessions.mockReset().mockResolvedValue([]);
  appApi.listGroups.mockReset().mockResolvedValue([]);
  appApi.getSpending.mockReset().mockResolvedValue({ spending_usd: 0 });
});

describe("App tab focus requests", () => {
  it("switches a restored Files view to the chat half on a chat focus request", async () => {
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("files", "chat"));
    renderApp();
    // Restore lands first — the request below has to override it, not the reverse.
    await waitFor(() => expect(mainAttr("data-active-view")).toBe("files"));

    act(() => tabFocusActions.request({ kind: "chat", projectPath: "/proj" }));

    await waitFor(() => expect(mainAttr("data-active-view")).toBe("sessions"));
    expect(mainAttr("data-focused-kind")).toBe("chat");
    // Consumed, so it can't re-fire on a later render.
    expect(tabFocusStore.state.pending).toBeNull();
  });

  it("reveals the terminal half and activates the requested terminal", async () => {
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("files", "chat"));
    renderApp();
    await waitFor(() => expect(mainAttr("data-active-view")).toBe("files"));
    await screen.findByTestId("terminal-tabs");

    act(() => tabFocusActions.request({ kind: "terminal", projectPath: "/proj", terminalId: "t1" }));

    await waitFor(() => expect(mainAttr("data-active-view")).toBe("sessions"));
    expect(mainAttr("data-focused-kind")).toBe("terminal");
    await waitFor(() =>
      expect(screen.getByTestId("terminal-tabs").getAttribute("data-active-id")).toBe("t1"),
    );
  });

  it("does not apply a request that names a project which is not active", async () => {
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("files", "chat"));
    renderApp();
    await waitFor(() => expect(mainAttr("data-active-view")).toBe("files"));

    act(() => tabFocusActions.request({ kind: "terminal", projectPath: "/other", terminalId: "t9" }));
    // Let the passive effect run; it must bail on the project mismatch.
    await act(async () => {
      await Promise.resolve();
    });

    expect(mainAttr("data-active-view")).toBe("files");
    // Still queued — it applies when (and only when) that project becomes active.
    expect(tabFocusStore.state.pending?.projectPath).toBe("/other");
  });

  it("does not apply a request whose host differs even when the path matches", async () => {
    // Path alone is not project identity: a local and a remote project can share
    // an absolute path. The active project here is the LOCAL "/proj" (no host),
    // so a request for the remote "/proj" must not switch this one's view.
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("files", "chat"));
    renderApp();
    await waitFor(() => expect(mainAttr("data-active-view")).toBe("files"));

    act(() =>
      tabFocusActions.request({ kind: "chat", projectPath: "/proj", host: "dev@box" }),
    );
    await act(async () => {
      await Promise.resolve();
    });

    expect(mainAttr("data-active-view")).toBe("files");
    expect(tabFocusStore.state.pending?.host).toBe("dev@box");
  });
});
