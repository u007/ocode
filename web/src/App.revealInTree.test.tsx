// End-to-end (App-level) coverage for the Files tab's "Show in file tree".
//
// Both ends are the REAL components here — the editor tab bar's context menu
// and the file tree — because the failure this guards against is a wiring gap
// between them, which neither unit test can see. Everything else is stubbed
// (same pattern as App.editorTabScope.test.tsx).
//
// The mocked tree is
//   /local
//     └ src/
//         └ app/
//             └ deep.ts        ← the open editor tab's file
import { render, screen, waitFor, fireEvent, within } from "@testing-library/react";
import { describe, it, expect, vi, beforeAll, beforeEach, afterAll, afterEach } from "vitest";
import { MemoryRouter } from "react-router-dom";

/** Mutable matchMedia result so a test can simulate a phone viewport. */
let mqMatches = false;

beforeAll(() => {
  window.matchMedia = ((media: string) => ({
    get matches() {
      return mqMatches;
    },
    media,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
  if (!(globalThis as any).PointerEvent) (globalThis as any).PointerEvent = MouseEvent;
  if (!(globalThis as any).ResizeObserver) {
    (globalThis as any).ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

afterEach(() => {
  mqMatches = false;
});

const appApi = vi.hoisted(() => ({
  listProjects: vi.fn(),
  getCurrentProject: vi.fn(),
  listProjectSessions: vi.fn(),
  listGroups: vi.fn(),
  getSpending: vi.fn(),
  getPathsConfig: vi.fn(),
}));

vi.mock("./api/client", () => {
  const impl: Record<string, unknown> = {
    listProjects: appApi.listProjects,
    getCurrentProject: appApi.getCurrentProject,
    listProjectSessions: appApi.listProjectSessions,
    listGroups: appApi.listGroups,
    getSpending: appApi.getSpending,
    getPathsConfig: appApi.getPathsConfig,
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
  eventBus: {
    on: () => () => {},
    onReconnect: () => () => {},
    emit: () => {},
    start: () => {},
    stop: () => {},
    setProjects: () => {},
    onHostsChange: () => () => {}, currentHosts: () => [], setHosts: () => {},
  },
}));

// One open editor tab, addressed by ABSOLUTE path (the shape a chat file link
// produces) with its project root — the tree has to reconcile the two.
const TAB = {
  id: "editor-/local::/local/src/app/deep.ts",
  path: "/local/src/app/deep.ts",
  projectRoot: "/local",
  content: "export const x = 1;",
  originalContent: "export const x = 1;",
  isBinary: false,
  isDirty: false,
  diffVersion: 0,
  externalChange: false,
  includeInContext: true,
  baseHash: "",
};

vi.mock("./hooks/useEditorTabs", () => ({
  useEditorTabs: () => ({
    editorTabs: [TAB],
    activeEditorTabId: TAB.id,
    setActiveEditorTabId: vi.fn(),
    handleOpenFile: vi.fn(async () => true),
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

// Heavy / unrelated surfaces.
vi.mock("./components/Files/FileTabContent", () => ({ default: () => null }));
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
// The real shadcn Dialog deadlocks jsdom/React (FileTree and ShareDialog
// import it); stub the whole surface any of them touches.
vi.mock("./components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open?: boolean; children?: React.ReactNode }) =>
    open ? <>{children}</> : null,
  DialogContent: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogFooter: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogDescription: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogTrigger: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  DialogClose: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
}));

import App from "./App";

const PROJECT = { path: "/local", name: "local" };

const ROOT = { children: [{ name: "src", path: "src", is_dir: true }], truncated: false };
const SRC = { children: [{ name: "app", path: "src/app", is_dir: true }], truncated: false };
const APP_DIR = { children: [{ name: "deep.ts", path: "src/app/deep.ts", is_dir: false }], truncated: false };

let scrollIntoView: ReturnType<typeof vi.fn>;
let originalScrollIntoView: unknown;

beforeAll(() => {
  // src/test/setup.ts polyfills a no-op scrollIntoView on HTMLElement.prototype;
  // the spy must replace it there or it is shadowed.
  originalScrollIntoView = (globalThis as any).HTMLElement.prototype.scrollIntoView;
  scrollIntoView = vi.fn();
  (globalThis as any).HTMLElement.prototype.scrollIntoView = scrollIntoView;
});

afterAll(() => {
  (globalThis as any).HTMLElement.prototype.scrollIntoView = originalScrollIntoView;
});

function row(path: string): HTMLElement | null {
  return document.querySelector<HTMLElement>(`[data-tree-path="${path}"]`);
}

beforeEach(() => {
  window.localStorage.clear();
  scrollIntoView.mockClear();
  appApi.listProjects.mockReset().mockResolvedValue([PROJECT]);
  appApi.getCurrentProject.mockReset().mockResolvedValue({ project: PROJECT });
  appApi.listProjectSessions.mockReset().mockResolvedValue([]);
  appApi.listGroups.mockReset().mockResolvedValue([]);
  appApi.getSpending.mockReset().mockResolvedValue({ spending_usd: 0 });
  appApi.getPathsConfig.mockReset().mockResolvedValue({ extra_allowed_paths: [], upload_dir: "", platform: "darwin" });
  vi.spyOn(globalThis as any, "fetch").mockImplementation((async (input: RequestInfo | URL) => {
    const url = String(input);
    if (!url.includes("/api/files/tree")) return new Response("{}", { status: 200 });
    const path = new URL(url, "http://test").searchParams.get("path") ?? "";
    if (path === "/local/src/app") return new Response(JSON.stringify(APP_DIR), { status: 200 });
    if (path === "/local/src") return new Response(JSON.stringify(SRC), { status: 200 });
    if (path === "/local") return new Response(JSON.stringify(ROOT), { status: 200 });
    return new Response(JSON.stringify({ children: [], truncated: false }), { status: 200 });
  }) as any);
});

describe("Show in file tree (App wiring)", () => {
  it("right-clicking the editor tab reveals its file in the Files tree", async () => {
    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );

    // The tree rendered its root listing.
    await waitFor(() => expect(row("src")).toBeTruthy());
    expect(row("src/app")).toBeNull(); // collapsed: app/ is not visible yet

    fireEvent.contextMenu(await screen.findByText("deep.ts"));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Show in file tree" }));

    // The ancestors expanded and the row is on screen, scrolled to and marked.
    await waitFor(() => expect(row("src/app/deep.ts")).toBeTruthy());
    expect(row("src/app")).toBeTruthy();
    expect(scrollIntoView).toHaveBeenCalledWith({ block: "center", inline: "nearest" });
    expect(row("src/app/deep.ts")?.getAttribute("data-revealed")).toBe("");
  });

  it("switches to the Files view when the reveal starts from another view", async () => {
    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );
    await waitFor(() => expect(row("src")).toBeTruthy());

    // Radix keeps an inactive TabsContent mounted but hidden, and the editor tab
    // bar lives inside it — so the tab can be right-clicked from any view.
    const filesPanel = document.querySelector('[role="tabpanel"]') as HTMLElement | null;
    expect(filesPanel?.dataset.state).toBe("inactive");

    fireEvent.contextMenu(await screen.findByText("deep.ts"));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Show in file tree" }));

    // The reveal brings the user to the Files view, not just the tree's data.
    await waitFor(() => expect(row("src/app/deep.ts")).toBeTruthy());
    expect(
      (document.querySelector('[role="tabpanel"]') as HTMLElement | null)?.dataset.state,
    ).toBe("active");
  });

  it("reveals even when the file tree pane is collapsed", async () => {
    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );
    // Collapse the tree the way the pane's own toggle does.
    const collapse = await screen.findByTitle("Hide file tree");
    fireEvent.click(collapse);

    fireEvent.contextMenu(await screen.findByText("deep.ts"));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Show in file tree" }));

    // Re-opened and revealed, not silently swallowed by the collapsed pane.
    await waitFor(() => expect(screen.getByTitle("Hide file tree")).toBeTruthy());
    await waitFor(() => expect(row("src/app/deep.ts")).toBeTruthy());
  });
});
// On a phone there is no room for a side-by-side tree+editor split, so the file
// tree becomes an overlay drawer over the editor. These drive the REAL App so
// the drawer state, the toggle, and the open-file hand-off are wired together.
describe("Files tree drawer on a phone", () => {
  it("opens as an overlay and closes when a file is picked", async () => {
    mqMatches = true;
    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );

    const pane = await waitFor(() => screen.getByTestId("file-tree-pane"));
    // Closed by default: the editor owns the full width.
    expect(pane.className).toContain("absolute");
    expect(pane.className).toContain("-translate-x-full");
    expect(screen.queryByTestId("file-tree-backdrop")).toBeNull();

    // "Show in file tree" from another view must surface the tree on a phone.
    fireEvent.contextMenu(await screen.findByText("deep.ts"));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Show in file tree" }));
    await waitFor(() => expect(row("src/app/deep.ts")).toBeTruthy());
    expect(pane.className).not.toContain("-translate-x-full");
    expect(screen.getByTestId("file-tree-backdrop")).toBeTruthy();

    // Picking a file dismisses the drawer so the opened file is visible. The
    // row's clickable element is the inner button, not the wrapper carrying
    // `data-tree-path`.
    fireEvent.click(within(row("src/app/deep.ts")!).getByText("deep.ts"));
    await waitFor(() => expect(pane.className).toContain("-translate-x-full"));
    expect(screen.queryByTestId("file-tree-backdrop")).toBeNull();
  });

  it("toggles from the header button and dismisses on backdrop click and Esc", async () => {
    mqMatches = true;
    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );

    // Reach the Files view through a reveal (TopTabs is stubbed out here).
    fireEvent.contextMenu(await screen.findByText("deep.ts"));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Show in file tree" }));
    const pane = await waitFor(() => screen.getByTestId("file-tree-pane"));
    expect(pane.className).not.toContain("-translate-x-full");

    // The pane's own toggle closes it again.
    fireEvent.click(screen.getByTitle("Hide file tree"));
    expect(pane.className).toContain("-translate-x-full");
    expect(screen.queryByTestId("file-tree-backdrop")).toBeNull();

    // ...and re-opens it.
    fireEvent.click(screen.getByTitle("Show file tree"));
    expect(pane.className).not.toContain("-translate-x-full");

    // Tapping the backdrop dismisses it.
    fireEvent.click(screen.getByTestId("file-tree-backdrop"));
    expect(pane.className).toContain("-translate-x-full");

    // Esc does the same.
    fireEvent.click(screen.getByTitle("Show file tree"));
    expect(pane.className).not.toContain("-translate-x-full");
    fireEvent.keyDown(window, { key: "Escape" });
    expect(pane.className).toContain("-translate-x-full");
  });
});
