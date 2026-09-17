// App-level coverage for per-host event streams: the bus keeps one `/api/events`
// stream per remote host that has at least one OPEN session tab (the local ""
// stream always runs). The host set is derived from the open tabs, not from the
// active project — a remote project with a background tab still needs its
// stream. All heavy children are stubbed, mirroring App.browser.test.tsx's
// mocking pattern; the real projects store and eventBus wiring stay live, with
// only `setHosts` captured.
import { render, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeAll, beforeEach } from "vitest";
import { MemoryRouter } from "react-router-dom";

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

const appApi = vi.hoisted(() => ({
  listProjects: vi.fn(),
  getCurrentProject: vi.fn(),
  listProjectSessions: vi.fn(),
  listGroups: vi.fn(),
  getSpending: vi.fn(),
  getTabs: vi.fn(),
}));

vi.mock("./api/client", () => {
  const impl: Record<string, unknown> = {
    listProjects: appApi.listProjects,
    getCurrentProject: appApi.getCurrentProject,
    listProjectSessions: appApi.listProjectSessions,
    listGroups: appApi.listGroups,
    getSpending: appApi.getSpending,
    getTabs: appApi.getTabs,
  };
  const api = new Proxy({} as Record<string, unknown>, {
    get: (target, prop: string) => {
      if (!(prop in target)) target[prop] = impl[prop] ?? vi.fn(async () => ({}));
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
    remoteApiBase: (host?: string) => (host ? `/api/remote/${encodeURIComponent(host)}` : ""),
    authedFetch: vi.fn(async () => new Response("{}")),
    getBrowseBase: vi.fn(async () => "http://browse.test"),
    mintBrowseGrant: vi.fn(async () => "G1"),
    revokeBrowseSession: vi.fn(async () => {}),
    browseSrc: (base: string, grant: string | null, key: string) =>
      `${base}/b/${key}/${grant ? "g" : "n"}`,
  };
});

const bus = vi.hoisted(() => ({
  on: vi.fn(() => () => {}),
  onReconnect: vi.fn(() => () => {}),
  off: vi.fn(),
  offReconnect: vi.fn(),
  setProjects: vi.fn(),
  setHosts: vi.fn(),
  start: vi.fn(),
  stop: vi.fn(),
}));
vi.mock("./lib/eventBus", () => ({ eventBus: bus }));

vi.mock("./components/Browser/BrowserPanel", () => ({ BrowserPanel: () => null }));
vi.mock("./components/Chat/ChatPanel", () => ({ default: () => null }));
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
vi.mock("./components/Layout/TopTabs", () => ({ default: () => null }));
vi.mock("./components/Files/FilePicker", () => ({ default: () => null }));
vi.mock("./components/Files/ConfirmCloseDialog", () => ({ default: () => null }));
vi.mock("./pages/SessionPage", () => ({ default: () => null }));
vi.mock("./lib/debug/frontendMemoryReporter", () => ({ default: () => null }));
vi.mock("./components/common/ErrorBoundary", () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

import App from "./App";

const REMOTE_PROJECT = { path: "/remote", name: "remote", host: "me@ssh" };
const LOCAL_PROJECT = { path: "/local", name: "local" };

function renderApp() {
  return render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  window.localStorage.clear();
  bus.setProjects.mockClear();
  bus.setHosts.mockClear();
  appApi.listProjects.mockReset().mockResolvedValue([REMOTE_PROJECT, LOCAL_PROJECT]);
  // The active project is LOCAL — the remote host must still get a stream
  // because it has a background tab open.
  appApi.getCurrentProject.mockReset().mockResolvedValue({ project: LOCAL_PROJECT });
  appApi.listProjectSessions.mockReset().mockResolvedValue([]);
  appApi.listGroups.mockReset().mockResolvedValue([]);
  appApi.getSpending.mockReset().mockResolvedValue({ spending_usd: 0 });
  appApi.getTabs.mockReset().mockResolvedValue({ projects: {} });
});

describe("App per-host event streams", () => {
  it("declares the host of a background remote-project tab even when the active project is local", async () => {
    appApi.getTabs.mockResolvedValue({
      projects: { "/remote": { tabs: [{ id: "s1", title: "s1", sub_tab: "chat" }], active: "s1" } },
    });

    renderApp();

    // Restore settled once the tab is visible to the projects store.
    await waitFor(() => expect(bus.setProjects).toHaveBeenCalledWith(["/remote"]));
    await waitFor(() => expect(bus.setHosts).toHaveBeenCalledWith(["me@ssh"]));
  });

  it("declares no remote host when every open tab belongs to a local project", async () => {
    appApi.getTabs.mockResolvedValue({
      projects: { "/local": { tabs: [{ id: "s2", title: "s2", sub_tab: "chat" }], active: "s2" } },
    });

    renderApp();

    await waitFor(() => expect(bus.setProjects).toHaveBeenCalledWith(["/local"]));
    expect(bus.setHosts).toHaveBeenLastCalledWith([]);
    expect(bus.setHosts.mock.calls.flat().flat()).not.toContain("me@ssh");
  });
});
