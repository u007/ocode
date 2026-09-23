// App-level wiring for sidebar preview activation: the `ocode:open-preview`
// event (file tree / AI tool) must open the side panel (Browser / Preview) and
// hand the request to PreviewHost, together with the one-shot `consume`
// callback. All heavy children are stubbed, mirroring App.browser.test.tsx.
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
    browseSrc: (base: string, grant: string | null, key: string) =>
      `${base}/b/${key}/${grant ? "g" : "n"}`,
  };
});

vi.mock("./lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {}, emit: () => {}, start: () => {}, stop: () => {}, setProjects: () => {}, setHosts: () => {} },
}));

// Capture what App passes to the sidebar PreviewHost, and expose a button that
// exercises the real `consume` callback.
const host = vi.hoisted(() => ({
  path: null as string | null,
  nonce: 0,
  consume: null as null | (() => void),
  mounted: false,
}));
vi.mock("./components/Preview/PreviewHost", () => ({
  default: ({ request, nonce, onConsumeActivation }: { request: { path: string } | null; nonce: number; onConsumeActivation?: () => void }) => {
    host.path = request?.path ?? null;
    host.nonce = nonce;
    host.consume = onConsumeActivation ?? null;
    host.mounted = true;
    return (
      <div data-testid="preview-host" data-path={request?.path ?? ""} data-nonce={String(nonce)}>
        <button type="button" data-testid="preview-consume" onClick={() => onConsumeActivation?.()}>
          consume
        </button>
      </div>
    );
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

// Capture what App passes to the full-width Preview session sub-tab page. On
// mobile the side pane is hidden, so a preview activation must land here.
const previewTab = vi.hoisted(() => ({ mounted: false, path: null as string | null, nonce: 0 }));
vi.mock("./components/Preview/PreviewTabPage", () => ({
  default: ({ request, nonce }: { request?: { path: string } | null; nonce?: number }) => {
    previewTab.mounted = true;
    previewTab.path = request?.path ?? null;
    previewTab.nonce = nonce ?? 0;
    return <div data-testid="preview-tab-page" data-path={request?.path ?? ""} data-nonce={String(nonce ?? 0)} />;
  },
}));

import App from "./App";
import { dispatchOpenPreview } from "./lib/previewKind";

/** Swap the matchMedia stub so `useIsMobile()` resolves the given value. */
function setMobileMatchMedia(mobile: boolean) {
  window.matchMedia = ((media: string) => ({
    matches: mobile,
    media,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
}

beforeEach(() => {
  window.localStorage.clear();
  host.path = null;
  host.nonce = 0;
  host.consume = null;
  host.mounted = false;
  previewTab.mounted = false;
  previewTab.path = null;
  previewTab.nonce = 0;
  appApi.listProjects.mockReset().mockResolvedValue([{ path: "/proj", name: "proj" }]);
  appApi.getCurrentProject.mockReset().mockResolvedValue({ project: { path: "/proj", name: "proj" } });
  appApi.listProjectSessions.mockReset().mockResolvedValue([]);
  appApi.listGroups.mockReset().mockResolvedValue([]);
  appApi.getSpending.mockReset().mockResolvedValue({ spending_usd: 0 });
});

describe("App side-pane visibility scoping", () => {
  it("mounts the pane only while the session tab is on the chat sub-tab", async () => {
    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );
    fireEvent.click(await screen.findByRole("button", { name: /new chat session/i }));
    dispatchOpenPreview("docs/spec.md", 1, "/proj");
    await screen.findByTestId("preview-host");
    expect(host.mounted).toBe(true);
  });
});

describe("App sidebar preview activation", () => {
  it("opens the side panel and passes the request + nonce + consume to PreviewHost", async () => {
    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );
    await waitFor(() => expect(appApi.getCurrentProject).toHaveBeenCalled());
    // The side panel accompanies a focused chat session (it is keyed by that
    // session's tab), so a session tab must exist first.
    fireEvent.click(await screen.findByRole("button", { name: /new chat session/i }));
    await waitFor(() => expect(screen.queryByTestId("preview-host")).toBeNull());

    dispatchOpenPreview("docs/spec.md", 1, "/proj");
    const panel = await screen.findByTestId("preview-host");
    expect(panel.getAttribute("data-path")).toBe("docs/spec.md");
    expect(Number(panel.getAttribute("data-nonce"))).toBeGreaterThan(0);
    // The consume callback is threaded through so the activation is one-shot.
    expect(host.consume).toBeTypeOf("function");
  });

  it("clears the pending activation when consumed (no replay on later renders)", async () => {
    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );
    fireEvent.click(await screen.findByRole("button", { name: /new chat session/i }));
    dispatchOpenPreview("docs/spec.md", 1, "/proj");
    await screen.findByTestId("preview-host");
    const nonceBefore = Number(screen.getByTestId("preview-host").getAttribute("data-nonce"));

    fireEvent.click(screen.getByTestId("preview-consume"));
    await waitFor(() => expect(screen.getByTestId("preview-host").getAttribute("data-path")).toBe(""));
    // Nonce resets with the cleared activation, so a later event must produce a
    // strictly larger nonce than the consumed one.
    dispatchOpenPreview("docs/other.md", 1, "/proj");
    await waitFor(() =>
      expect(Number(screen.getByTestId("preview-host").getAttribute("data-nonce"))).toBeGreaterThan(nonceBefore),
    );
  });
});

describe("App side pane on mobile", () => {
  it("never mounts the side pane, hides its toggle, and routes the preview activation to the Preview sub-tab", async () => {
    setMobileMatchMedia(true);
    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );
    fireEvent.click(await screen.findByRole("button", { name: /new chat session/i }));

    // The side pane's toggle is desktop-only; the browser lives in the tab
    // strip on phones.
    expect(screen.queryByRole("button", { name: /toggle browser panel/i })).toBeNull();

    dispatchOpenPreview("docs/spec.md", 3, "/proj");

    // The activation lands in the full-width Preview sub-tab (which becomes
    // active), NOT in the side pane.
    await waitFor(() => expect(previewTab.path).toBe("docs/spec.md"));
    expect(previewTab.nonce).toBeGreaterThan(0);
    expect(screen.getByTestId("preview-tab-page")).toBeTruthy();
    expect(screen.queryByTestId("preview-host")).toBeNull();
    expect(host.mounted).toBe(false);

    setMobileMatchMedia(false);
  });
});