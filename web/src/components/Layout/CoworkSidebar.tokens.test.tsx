import { describe, expect, it, vi, afterEach } from "vitest";
import { useEffect } from "react";
import { render, cleanup, screen } from "@testing-library/react";
import CoworkSidebar from "./CoworkSidebar";
import { ChatProvider, useChatDispatch } from "../../stores/chatStore";
import type { TUIStatus } from "../../api/types";

vi.mock("../../stores/projectStore", () => ({
  findProjectPathForTab: () => undefined,
  useProjectState: () => ({
    activeTabId: "session-1",
    state: { activeProject: null },
    dispatch: vi.fn(),
  }),
}));

vi.mock("../../api/client", () => ({
  api: {
    listAgents: vi.fn(() => Promise.resolve([])),
    getConfigModel: vi.fn(() => Promise.resolve({ model: "test-model" })),
    getThinkingBudget: vi.fn(() => Promise.resolve({ budget: 0 })),
    getPermissionModel: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getYolo: vi.fn(() => Promise.resolve({ yolo: false })),
    getRecapConfig: vi.fn(() => Promise.resolve({})),
    getAdvisor: vi.fn(() => Promise.resolve({ model: "" })),
    getAdvisorEnabled: vi.fn(() => Promise.resolve({ enabled: false })),
    getSmallModelWithEnabled: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getExplorerModel: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getContextModel: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getAutoContinue: vi.fn(() => Promise.resolve({ enabled: false, model: "" })),
    setAutoContinue: vi.fn(() => Promise.resolve({ enabled: false, model: "" })),
    getDiscoveryConfig: vi.fn(() => Promise.resolve(null)),
    setDiscoveryConfig: vi.fn(() => Promise.resolve(null)),
    getSessionStatus: vi.fn(() => Promise.resolve({})),
  },
  apiPath: (p: string) => p,
  remoteApiBase: (host?: string) => (host ? `/api/remote/${encodeURIComponent(host)}` : ""),
  authHeaders: () => ({}),
  reportAuthFailure: vi.fn(),
}));

const fetchMock = vi.fn(() =>
  Promise.resolve({ json: () => Promise.resolve({}) } as Response),
);
vi.stubGlobal("fetch", fetchMock);

afterEach(() => {
  cleanup();
  window.localStorage.clear();
});

// Seeds the active tab's per-session status snapshot, the way the server's
// fetch/SSE path would (SET_TUI_STATUS), so the sidebar renders real values.
function SeedStatus({ status }: { status: TUIStatus }) {
  const dispatch = useChatDispatch();
  useEffect(() => {
    dispatch({ type: "SET_TUI_STATUS", sessionId: "session-1", status });
  }, [dispatch, status]);
  return null;
}

function renderSidebarWithStatus(status: TUIStatus) {
  return render(
    <ChatProvider>
      <SeedStatus status={status} />
      <CoworkSidebar isOpen onClose={() => {}} activeAgent="build" />
    </ChatProvider>,
  );
}

describe("CoworkSidebar per-session token breakdown", () => {
  it("shows In/Cached/Output/Total tokens from the snapshot, even with no context reading", async () => {
    const { container } = renderSidebarWithStatus({
      input_tokens: 1000,
      cached_tokens: 300,
      output_tokens: 200,
      total_tokens: 1500,
      // No provider context reading: the token breakdown must still render
      // (it is independent of the context gauge).
      context_current_tokens: 0,
      context_max_tokens: 0,
    });
    const aside = container.querySelector("aside")!;
    expect(aside).not.toBeNull();
    expect(await screen.findByText("Input")).toBeTruthy();
    expect(screen.getByText("Cached")).toBeTruthy();
    expect(screen.getByText("Output")).toBeTruthy();
    expect(screen.getByText("Total")).toBeTruthy();
    expect(screen.getByText("1.0k")).toBeTruthy(); // input 1000
    expect(screen.getByText("300")).toBeTruthy(); // cached
    expect(screen.getByText("200")).toBeTruthy(); // output
    expect(screen.getByText("1.5k")).toBeTruthy(); // billed total 1500
  });

  it("shows no breakdown when the snapshot has no token counts", async () => {
    renderSidebarWithStatus({
      context_current_tokens: 12000,
      context_max_tokens: 200000,
    });
    // The context gauge is present...
    expect(await screen.findByText("Used")).toBeTruthy();
    // ...but the token rows are not fabricated from zero values.
    expect(screen.queryByText("Cached")).toBeNull();
    expect(screen.queryByText("Output")).toBeNull();
  });
});
