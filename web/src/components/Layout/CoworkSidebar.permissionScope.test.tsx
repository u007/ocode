import { describe, expect, it, vi, afterEach, beforeEach } from "vitest";
import { fireEvent, render, cleanup, screen, waitFor } from "@testing-library/react";
import CoworkSidebar from "./CoworkSidebar";
import { ChatProvider } from "../../stores/chatStore";
import { api } from "../../api/client";

// The permission pill must scope its write to the ACTIVE session id. The
// reported bug: toggling yolo in one chat changed every other chat/project,
// because the sidebar PUT a session-less process-global mode.
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
    getSessionStatus: vi.fn(() => Promise.resolve({ auto_continue_enabled: true })),
    setPermissionMode: vi.fn(() => Promise.resolve({ mode: "yolo", session_id: "session-1" })),
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

describe("CoworkSidebar permission-mode scoping", () => {
  it("cycles the mode scoped to the active session id", async () => {
    renderSidebar();
    // The pill renders the current mode uppercased; default is normal.
    const pill = await screen.findByRole("button", { name: /^normal/i });
    fireEvent.click(pill);
    await waitFor(() =>
      expect(api.setPermissionMode).toHaveBeenCalledWith("yolo", "session-1", undefined),
    );
  });

  it("never issues a session-less permission-mode write", async () => {
    renderSidebar();
    const pill = await screen.findByRole("button", { name: /^normal/i });
    fireEvent.click(pill);
    await waitFor(() => expect(api.setPermissionMode).toHaveBeenCalled());
    for (const call of vi.mocked(api.setPermissionMode).mock.calls) {
      expect(call[1]).toBe("session-1");
    }
  });
});
