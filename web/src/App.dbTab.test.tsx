// App-level wiring for the project-level DB tab. The panel itself is mocked with
// a probe that records MOUNTS (not renders), so this file can prove three things
// the panel tests cannot:
//   1. the panel is not mounted for a project whose DB tab was never opened,
//   2. opening the DB tab mounts it once, with that project's path,
//   3. switching to another project remounts it with the new path.
// Plus the no-active-project case, which must render an empty state instead of a
// panel that can only fail to resolve its .env.
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeAll, beforeEach } from "vitest";
import { MemoryRouter } from "react-router-dom";

beforeAll(() => {
  // jsdom has neither of these; the shell (panels, resize handles) needs them.
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
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
}));

vi.mock("./api/client", () => {
  const impl: Record<string, unknown> = {
    listProjects: appApi.listProjects,
    getCurrentProject: appApi.getCurrentProject,
    listProjectSessions: appApi.listProjectSessions,
    listGroups: appApi.listGroups,
    getSpending: appApi.getSpending,
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
    authedFetch: vi.fn(async () => new Response("{}")),
    isPortMapsAvailable: vi.fn(async () => false),
    getBrowseBase: vi.fn(async () => "http://browse.test"),
    mintBrowseGrant: vi.fn(async () => "G1"),
    revokeBrowseSession: vi.fn(async () => {}),
    browseSrc: () => "",
  };
});

vi.mock("./lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {}, emit: () => {}, start: () => {}, stop: () => {}, setProjects: () => {}, onHostsChange: () => () => {}, currentHosts: () => [], setHosts: () => {} },
}));

// Records one entry per MOUNT. A remount (new React key) runs the effect again;
// an ordinary re-render does not.
const dbPanel = vi.hoisted(() => ({
  mounts: [] as { projectPath?: string; projectHost?: string }[],
}));

vi.mock("./components/Layout/DBPanel", async () => {
  // The factory is hoisted, so React is imported here rather than relying on the
  // module-scope binding being initialized when the factory runs.
  const { useEffect } = await import("react");
  return {
    default: (props: { projectPath?: string; projectHost?: string }) => {
      const { projectPath, projectHost } = props;
      useEffect(() => {
        dbPanel.mounts.push({ projectPath, projectHost });
        // Mount-only: the deps are deliberately empty.
        // eslint-disable-next-line react-hooks/exhaustive-deps
      }, []);
      return <div data-testid="db-panel" data-path={projectPath ?? ""} />;
    },
  };
});

vi.mock("./components/Chat/ChatInput", () => ({ default: () => null }));
vi.mock("./components/Preview/PreviewHost", () => ({ default: () => null }));
vi.mock("./components/Browser/BrowserPanel", () => ({ BrowserPanel: () => null }));
vi.mock("./components/Chat/ChatPanel", () => ({ default: () => null }));
vi.mock("./components/Chat/AgentPreview", () => ({ default: () => null }));
vi.mock("./components/common/StatusBar", () => ({ default: () => null }));
vi.mock("./components/Status/StatusPanel", () => ({ default: () => null }));
vi.mock("./components/common/CommandPalette", () => ({ default: () => null }));
vi.mock("./components/Git/GitPanel", () => ({ default: () => null }));
vi.mock("./components/Changes/ChangesPanel", () => ({ default: () => null }));
vi.mock("./components/Files/FileTree", () => ({ default: () => null }));
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
vi.mock("./components/Layout/SessionSubTabs", () => ({ default: () => null }));
vi.mock("./components/Layout/SessionTabSync", () => ({ default: () => null }));
vi.mock("./components/Layout/CoworkSidebar", () => ({ default: () => null }));
vi.mock("./components/Files/FilePicker", () => ({ default: () => null }));
vi.mock("./components/Files/ConfirmCloseDialog", () => ({ default: () => null }));
vi.mock("./pages/SessionPage", () => ({ default: () => null }));
vi.mock("./lib/debug/frontendMemoryReporter", () => ({ default: () => null }));
vi.mock("./components/common/ErrorBoundary", () => ({
  default: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

import App from "./App";

const PROJECT_A = { path: "/proj-a", name: "proj-a" };
const PROJECT_B = { path: "/proj-b", name: "proj-b" };

beforeEach(() => {
  window.localStorage.clear();
  dbPanel.mounts.length = 0;
  appApi.listProjects.mockReset().mockResolvedValue([PROJECT_A, PROJECT_B]);
  appApi.getCurrentProject.mockReset().mockResolvedValue({ project: PROJECT_A });
  appApi.listProjectSessions.mockReset().mockResolvedValue([]);
  appApi.listGroups.mockReset().mockResolvedValue([]);
  appApi.getSpending.mockReset().mockResolvedValue({ spending_usd: 0 });
});

function renderApp() {
  return render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  );
}
async function openDbTab() {
  const tab = await screen.findByRole("tab", { name: /^DB$/ });
  // Radix Tabs activates on MOUSEDOWN, not click: its trigger only handles
  // onMouseDown/onFocus, so fireEvent.click never changes the value.
  fireEvent.mouseDown(tab, { button: 0 });
}

// The per-project view is restored by a layout effect the moment the active
// project resolves, so a click issued before that can be overwritten. Seeding
// the persisted view is both deterministic and the real first-frame path.
function seedView(view: "files" | "db", path = "/proj-a") {
  window.localStorage.setItem(
    "ocode.ui.view-state.v1",
    JSON.stringify({ version: 1, projects: { [path]: { view, focusedKind: "chat" } } }),
  );
}

function activeView(): string | null {
  return document.querySelector("main")?.getAttribute("data-active-view") ?? null;
}

describe("App project-level DB tab", () => {
  it("does not mount the panel for a project whose DB tab was never opened", async () => {
    seedView("files");
    renderApp();
    await waitFor(() => expect(appApi.listProjects).toHaveBeenCalled());
    await waitFor(() => expect(activeView()).toBe("files"));

    // A per-project surface must not be created before the tab is visited.
    expect(dbPanel.mounts).toHaveLength(0);
  });

  it("mounts the panel once, with the active project's path, when the DB tab is shown", async () => {
    seedView("db");
    renderApp();

    await waitFor(() => expect(activeView()).toBe("db"));
    await waitFor(() => expect(dbPanel.mounts).toHaveLength(1));
    expect(dbPanel.mounts[0].projectPath).toBe("/proj-a");
    expect(dbPanel.mounts[0].projectHost).toBeUndefined();
  });

  it("drops the old project's panel and remounts for the new one after a switch", async () => {
    seedView("db");
    renderApp();
    await waitFor(() => expect(dbPanel.mounts).toHaveLength(1));

    // Switch projects from the sidebar. The expanded row is a div[role=button]
    // whose accessible name concatenates the badge text, hence the regex.
    const rows = await screen.findAllByRole("button", { name: new RegExp(PROJECT_B.name) });
    fireEvent.click(rows[0]);

    // /proj-b has never visited the DB tab, so the old project's panel is gone
    // (its unmount drops that surface) and the restored view is not DB.
    await waitFor(() => expect(screen.queryByTestId("db-panel")).toBeNull());
    await waitFor(() => expect(activeView()).not.toBe("db"));

    // Opening the tab now mounts a fresh panel for the new project.
    await openDbTab();
    await waitFor(() => expect(dbPanel.mounts).toHaveLength(2));
    expect(dbPanel.mounts[1].projectPath).toBe("/proj-b");
  });

  it("shows an empty state instead of the panel when no project is active", async () => {
    appApi.listProjects.mockResolvedValue([]);
    appApi.getCurrentProject.mockResolvedValue({});
    renderApp();

    await openDbTab();

    expect(await screen.findByText(/Select a project to use the database connector/)).toBeTruthy();
    expect(dbPanel.mounts).toHaveLength(0);
  });
});
