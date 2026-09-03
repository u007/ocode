// App-level integration coverage for the embedded browser wiring (Part 10):
// the full-width browser tab view and the side panel toggle. All heavy
// children are stubbed; the real providers (Chat/Project/Terminal/BrowserTabs)
// stay live so the wiring under test is genuine.
import { render, screen, fireEvent } from "@testing-library/react";
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

vi.mock("./api/client", () => {
  // The project-store boot path needs a coherent trio; everything else may
  // return a generic empty object (all consumers guard or the call is inert).
  const project = { path: "/proj", name: "proj" };
  const impl: Record<string, () => Promise<unknown>> = {
    listProjects: async () => [project],
    getCurrentProject: async () => ({ project }),
    listProjectSessions: async () => [],
    listGroups: async () => [],
    getSpending: async () => ({ spending_usd: 0 }),
  };
  const api = new Proxy({} as Record<string, unknown>, {
    get: (target, prop: string) => {
      if (!(prop in target)) {
        target[prop] = vi.fn(impl[prop] ?? (async () => ({})));
      }
      return target[prop];
    },
  });
  return {
    api,
    authHeaders: () => ({}),
    authToken: () => null,
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
  eventBus: { on: () => () => {}, emit: () => {}, start: () => {}, stop: () => {}, setProjects: () => {} },
}));

vi.mock("./components/Browser/BrowserPanel", () => ({
  BrowserPanel: ({ stateKey, mode }: { stateKey: string; mode: string }) => (
    <div data-testid="browser-panel" data-key={stateKey} data-mode={mode} />
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
vi.mock("./components/Files/FilePicker", () => ({ default: () => null }));
vi.mock("./components/Files/ConfirmCloseDialog", () => ({ default: () => null }));
vi.mock("./components/Layout/TopTabs", () => ({ default: () => null }));
vi.mock("./pages/SessionPage", () => ({ default: () => null }));
vi.mock("./lib/debug/frontendMemoryReporter", () => ({ default: () => null }));
vi.mock("./components/common/ErrorBoundary", () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

import App from "./App";

function renderApp() {
  return render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  window.localStorage.clear();
});

describe("App browser wiring", () => {
  it("shows a full-width BrowserPanel when a browser tab is focused", async () => {
    renderApp();
    fireEvent.click(await screen.findByRole("button", { name: /new browser tab/i }));
    const panel = await screen.findByTestId("browser-panel");
    expect(panel).toHaveAttribute("data-mode", "full");
    expect(panel.getAttribute("data-key")).toMatch(/^tab:/);
  });

  it("toggles the side browser panel open beside a chat session", async () => {
    renderApp();
    // The toggle needs a focused chat session (side panel keys by its tab).
    fireEvent.click(await screen.findByRole("button", { name: /new chat session/i }));
    const toggle = await screen.findByRole("button", { name: /toggle browser panel/i });
    expect(toggle).toBeEnabled();
    fireEvent.click(toggle);
    const panels = await screen.findAllByTestId("browser-panel");
    const side = panels.find((p) => p.getAttribute("data-mode") === "side");
    expect(side).toBeTruthy();
    expect(side!.getAttribute("data-key")).toMatch(/^side:chat:/);
  });

  it("disables the side-panel toggle while a full-width browser tab is focused", async () => {
    renderApp();
    const toggle = await screen.findByRole("button", { name: /toggle browser panel/i });
    // No chat tab yet → nothing for the side panel to accompany.
    expect(toggle).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: /new browser tab/i }));
    expect(toggle).toBeDisabled();
  });

  it("collapses the side browser panel to a rail and expands it back", async () => {
    renderApp();
    fireEvent.click(await screen.findByRole("button", { name: /new chat session/i }));
    fireEvent.click(await screen.findByRole("button", { name: /toggle browser panel/i }));
    expect(await screen.findByTestId("browser-panel")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /collapse browser panel/i }));
    // Collapsed: panel body unmounted, only the expand rail remains.
    expect(screen.queryByTestId("browser-panel")).toBeNull();
    expect(screen.queryByRole("button", { name: /collapse browser panel/i })).toBeNull();

    fireEvent.click(await screen.findByRole("button", { name: /show browser panel/i }));
    expect(await screen.findByTestId("browser-panel")).toHaveAttribute("data-mode", "side");
    expect(screen.queryByRole("button", { name: /show browser panel/i })).toBeNull();
  });

  it("closes the side browser panel via its close button", async () => {
    renderApp();
    fireEvent.click(await screen.findByRole("button", { name: /new chat session/i }));
    fireEvent.click(await screen.findByRole("button", { name: /toggle browser panel/i }));
    fireEvent.click(await screen.findByRole("button", { name: /close browser panel/i }));
    expect(screen.queryByTestId("browser-panel")).toBeNull();
    // The top-row globe toggle still opens it again.
    expect(screen.queryByRole("button", { name: /toggle browser panel/i })).toBeEnabled();
  });
});
