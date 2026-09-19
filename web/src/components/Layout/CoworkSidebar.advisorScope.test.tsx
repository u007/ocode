import { describe, expect, it, vi, afterEach, beforeEach } from "vitest";
import { fireEvent, render, cleanup, screen, waitFor } from "@testing-library/react";
import CoworkSidebar from "./CoworkSidebar";
import { ChatProvider } from "../../stores/chatStore";
import { api } from "../../api/client";

// The advisor toggle must be scoped to the ACTIVE chat session. The reported
// bug: toggling the advisor in one chat flipped it for every other chat,
// because the sidebar PUT a session-less process-global value.
const mockState = vi.hoisted(() => ({ activeTabId: "session-1" }));

vi.mock("../../stores/projectStore", () => ({
  findProjectPathForTab: () => undefined,
  useProjectState: () => ({
    activeTabId: mockState.activeTabId,
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
    setAdvisorEnabled: vi.fn(() => Promise.resolve({ enabled: true })),
    getSmallModelWithEnabled: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getExplorerModel: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getContextModel: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getAutoContinue: vi.fn(() => Promise.resolve({ enabled: false, model: "" })),
    setAutoContinue: vi.fn(() => Promise.resolve({ enabled: false, model: "" })),
    getDiscoveryConfig: vi.fn(() => Promise.resolve(null)),
    setDiscoveryConfig: vi.fn(() => Promise.resolve(null)),
    getSessionStatus: vi.fn(() => Promise.resolve({ advisor_enabled: true })),
  },
  apiPath: (p: string) => p,
  remoteApiBase: (host?: string) => (host ? `/api/remote/${encodeURIComponent(host)}` : ""),
  authHeaders: () => ({}),
}));

const fetchMock = vi.fn(() => Promise.resolve({ json: () => Promise.resolve({}) } as Response));
vi.stubGlobal("fetch", fetchMock);

afterEach(() => {
  cleanup();
});
beforeEach(() => {
  vi.clearAllMocks();
});

function renderSidebar() {
  return render(
    <ChatProvider>
      <CoworkSidebar isOpen onClose={() => {}} activeAgent="build" />
    </ChatProvider>,
  );
}

describe("CoworkSidebar advisor-toggle scoping", () => {
  it("scopes the advisor toggle to the active session id", async () => {
    mockState.activeTabId = "session-1";
    renderSidebar();
    const toggle = await screen.findByLabelText("Advisor enabled");
    fireEvent.click(toggle);
    await waitFor(() =>
      expect(api.setAdvisorEnabled).toHaveBeenCalledWith(true, "session-1"),
    );
  });

  it("never issues a session-less advisor write for a real session", async () => {
    mockState.activeTabId = "session-1";
    renderSidebar();
    const toggle = await screen.findByLabelText("Advisor enabled");
    fireEvent.click(toggle);
    await waitFor(() => expect(api.setAdvisorEnabled).toHaveBeenCalled());
    for (const call of vi.mocked(api.setAdvisorEnabled).mock.calls) {
      expect(call[1]).toBe("session-1");
    }
  });

  it("falls back to the process-global write only for a draft tab", async () => {
    mockState.activeTabId = "new-1";
    renderSidebar();
    const toggle = await screen.findByLabelText("Advisor enabled");
    fireEvent.click(toggle);
    await waitFor(() => expect(api.setAdvisorEnabled).toHaveBeenCalledWith(true, undefined));
  });
});
