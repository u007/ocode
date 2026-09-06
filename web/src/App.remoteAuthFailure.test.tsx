// App-level coverage for the remote-session auth-failure guard (whole-branch
// review fix Important 7): `isRemoteSession() && !authToken()` alone only
// catches "no token at all". This covers the extended guard —
// `isRemoteSession() && (!authToken() || remoteAuthFailed)` — where
// remoteAuthFailed flips via the reportAuthFailure → setAuthFailureHandler
// wiring in client.ts when a remote-mode API call 401s with a still-cached
// (now stale) token. All heavy children are stubbed, mirroring
// App.browser.test.tsx's mocking pattern.
import { render, screen, act } from "@testing-library/react";
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

const authState = vi.hoisted(() => ({
  isRemote: false,
  token: null as string | null,
  handler: null as (() => void) | null,
}));

vi.mock("./api/client", () => {
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
    authToken: () => authState.token,
    isRemoteSession: () => authState.isRemote,
    setAuthFailureHandler: (fn: (() => void) | null) => {
      authState.handler = fn;
    },
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

const eventBusStop = vi.hoisted(() => vi.fn());
vi.mock("./lib/eventBus", () => ({
  eventBus: { on: () => () => {}, emit: () => {}, start: () => {}, stop: eventBusStop, setProjects: () => {} },
}));

vi.mock("./components/Browser/BrowserPanel", () => ({
  BrowserPanel: () => <div data-testid="browser-panel" />,
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
  authState.isRemote = false;
  authState.token = null;
  authState.handler = null;
  eventBusStop.mockClear();
});

describe("App remote auth-failure guard", () => {
  it("shows RemoteReconnect when isRemoteSession is true and no token was ever present", () => {
    authState.isRemote = true;
    authState.token = null;
    renderApp();
    expect(screen.getByText(/reconnect/i)).toBeInTheDocument();
  });

  it("renders the normal app for a remote session with a valid token", async () => {
    authState.isRemote = true;
    authState.token = "valid-token";
    renderApp();
    expect(await screen.findByRole("button", { name: /new chat session/i })).toBeInTheDocument();
    expect(screen.queryByText(/reconnect/i)).not.toBeInTheDocument();
  });

  it("switches to RemoteReconnect and stops the event bus when the auth-failure handler fires", async () => {
    authState.isRemote = true;
    authState.token = "valid-token";
    renderApp();
    await screen.findByRole("button", { name: /new chat session/i });
    expect(authState.handler).toBeInstanceOf(Function);

    act(() => {
      authState.handler?.();
    });

    expect(await screen.findByText(/reconnect/i)).toBeInTheDocument();
    expect(eventBusStop).toHaveBeenCalled();
  });

  it("does not register an auth-failure handler for a non-remote session", async () => {
    authState.isRemote = false;
    authState.token = null;
    renderApp();
    await screen.findByRole("button", { name: /new chat session/i });
    expect(authState.handler).toBeNull();
  });
});
