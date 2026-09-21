// TRUE end-to-end coverage for the remote-project inventory reveal: the real
// App + real ProjectSidebar + real RemoteProjectStatus are rendered (only the
// host-status/terminal hooks and heavy children are stubbed), so a single
// click on a chat in a NON-active remote project's inventory exercises the
// whole chain — select the project, bind the tab to it, queue the focus, and
// have HomeApp apply it over the restored view.
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

vi.mock("./components/Chat/ChatPanel", () => ({
  default: ({ sessionId }: { sessionId: string }) => (
    <div data-testid="chat-panel" data-session-id={sessionId} />
  ),
}));
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

// The remote row's host status and terminal inventory are the only network
// dependencies of the real sidebar; stub them so the row renders connected.
const hostFake = vi.hoisted(() => ({ connected: true }));
vi.mock("./hooks/useRemoteHostStatus", () => ({
  useRemoteHostStatus: () => ({
    status: {
      host: "dev@box",
      connected: hostFake.connected,
      version: "1.2.3",
      local_version: "1.2.3",
      outdated: false,
      pid: 1,
    },
    loading: false,
    error: null,
    busy: "idle",
    refresh: vi.fn(),
    connect: vi.fn(),
    restart: vi.fn(),
  }),
}));
vi.mock("./hooks/useRemoteTerminals", () => ({
  useRemoteTerminals: () => ({ terminals: [], loading: false, error: null, refresh: vi.fn() }),
}));

import { fireEvent } from "@testing-library/react";
import { getProjectTerminals, useTerminalState } from "./stores/terminalStore";
import App from "./App";

const remoteProject = {
  path: "/remote",
  name: "remote",
  host: "dev@box",
  added_at: "",
  last_used_at: "",
  order: 1,
  group: "",
};
const localProject = {
  path: "/proj",
  name: "proj",
  added_at: "",
  last_used_at: "",
  order: 2,
  group: "",
};

function renderApp() {
  return render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  window.localStorage.clear();
  hostFake.connected = true;
  appApi.listProjects.mockReset().mockResolvedValue([remoteProject, localProject]);
  // The LOCAL project is active, so the remote row's inventory is the
  // non-active-project case the binding rule exists for.
  appApi.getCurrentProject.mockReset().mockResolvedValue({ project: localProject });
  appApi.listProjectSessions
    .mockReset()
    .mockImplementation(async (_path: string, host?: string) =>
      host === "dev@box"
        ? [{ id: "remote-s1", title: "Remote one", created_at: "", updated_at: "" }]
        : [],
    );
  appApi.listGroups.mockReset().mockResolvedValue([]);
  appApi.getSpending.mockReset().mockResolvedValue({ spending_usd: 0 });
});

describe("App end-to-end: revealing a tab from a remote project's inventory", () => {
  it("selects the non-active project, binds its tab, and reveals it in one click", async () => {
    // The remote project's SAVED view is Files: the click has to override the
    // restore with Sessions, which is the ordering the passive effect exists for.
    window.localStorage.setItem(
      "ocode.ui.view-state.v1",
      JSON.stringify({ version: 1, projects: { "/remote": { view: "files", focusedKind: "chat" } } }),
    );
    renderApp();

    // Let the boot auto-select settle first: the sidebar rows rebuild while the
    // project list lands, and a node captured mid-boot can be replaced.
    await waitFor(() => expect(screen.getByText("remote")).toBeTruthy());
    await waitFor(() => expect(appApi.listProjects).toHaveBeenCalled());
    await act(async () => {});

    // Real sidebar row → real inventory. Expanding the status line must NOT
    // dismiss/select anything (it stops propagation), then the chat opens. The
    // chat row's handler is onPointerUp (the terminal row's is onClick).
    fireEvent.click(screen.getByTestId("remote-project-status"));
    await waitFor(() =>
      expect(screen.getByTestId("remote-project-status").getAttribute("aria-expanded")).toBe("true"),
    );
    fireEvent.pointerUp(await screen.findByText("Remote one"));

    // HomeApp switched the top view + focused half over the restored (Files) view.
    await waitFor(() =>
      expect(document.querySelector("main")?.getAttribute("data-active-view")).toBe("sessions"),
    );
    expect(document.querySelector("main")?.getAttribute("data-focused-kind")).toBe("chat");

    // The tab is owned by the REMOTE project (which is what makes
    // resolveSessionHost route it through dev@box) and is the active tab, so
    // App renders its chat panel with that session id.
    await waitFor(() =>
      expect(screen.getByTestId("chat-panel").getAttribute("data-session-id")).toBe("remote-s1"),
    );
  });
});

