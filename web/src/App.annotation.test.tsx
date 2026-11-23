// App owns the window listener for browser annotations: the browser surfaces
// only dispatch `ocode:annotation` with their state key, and App resolves which
// chat composer owns it. These tests pin that routing (the composer half lives
// in Chat/ChatInput.annotation.test.tsx). The real ChatInput is replaced by a
// stub that registers its imperative handle, so the assertion is that App
// reaches the RIGHT composer — not that the note renders.
import { render, screen, waitFor, act, fireEvent } from "@testing-library/react";
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
  eventBus: {
    on: () => () => {},
    onReconnect: () => () => {},
    emit: () => {},
    start: () => {},
    stop: () => {},
    setProjects: () => {},
    onHostsChange: () => () => {},
    currentHosts: () => [],
    setHosts: () => {},
  },
}));

// The composer stub: registers App's imperative handle and records which
// session tab it belongs to plus every addAnnotation call.
const annotation = vi.hoisted(() => ({
  addAnnotation: vi.fn(),
  sessionTabId: null as string | null,
}));

vi.mock("./components/Chat/ChatInput", async () => {
  const React = await import("react");
  const MockChatInput = React.forwardRef(function MockChatInput(
    props: { sessionTabId?: string | null },
    ref: React.ForwardedRef<{ focus: () => void; addAnnotation: (a: unknown) => void }>,
  ) {
    React.useImperativeHandle(ref, () => ({
      focus: () => {},
      addAnnotation: (a) => annotation.addAnnotation(props.sessionTabId, a),
    }));
    annotation.sessionTabId = props.sessionTabId ?? null;
    return null;
  });
  return { default: MockChatInput };
});

vi.mock("./components/common/CommandPalette", () => ({ default: () => null }));
vi.mock("./components/Chat/ChatPanel", () => ({ default: () => null }));
vi.mock("./components/Preview/PreviewHost", () => ({ default: () => null }));
vi.mock("./components/Browser/BrowserPanel", () => ({ BrowserPanel: () => null }));
vi.mock("./components/Chat/AgentPreview", () => ({ default: () => null }));
vi.mock("./components/Agents/AgentsPanel", () => ({ default: () => null }));
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
import { BROWSER_ANNOTATION_EVENT } from "./components/Browser/annotation";

function dispatchAnnotation(detail: { stateKey: string; text: string; image?: Blob | null }) {
  act(() => {
    window.dispatchEvent(new CustomEvent(BROWSER_ANNOTATION_EVENT, { detail: { image: null, ...detail } }));
  });
}

beforeEach(() => {
  window.localStorage.clear();
  annotation.addAnnotation.mockReset();
  annotation.sessionTabId = null;
  appApi.listProjects.mockReset().mockResolvedValue([{ path: "/proj", name: "proj" }]);
  appApi.getCurrentProject.mockReset().mockResolvedValue({ project: { path: "/proj", name: "proj" } });
  appApi.listProjectSessions.mockReset().mockResolvedValue([]);
  appApi.listGroups.mockReset().mockResolvedValue([]);
  appApi.getSpending.mockReset().mockResolvedValue({ spending_usd: 0 });
});

async function renderWithChat() {
  render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  );
  fireEvent.click(await screen.findByRole("button", { name: /new chat session/i }));
  const tabId = await waitFor(() => {
    expect(annotation.sessionTabId).toBeTruthy();
    return annotation.sessionTabId as string;
  });
  return tabId;
}

describe("App browser annotation routing", () => {
  it("routes a side-pane annotation to the session named in its state key", async () => {
    const tabId = await renderWithChat();

    dispatchAnnotation({ stateKey: `side:chat:${tabId}`, text: "side note" });

    expect(annotation.addAnnotation).toHaveBeenCalledWith(tabId, expect.objectContaining({ text: "side note" }));
  });

  it("routes a browser-tab annotation to the project's active chat", async () => {
    const tabId = await renderWithChat();

    // Unknown browser tab id → no project owner, so the resolver falls back to
    // the active chat session.
    dispatchAnnotation({ stateKey: "tab:not-open", text: "canvas note" });

    expect(annotation.addAnnotation).toHaveBeenCalledWith(tabId, expect.objectContaining({ text: "canvas note" }));
  });

  it("ignores an annotation when no composer owns it", async () => {
    await renderWithChat();

    dispatchAnnotation({ stateKey: "side:chat:some-closed-session", text: "orphan" });

    expect(annotation.addAnnotation).not.toHaveBeenCalled();
  });

  it("reports no acceptance to the sender when no composer owns the annotation", async () => {
    await renderWithChat();
    const detail = { stateKey: "side:chat:some-closed-session", text: "orphan", image: null, accepted: false };

    act(() => {
      window.dispatchEvent(new CustomEvent(BROWSER_ANNOTATION_EVENT, { detail }));
    });

    expect(detail.accepted).toBe(false);
  });
});
