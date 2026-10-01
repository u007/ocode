// App-level wiring for the "something is running" composer bar. App owns the
// single `await dispatchCommand(...)` every client-side slash command passes
// through, so this test drives the real wrapper via CommandPalette's onExecute
// (the same callback the composer uses) and pins that activity is recorded for
// the duration of the handler and released afterwards.
//
// This is the assertion that stops the store and the bar from being two
// perfect, mutually unaware components: without the wrapper nothing ever calls
// setCommandActivity and every other test here would still pass.
import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
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
  recapSession: vi.fn(),
}));

vi.mock("./api/client", () => {
  const impl: Record<string, unknown> = {
    listProjects: appApi.listProjects,
    getCurrentProject: appApi.getCurrentProject,
    listProjectSessions: appApi.listProjectSessions,
    listGroups: appApi.listGroups,
    getSpending: appApi.getSpending,
    recapSession: appApi.recapSession,
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

// Capture the onExecute callback App hands the command palette.
const palette = vi.hoisted(() => ({ onExecute: null as null | ((cmd: string, id?: string | null) => unknown) }));
vi.mock("./components/common/CommandPalette", () => ({
  default: ({ onExecute }: { onExecute?: (cmd: string, id?: string | null) => unknown }) => {
    palette.onExecute = onExecute ?? null;
    return (
      <div data-testid="command-palette">
        <button type="button" data-testid="run-recap" onClick={() => onExecute?.("/recap", "ses_1")}>
          recap
        </button>
      </div>
    );
  },
}));

vi.mock("./components/Chat/ChatPanel", () => ({ default: () => null }));
vi.mock("./components/Preview/PreviewHost", () => ({ default: () => null }));
vi.mock("./components/Browser/BrowserPanel", () => ({ BrowserPanel: () => null }));
vi.mock("./components/Chat/AgentPreview", () => ({ default: () => null }));
vi.mock("./components/Agents/AgentsPanel", () => ({ default: () => null }));
vi.mock("./components/Chat/ChatInput", () => ({ default: () => null }));
vi.mock("./components/common/StatusBar", () => ({ default: () => null }));
vi.mock("./components/Status/StatusPanel", () => ({ default: () => null }));
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
import { __resetSessionActivityForTests, getSessionActivity } from "./lib/commandActivity";

/** A promise the test resolves by hand, so the handler can be held open. */
function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => { resolve = r; });
  return { promise, resolve };
}

beforeEach(() => {
  window.localStorage.clear();
  palette.onExecute = null;
  __resetSessionActivityForTests();
  appApi.listProjects.mockReset().mockResolvedValue([{ path: "/proj", name: "proj" }]);
  appApi.getCurrentProject.mockReset().mockResolvedValue({ project: { path: "/proj", name: "proj" } });
  appApi.listProjectSessions.mockReset().mockResolvedValue([]);
  appApi.listGroups.mockReset().mockResolvedValue([]);
  appApi.getSpending.mockReset().mockResolvedValue({ spending_usd: 0 });
});

describe("App command activity wrapper", () => {
  it("records activity for a slow command and releases it when the handler settles", async () => {
    const gate = deferred<{ recap: string }>();
    appApi.recapSession.mockReset().mockReturnValue(gate.promise);

    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );
    fireEvent.click(await screen.findByRole("button", { name: /new chat session/i }));
    await screen.findByTestId("command-palette");
    expect(palette.onExecute).toBeTypeOf("function");

    // Fire without awaiting: the handler is now parked inside the wrapper's
    // try block, awaiting the server.
    act(() => {
      void palette.onExecute?.("/recap", "ses_1");
    });

    await waitFor(() => {
      const activity = getSessionActivity("ses_1");
      expect(activity?.kind).toBe("command");
      if (activity?.kind === "command") expect(activity.label).toBe("/recap");
    });

    // Resolving the server call must release the bar.
    await act(async () => {
      gate.resolve({ recap: "done" });
      await gate.promise;
    });
    await waitFor(() => expect(getSessionActivity("ses_1")).toBeUndefined());
  });

  it("releases the bar even when the command throws", async () => {
    appApi.recapSession.mockReset().mockRejectedValue(new Error("server exploded"));

    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );
    fireEvent.click(await screen.findByRole("button", { name: /new chat session/i }));
    await screen.findByTestId("command-palette");

    await act(async () => {
      await palette.onExecute?.("/recap", "ses_1");
    });

    // handleRecap converts the throw into a message, so the wrapper's finally
    // must still have run — a leaked bar would stick forever.
    await waitFor(() => expect(getSessionActivity("ses_1")).toBeUndefined());
  });
});