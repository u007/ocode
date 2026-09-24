// App-level regression for per-session side-pane scoping: opening the
// "Browser / Preview" pane in one chat session must NOT open it in another.
// The real SessionDialog is kept live so two session tabs can be opened the
// way a user does (All sessions → pick), which is the only way to have two
// distinct chat tabs without sending a message. All heavy children are stubbed.
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
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

const sidePanels = () =>
  screen.queryAllByTestId("browser-panel").filter((p) => p.getAttribute("data-mode") === "side");

beforeEach(() => {
  window.localStorage.clear();
  appApi.listProjects.mockReset().mockResolvedValue([{ path: "/proj", name: "proj" }]);
  appApi.getCurrentProject.mockReset().mockResolvedValue({ project: { path: "/proj", name: "proj" } });
  appApi.listProjectSessions.mockReset().mockResolvedValue([
    { id: "s1", title: "One", created_at: "", updated_at: "" },
    { id: "s2", title: "Two", created_at: "", updated_at: "" },
  ]);
  appApi.listGroups.mockReset().mockResolvedValue([]);
  appApi.getSpending.mockReset().mockResolvedValue({ spending_usd: 0 });
});

describe("App side pane is scoped per chat session", () => {
  it("does not open the side pane in another chat, and restores it on return", async () => {
    renderApp();

    // Open chat session One and turn on its side pane.
    await openSession("One");
    const toggle = await screen.findByRole("button", { name: /toggle browser panel/i });
    fireEvent.click(toggle);
    await waitFor(() => expect(sidePanels()).toHaveLength(1));
    expect(sidePanels()[0].getAttribute("data-key")).toBe("side:chat:s1");

    // Switch to session Two (same project). Its pane must stay closed — the
    // pane is per-session, not "enabled for all chat".
    await openSession("Two");
    await waitFor(() => expect(sidePanels()).toHaveLength(0));

    // Back to One: its pane is restored.
    await openSession("One");
    await waitFor(() => expect(sidePanels()).toHaveLength(1));
    expect(sidePanels()[0].getAttribute("data-key")).toBe("side:chat:s1");
  });
});
