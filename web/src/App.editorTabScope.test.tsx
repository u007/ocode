// App-level coverage for editor-tab scoping in the Files tab: a file left open
// in a remote SSH project must not stay visible after switching to a local
// project (and a local tab must not be replaced by a same-path remote tab).
// Heavy children are stubbed, mirroring App.browser.test.tsx's mocking pattern;
// only the projects store, the editor-tabs hook, and the tab bar are live.
import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeAll, beforeEach } from "vitest";
import { MemoryRouter } from "react-router-dom";

beforeAll(() => {
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
      if (!(prop in target)) target[prop] = (impl[prop] as unknown) ?? vi.fn(async () => ({}));
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
  eventBus: { on: () => () => {}, onReconnect: () => () => {}, emit: () => {}, start: () => {}, stop: () => {}, setProjects: () => {}, setHosts: () => {} },
}));

const REMOTE_TAB = {
  id: "editor-me@ssh::/remote::a.ts",
  path: "a.ts",
  projectRoot: "/remote",
  projectHost: "me@ssh",
  content: "",
  originalContent: "",
  isBinary: false,
  isDirty: false,
  diffVersion: 0,
  externalChange: false,
  includeInContext: true,
  baseHash: "",
};
const LOCAL_TAB = {
  id: "editor-/local::b.ts",
  path: "b.ts",
  projectRoot: "/local",
  content: "",
  originalContent: "",
  isBinary: false,
  isDirty: false,
  diffVersion: 0,
  externalChange: false,
  includeInContext: true,
  baseHash: "",
};

vi.mock("./hooks/useEditorTabs", () => ({
  useEditorTabs: () => ({
    editorTabs: [REMOTE_TAB, LOCAL_TAB],
    // The globally active tab is the REMOTE one — the tricky case.
    activeEditorTabId: REMOTE_TAB.id,
    setActiveEditorTabId: vi.fn(),
    handleOpenFile: vi.fn(async () => {}),
    handleEditorChange: vi.fn(),
    handleSelectionChange: vi.fn(),
    activeEditorContext: null,
    requestCloseTab: vi.fn(),
    toggleIncludeInContext: vi.fn(),
    closeTabsForPaths: vi.fn(),
    renameTabPath: vi.fn(),
    pendingClose: null,
    confirmSaveAndClose: vi.fn(async () => {}),
    confirmDiscardAndClose: vi.fn(),
    cancelClose: vi.fn(),
    saveError: null,
    saveEditorTab: vi.fn(async () => {}),
    forceSaveEditorTab: vi.fn(async () => {}),
    reloadTabFromDisk: vi.fn(async () => {}),
    dismissExternalChange: vi.fn(),
  }),
}));

const tabBar = vi.hoisted(() => ({
  items: [] as Array<{ id: string; path: string }>,
  activeId: null as string | null,
}));
vi.mock("./components/Layout/EditorTabBar", () => ({
  default: ({ editorTabs, activeEditorTabId }: { editorTabs: Array<{ id: string; path: string }>; activeEditorTabId: string | null }) => {
    tabBar.items = editorTabs;
    tabBar.activeId = activeEditorTabId;
    return (
      <div data-testid="editor-tab-bar">
        {editorTabs.map((t) => (
          <span key={t.id} data-testid="editor-tab-item" data-path={t.path} data-active={String(t.id === activeEditorTabId)} />
        ))}
      </div>
    );
  },
}));
const paneProps = vi.hoisted(() => ({
  byPath: {} as Record<string, { onSave?: unknown; dirty?: unknown }>,
}));
vi.mock("./components/Files/FileTabContent", () => ({
  default: ({ path, onSave, dirty }: { path: string; onSave?: unknown; dirty?: unknown }) => {
    paneProps.byPath[path] = { onSave, dirty };
    return <div data-testid="editor-pane" data-path={path} />;
  },
}));

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
vi.mock("./components/Logs/LogPanel", () => ({ default: () => null }));
vi.mock("./components/Terminal/TerminalTabs", () => ({ default: () => null }));
vi.mock("./components/Assets/AssetsPanel", () => ({ default: () => null }));
vi.mock("./components/Cron/CronPanel", () => ({ default: () => null }));
vi.mock("./components/Layout/SessionDialog", () => ({ default: () => null }));
vi.mock("./components/Layout/ModelDialog", () => ({ default: () => null }));
vi.mock("./components/Chat/PermissionDialog", () => ({ default: () => null }));
vi.mock("./components/Chat/QuestionDialog", () => ({ default: () => null }));
vi.mock("./components/Settings/SettingsPanel", () => ({ default: () => null }));
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

beforeEach(() => {
  window.localStorage.clear();
  tabBar.items = [];
  tabBar.activeId = null;
  paneProps.byPath = {};
  appApi.listProjects.mockReset().mockResolvedValue([REMOTE_PROJECT, LOCAL_PROJECT]);
  // Boot auto-selects the local project as the active project.
  appApi.getCurrentProject.mockReset().mockResolvedValue({ project: LOCAL_PROJECT });
  appApi.listProjectSessions.mockReset().mockResolvedValue([]);
  appApi.listGroups.mockReset().mockResolvedValue([]);
  appApi.getSpending.mockReset().mockResolvedValue({ spending_usd: 0 });
});

describe("Files tab editor-tab scoping", () => {
  it("shows only the active local project's tab, never the remote project's", async () => {
    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );

    // The editor tab bar is inside a force-mounted Files TabsContent, so it
    // renders regardless of the restored active view.
    await waitFor(() => expect(tabBar.items.length).toBeGreaterThan(0));

    const paths = tabBar.items.map((t) => t.path);
    expect(paths).toContain("b.ts"); // local project's file
    expect(paths).not.toContain("a.ts"); // remote project's file
    // The global active id pointed at the remote tab; the visible active id
    // must fall back to the local tab instead of leaving a hidden tab active.
    expect(tabBar.activeId).toBe("editor-/local::b.ts");

    const panes = screen.getAllByTestId("editor-pane").map((el) => ({
      path: el.getAttribute("data-path"),
      hidden: el.parentElement?.classList.contains("hidden") ?? false,
    }));
    // Both projects' panes stay MOUNTED (so their viewer state survives a
    // switch); the inactive project's pane is hidden, not removed.
    expect(panes.find((p) => p.path === "b.ts")).toMatchObject({ hidden: false });
    expect(panes.find((p) => p.path === "a.ts")).toMatchObject({ hidden: true });
  });

  it("wires a Save handler and the dirty flag into each editor pane", async () => {
    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );
    await waitFor(() => expect(tabBar.items.length).toBeGreaterThan(0));

    // The header Save button lives in FileEditor (touch devices have no
    // Cmd/Ctrl+S), so App must hand each pane a save callback and the tab's
    // dirty state — otherwise the button never renders.
    const local = paneProps.byPath["b.ts"];
    expect(typeof local?.onSave).toBe("function");
    expect(local?.dirty).toBe(false);
  });
});
