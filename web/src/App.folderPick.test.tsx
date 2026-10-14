// App-level coverage for picking a FOLDER in the global ctrl/cmd+P picker.
//
// The failure this guards against is a wiring gap between the App's picker
// callback and the file tree's reveal (e.g. dropping `isDir` at the call site),
// which the picker and tree unit tests cannot see. The tree and App are real;
// the rest is stubbed as in App.revealInTree.test.tsx. The mocked tree is
//   /local
//     └ src/
//         └ app/
//             └ deep.ts
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeAll, beforeEach, afterAll } from "vitest";
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
  if (!(globalThis as any).PointerEvent) (globalThis as any).PointerEvent = MouseEvent;
  if (!(globalThis as any).ResizeObserver) {
    (globalThis as any).ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
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
    setHosts: () => {},
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

// The picker's own UI is stubbed: it only exposes the one callback this suite
// drives, exactly as the real picker calls it (a root-relative path plus the
// project root). Opening goes through the REAL global ctrl/cmd+P keydown.
vi.mock("./components/Files/FilePicker", () => ({
  default: (props: { open: boolean; onOpenFolder?: (path: string, root?: string) => void }) =>
    props.open ? (
      <button type="button" onClick={() => props.onOpenFolder?.("src/app", "/local")}>
        pick folder
      </button>
    ) : null,
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

describe("ctrl/cmd+P folder pick (App wiring)", () => {
  it("picking a folder reveals it in the Files tree, expanded and in view", async () => {
    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );
    await waitFor(() => expect(row("src")).toBeTruthy());
    expect(row("src/app")).toBeNull(); // collapsed: nothing under src is open yet

    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    fireEvent.click(await screen.findByRole("button", { name: "pick folder" }));

    // The folder is expanded, so its file is listed. Without the isDir
    // expansion only src would open and this row would never appear.
    await waitFor(() => expect(row("src/app/deep.ts")).toBeTruthy());
    expect(row("src/app")?.getAttribute("data-revealed")).toBe("");
    expect(scrollIntoView).toHaveBeenCalledWith({ block: "center", inline: "nearest" });
    // The reveal lands on the Files view.
    expect(
      (document.querySelector('[role="tabpanel"]') as HTMLElement | null)?.dataset.state,
    ).toBe("active");
  });
});
