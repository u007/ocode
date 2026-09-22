// App-level wiring for the interrupted-turn Continue action: ChatPanel owns no
// send path, so it calls the stable `onContinueInterrupted(sessionId)` prop App
// hands down, and App re-sends through the normal message path with the literal
// text "continue". This test captures the prop and pins both halves of that
// contract. Heavy children are stubbed, mirroring App.previewActivation.test.tsx.
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
  sendMessage: vi.fn(),
  chat: vi.fn(),
}));

vi.mock("./api/client", () => {
  const impl: Record<string, unknown> = {
    listProjects: appApi.listProjects,
    getCurrentProject: appApi.getCurrentProject,
    listProjectSessions: appApi.listProjectSessions,
    listGroups: appApi.listGroups,
    getSpending: appApi.getSpending,
    sendMessage: appApi.sendMessage,
    chat: appApi.chat,
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
    browseSrc: (base: string, grant: string | null, key: string) =>
      `${base}/b/${key}/${grant ? "g" : "n"}`,
  };
});

vi.mock("./lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {}, emit: () => {}, start: () => {}, stop: () => {}, setProjects: () => {}, setHosts: () => {} },
}));

// Capture the Continue callback App hands ChatPanel, and expose a button that
// invokes it exactly as the real notice's Continue button does.
const panel = vi.hoisted(() => ({ onContinue: null as null | ((id: string) => void) }));
vi.mock("./components/Chat/ChatPanel", () => ({
  default: ({ onContinueInterrupted }: { onContinueInterrupted?: (id: string) => void }) => {
    panel.onContinue = onContinueInterrupted ?? null;
    return (
      <div data-testid="chat-panel">
        <button type="button" data-testid="continue" onClick={() => onContinueInterrupted?.("ses_continue-test")}>
          continue
        </button>
      </div>
    );
  },
}));

vi.mock("./components/Preview/PreviewHost", () => ({ default: () => null }));
vi.mock("./components/Browser/BrowserPanel", () => ({ BrowserPanel: () => null }));
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
  panel.onContinue = null;
  appApi.listProjects.mockReset().mockResolvedValue([{ path: "/proj", name: "proj" }]);
  appApi.getCurrentProject.mockReset().mockResolvedValue({ project: { path: "/proj", name: "proj" } });
  appApi.listProjectSessions.mockReset().mockResolvedValue([]);
  appApi.listGroups.mockReset().mockResolvedValue([]);
  appApi.getSpending.mockReset().mockResolvedValue({ spending_usd: 0 });
  appApi.sendMessage.mockReset().mockResolvedValue({});
  appApi.chat.mockReset().mockResolvedValue({ sessionId: "ses_new" });
});

describe("App Continue wiring", () => {
  it("re-sends the interrupted turn through the normal message path with 'continue'", async () => {
    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );
    // A chat tab must exist for ChatPanel (and so the prop) to mount.
    fireEvent.click(await screen.findByRole("button", { name: /new chat session/i }));
    await screen.findByTestId("chat-panel");

    expect(panel.onContinue).toBeTypeOf("function");
    fireEvent.click(screen.getByTestId("continue"));

    await waitFor(() => expect(appApi.sendMessage).toHaveBeenCalledWith("ses_continue-test", "continue", undefined));
  });
});
