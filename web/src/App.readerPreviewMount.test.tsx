// Regression for the side preview's identity across reader mode. The side
// PreviewHost is the same element whether the preview sits in the side pane or
// fills the main area in reader mode. Toggling reader mode must MOVE the
// preview, never remount it: a remount runs PreviewHost's unmount cleanup
// (which clears the chat viewer chip's selection) and reloads the browser
// iframe. PreviewHost is stubbed with a mount/unmount log; everything else
// (session dialog, side pane, reader header) is the real App wiring.
import { fireEvent, render, screen, within } from "@testing-library/react";
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

const previewLog = vi.hoisted(() => ({
  mounts: [] as string[],
  unmounts: [] as string[],
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
    onHostsChange: () => () => {}, currentHosts: () => [], setHosts: () => {},
  },
}));

vi.mock("./components/Preview/PreviewHost", async () => {
  const { useEffect } = await import("react");
  return {
    default: ({ stateKey }: { stateKey: string }) => {
      useEffect(() => {
        previewLog.mounts.push(stateKey);
        return () => {
          previewLog.unmounts.push(stateKey);
        };
      }, [stateKey]);
      return <div data-testid="preview-host" data-key={stateKey} />;
    },
  };
});

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
// NOTE: SessionDialog is deliberately NOT mocked — the test drives it.

import App from "./App";

const SIDE_KEY = "side:chat:s1";

function renderApp() {
  return render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  );
}

/** Open a session tab through the real "All sessions" dialog. */
async function openSession(title: string) {
  fireEvent.click(await screen.findByRole("button", { name: /all sessions/i }));
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(within(dialog).getByText(title));
}

const countFor = (list: string[]) => list.filter((key) => key === SIDE_KEY).length;

beforeEach(() => {
  window.localStorage.clear();
  previewLog.mounts.length = 0;
  previewLog.unmounts.length = 0;
  appApi.listProjects.mockReset().mockResolvedValue([{ path: "/proj", name: "proj" }]);
  appApi.getCurrentProject.mockReset().mockResolvedValue({ project: { path: "/proj", name: "proj" } });
  appApi.listProjectSessions.mockReset().mockResolvedValue([
    { id: "s1", title: "One", created_at: "", updated_at: "" },
    { id: "s2", title: "Two", created_at: "", updated_at: "" },
  ]);
  appApi.listGroups.mockReset().mockResolvedValue([]);
  appApi.getSpending.mockReset().mockResolvedValue({ spending_usd: 0 });
});

describe("side preview keeps its mount across reader mode", () => {
  it("moves the preview in and out of reader mode without remounting it", async () => {
    renderApp();
    await openSession("One");
    fireEvent.click(await screen.findByRole("button", { name: /toggle browser panel/i }));
    fireEvent.click(await screen.findByRole("button", { name: "Focus preview (reader mode)" }));

    // Reader mode is on: the side pane's enter button is gone and the reader header is up.
    await screen.findByRole("button", { name: "Exit reader mode" });
    expect(screen.queryByRole("button", { name: "Focus preview (reader mode)" })).toBeNull();
    expect(countFor(previewLog.mounts)).toBe(1);
    expect(countFor(previewLog.unmounts)).toBe(0);

    fireEvent.click(screen.getByRole("button", { name: "Exit reader mode" }));
    await screen.findByRole("button", { name: "Focus preview (reader mode)" });
    expect(screen.queryByRole("button", { name: "Exit reader mode" })).toBeNull();
    expect(countFor(previewLog.mounts)).toBe(1);
    expect(countFor(previewLog.unmounts)).toBe(0);
  });
});
