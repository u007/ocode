// Regression coverage for the session-scoped ask-dialog gate: a pending
// permission/question ask belongs to its chat session, so its dialog may only
// mount while that session's Chat sub-tab is actually on screen. Anywhere else
// (another top-level view, the terminal half, a non-chat sub-tab) it must stay
// unmounted — off-surface the chat panel is `display:none`, so a confined ask
// would render invisibly with no signal at all.
//
// The dialog stubs portal into the `container` they are handed, so this file
// covers the gate AND the containment wiring (the ask lands in the chat
// surface, not on the viewport).
//
// Heavy children are stubbed; the real providers (Chat/Project/Terminal) and
// the real HomeApp view/sub-tab wiring stay live, so the gate under test is
// genuine. `useChat` is mocked to supply a constant pending ask.
import { act, render, screen, waitFor } from "@testing-library/react";
import { createPortal } from "react-dom";
import { describe, it, expect, vi, beforeAll, beforeEach } from "vitest";
import { MemoryRouter } from "react-router-dom";

const appApi = vi.hoisted(() => ({
  listProjects: vi.fn(),
  getCurrentProject: vi.fn(),
  listProjectSessions: vi.fn(),
  listGroups: vi.fn(),
  getSpending: vi.fn(),
  getTabs: vi.fn(),
}));

const chatMock = vi.hoisted(() => ({
  pendingPermission: null as unknown,
  pendingQuestion: null as unknown,
  hiddenQuestionRequestId: null as string | null,
}));

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

vi.mock("./api/client", () => {
  const impl: Record<string, unknown> = {
    listProjects: appApi.listProjects,
    getCurrentProject: appApi.getCurrentProject,
    listProjectSessions: appApi.listProjectSessions,
    listGroups: appApi.listGroups,
    getSpending: appApi.getSpending,
    getTabs: appApi.getTabs,
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

// The ask under test — the dialogs themselves are stubbed to mount markers.
vi.mock("./hooks/useChat", () => ({
  useChat: () => ({
    resolvePermission: async () => ({ ok: true }),
    pendingPermission: chatMock.pendingPermission,
    pendingQuestion: chatMock.pendingQuestion,
    hiddenQuestionRequestId: chatMock.hiddenQuestionRequestId,
    askContext: null,
    submitQuestionAnswers: async () => true,
    hideQuestion: vi.fn(),
    cancelQuestion: async () => true,
  }),
}));

// The stubs portal into the `container` they are HANDED, so these suites also
// prove the containment wiring and not just the gate: if App.tsx passed `null`
// or the wrong element, the marker would not appear inside the chat surface.
// (The real dialogs do the same portal; PermissionDialog/QuestionDialog suites
// cover that side.)
vi.mock("./components/Chat/PermissionDialog", () => ({
  default: ({ container }: { container: HTMLElement | null }) =>
    container ? createPortal(<div data-testid="permission-dialog" />, container) : null,
}));
vi.mock("./components/Chat/QuestionDialog", () => ({
  default: ({ container }: { container: HTMLElement | null }) =>
    container ? createPortal(<div data-testid="question-dialog" />, container) : null,
}));

// Per-sub-tab markers: their presence proves the tab list restored AND the
// active sub-tab pane mounted, so an absent ask dialog is a real gate, not a
// still-loading app.
vi.mock("./components/Chat/ChatPanel", () => ({ default: () => <div data-testid="chat-panel" /> }));
vi.mock("./components/Changes/ChangesPanel", () => ({ default: () => <div data-testid="changes-panel" /> }));

vi.mock("./components/Chat/AgentPreview", () => ({ default: () => null }));
vi.mock("./components/Agents/AgentsPanel", () => ({ default: () => null }));
vi.mock("./components/Chat/ChatInput", () => ({ default: () => null }));
vi.mock("./components/common/StatusBar", () => ({ default: () => null }));
vi.mock("./components/Status/StatusPanel", () => ({ default: () => null }));
vi.mock("./components/common/CommandPalette", () => ({ default: () => null }));
vi.mock("./components/common/AttentionSoundBridge", () => ({ default: () => null }));
vi.mock("./components/common/ActionErrorToast", () => ({ default: () => null }));
vi.mock("./components/Git/GitPanel", () => ({ default: () => null }));
vi.mock("./components/Files/FileTree", () => ({ default: () => null }));
vi.mock("./components/Files/FileTabContent", () => ({ default: () => null }));
vi.mock("./components/Files/FilePicker", () => ({ default: () => null }));
vi.mock("./components/Files/ConfirmCloseDialog", () => ({ default: () => null }));
vi.mock("./components/Logs/LogPanel", () => ({ default: () => null }));
vi.mock("./components/Terminal/TerminalTabs", () => ({ default: () => null }));
vi.mock("./components/Assets/AssetsPanel", () => ({ default: () => null }));
vi.mock("./components/Cron/CronPanel", () => ({ default: () => null }));
vi.mock("./components/Preview/PreviewHost", () => ({ default: () => null }));
vi.mock("./components/Preview/PreviewTabPage", () => ({ default: () => null }));
// Renders a marker so the containment suite can prove this interactive banner
// stays clickable — i.e. OUTSIDE the ask's confined surface.
vi.mock("./components/Chat/RemoteVersionBanner", () => ({
  default: () => <div data-testid="version-banner" />,
}));
vi.mock("./components/Settings/SettingsPanel", () => ({ default: () => null }));
vi.mock("./components/Layout/TopTabs", () => ({ default: () => null }));
vi.mock("./components/Layout/ProjectSidebar", () => ({ default: () => null }));
// Renders a marker (not null) so the containment suite can prove the sub-tab
// bar sits OUTSIDE the confined chat surface.
vi.mock("./components/Layout/SessionSubTabs", () => ({
  default: () => <div data-testid="session-sub-tabs" />,
}));
vi.mock("./components/Layout/SessionTabSync", () => ({ default: () => null }));
vi.mock("./components/Layout/CoworkSidebar", () => ({ default: () => null }));
vi.mock("./components/Layout/SessionDialog", () => ({ default: () => null }));
vi.mock("./components/Layout/ModelDialog", () => ({ default: () => null }));
vi.mock("./components/Layout/ShareDialog", () => ({ default: () => null }));
vi.mock("./components/Layout/EditorTabBar", () => ({ default: () => null }));
vi.mock("./pages/SessionPage", () => ({ default: () => null }));
vi.mock("./lib/debug/frontendMemoryReporter", () => ({ default: () => null }));
vi.mock("./components/common/ErrorBoundary", () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

import { tabFocusActions } from "./lib/tabFocus";
import App from "./App";

function renderApp() {
  return render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  );
}

/** Per-project persisted view state, read by HomeApp's restore layout effect. */
const viewState = (view: string, focusedKind: string) =>
  JSON.stringify({ version: 1, projects: { "/proj": { view, focusedKind } } });

/** Server tab snapshot: one active session tab on the given sub-tab. */
const sessionTabs = (subTab: string) => ({
  projects: {
    "/proj": {
      tabs: [{ id: "s1", title: "Session 1", sub_tab: subTab }],
      active: "s1",
    },
  },
});

const mainAttr = (attr: string) => document.querySelector("main")?.getAttribute(attr);

const PENDING_PERMISSION = { request_id: "req-1", tool: "bash", command: "ls" };
const PENDING_QUESTION = { request_id: "q-1", questions: [] };

beforeEach(() => {
  window.localStorage.clear();
  tabFocusActions.clear();
  chatMock.pendingPermission = PENDING_PERMISSION;
  chatMock.pendingQuestion = PENDING_QUESTION;
  chatMock.hiddenQuestionRequestId = null;
  appApi.listProjects.mockReset().mockResolvedValue([{ path: "/proj", name: "proj" }]);
  appApi.getCurrentProject.mockReset().mockResolvedValue({ project: { path: "/proj", name: "proj" } });
  appApi.listProjectSessions.mockReset().mockResolvedValue([]);
  appApi.listGroups.mockReset().mockResolvedValue([]);
  appApi.getSpending.mockReset().mockResolvedValue({ spending_usd: 0 });
  appApi.getTabs.mockReset().mockResolvedValue(sessionTabs("chat"));
});

describe("ask dialogs are confined to the active session's chat surface", () => {
  /**
   * The element the asks are confined to. Located by its test id rather than by
   * DOM shape: the surface is a nested flex column, and guessing at classes
   * broke the moment the banner moved out of it.
   */
  const chatSurface = (): HTMLElement => screen.getByTestId("chat-ask-surface");

  it("mounts both asks INSIDE the chat tab wrapper, not on the viewport", async () => {
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("sessions", "chat"));
    renderApp();

    await screen.findByTestId("permission-dialog");
    await screen.findByTestId("question-dialog");

    const surface = chatSurface();
    expect(surface.contains(screen.getByTestId("permission-dialog"))).toBe(true);
    expect(surface.contains(screen.getByTestId("question-dialog"))).toBe(true);
    // The portal target IS the surface — not <body>, which is what a
    // viewport-level dialog would have used.
    expect(screen.getByTestId("permission-dialog").parentElement).toBe(surface);
    expect(document.body.contains(surface)).toBe(true);
  });

  it("keeps the session's sub-tab bar OUTSIDE the confined surface", async () => {
    // SessionSubTabs renders ABOVE the tab map, so the user can still leave the
    // ask (switch sub-tab / session) instead of being walled in.
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("sessions", "chat"));
    renderApp();

    await screen.findByTestId("permission-dialog");
    const surface = chatSurface();
    const subTabs = screen.getByTestId("session-sub-tabs");
    expect(surface.contains(subTabs)).toBe(false);
    expect(document.body.contains(subTabs)).toBe(true);
  });

  it("keeps the interactive version-mismatch banner OUTSIDE the confined surface", async () => {
    // The banner has an expand toggle and an Update action, so covering it would
    // make the one piece of session chrome a pending ask can act on unreachable.
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("sessions", "chat"));
    renderApp();

    await screen.findByTestId("permission-dialog");
    const surface = chatSurface();
    const banner = screen.getByTestId("version-banner");
    expect(surface.contains(banner)).toBe(false);
    // It is a SIBLING of the surface, inside the same tab wrapper.
    expect(banner.parentElement).toBe(surface.parentElement);
  });
});

describe("session-scoped ask dialogs", () => {
  it("shows the ask on the session's Chat sub-tab", async () => {
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("sessions", "chat"));
    appApi.getTabs.mockResolvedValue(sessionTabs("chat"));
    renderApp();

    await screen.findByTestId("chat-panel");
    expect(await screen.findByTestId("permission-dialog")).toBeInTheDocument();
    expect(screen.getByTestId("question-dialog")).toBeInTheDocument();
  });

  it("does not mount the ask while another top-level view is showing", async () => {
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("files", "chat"));
    renderApp();

    await waitFor(() => expect(mainAttr("data-active-view")).toBe("files"));
    // The chat pane still mounts off-view (forceMount), proving tabs restored.
    await screen.findByTestId("chat-panel");

    expect(screen.queryByTestId("permission-dialog")).toBeNull();
    expect(screen.queryByTestId("question-dialog")).toBeNull();
  });

  it("does not mount the ask while the terminal half of Sessions is focused", async () => {
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("sessions", "terminal"));
    renderApp();

    await waitFor(() => expect(mainAttr("data-focused-kind")).toBe("terminal"));
    await screen.findByTestId("chat-panel");

    expect(screen.queryByTestId("permission-dialog")).toBeNull();
    expect(screen.queryByTestId("question-dialog")).toBeNull();
  });

  it("does not mount the ask on a non-chat sub-tab of the same session", async () => {
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("sessions", "chat"));
    appApi.getTabs.mockResolvedValue(sessionTabs("changes"));
    renderApp();

    await screen.findByTestId("changes-panel");

    expect(screen.queryByTestId("permission-dialog")).toBeNull();
    expect(screen.queryByTestId("question-dialog")).toBeNull();
  });

  it("re-opens the ask once the user returns to the session's Chat sub-tab", async () => {
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("files", "chat"));
    renderApp();

    await screen.findByTestId("chat-panel");
    expect(screen.queryByTestId("permission-dialog")).toBeNull();

    act(() => tabFocusActions.request({ kind: "chat", projectPath: "/proj" }));

    await waitFor(() => expect(mainAttr("data-active-view")).toBe("sessions"));
    expect(await screen.findByTestId("permission-dialog")).toBeInTheDocument();
    expect(screen.getByTestId("question-dialog")).toBeInTheDocument();
  });

  it("does not mount a locally hidden question on the Chat surface", async () => {
    chatMock.hiddenQuestionRequestId = PENDING_QUESTION.request_id;
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("sessions", "chat"));
    renderApp();

    await screen.findByTestId("chat-panel");
    expect(await screen.findByTestId("permission-dialog")).toBeInTheDocument();
    expect(screen.queryByTestId("question-dialog")).toBeNull();
  });

  it("keeps a locally hidden question hidden off-surface and after returning", async () => {
    chatMock.hiddenQuestionRequestId = PENDING_QUESTION.request_id;
    window.localStorage.setItem("ocode.ui.view-state.v1", viewState("files", "chat"));
    renderApp();

    await waitFor(() => expect(mainAttr("data-active-view")).toBe("files"));
    await screen.findByTestId("chat-panel");
    expect(screen.queryByTestId("question-dialog")).toBeNull();

    act(() => tabFocusActions.request({ kind: "chat", projectPath: "/proj" }));

    await waitFor(() => expect(mainAttr("data-active-view")).toBe("sessions"));
    expect(await screen.findByTestId("permission-dialog")).toBeInTheDocument();
    expect(screen.queryByTestId("question-dialog")).toBeNull();
  });
});
