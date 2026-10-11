// App-level wiring for the /btw docked panel: a `btw` command result must call
// startBtw, and BtwPanel must be mounted in the active chat tab. The command
// handler itself (result.btw) is covered by commands.btw.test.tsx, and the
// store by btwStore.test.ts, so this file isolates the App seam.
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
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
    getBrowseBase: vi.fn(async () => "http://browse.test"),
    mintBrowseGrant: vi.fn(async () => "G1"),
    revokeBrowseSession: vi.fn(async () => {}),
    browseSrc: () => "",
  };
});

vi.mock("./lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {}, emit: () => {}, start: () => {}, stop: () => {}, setProjects: () => {}, setHosts: () => {} },
}));

// The btw store is mocked so the App seam is isolated: startBtw is asserted,
// and useBtwState drives BtwPanel without any bus traffic.
const btw = vi.hoisted(() => ({
  startBtw: vi.fn(),
  rekeyBtw: vi.fn(),
  closeBtw: vi.fn(),
}));
vi.mock("./lib/btwStore", () => ({
  startBtw: btw.startBtw,
  rekeyBtw: btw.rekeyBtw,
  closeBtw: btw.closeBtw,
  useBtwState: () => ({
    sessionId: "ses_1",
    question: "use tabs",
    generation: 1,
    activity: [],
    answer: "42",
    loading: false,
    open: true,
  }),
}));

// The command handler is mocked to return a `btw` effect directly.
vi.mock("./components/Chat/commands", () => ({
  dispatchCommand: vi.fn(async () => ({
    handled: true,
    btw: { sessionId: "ses_1", question: "use tabs", host: undefined },
  })),
}));

vi.mock("./components/Chat/ChatInput", () => ({
  default: ({ onSlashCommand }: { onSlashCommand?: (cmd: string) => void }) => (
    <button type="button" data-testid="slash-btw" onClick={() => onSlashCommand?.("/btw use tabs")}>
      btw
    </button>
  ),
}));

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

beforeEach(() => {
  window.localStorage.clear();
  btw.startBtw.mockClear();
  btw.rekeyBtw.mockClear();
  btw.closeBtw.mockClear();
  appApi.listProjects.mockReset().mockResolvedValue([{ path: "/proj", name: "proj" }]);
  appApi.getCurrentProject.mockReset().mockResolvedValue({ project: { path: "/proj", name: "proj" } });
  appApi.listProjectSessions.mockReset().mockResolvedValue([]);
  appApi.listGroups.mockReset().mockResolvedValue([]);
  appApi.getSpending.mockReset().mockResolvedValue({ spending_usd: 0 });
});

describe("App /btw panel wiring", () => {
  it("opens the panel when /btw returns a btw effect", async () => {
    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );
    fireEvent.click(await screen.findByRole("button", { name: /new chat session/i }));
    fireEvent.click(await screen.findByTestId("slash-btw"));

    await waitFor(() => expect(btw.startBtw).toHaveBeenCalledWith("ses_1", undefined, "use tabs"));
    const panel = await screen.findByTestId("btw-panel");
    expect(panel.textContent).toContain("use tabs");
    expect(panel.textContent).toContain("42");
  });
});
