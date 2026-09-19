import { describe, expect, it, vi, afterEach, beforeEach } from "vitest";
import { fireEvent, render, cleanup, screen, waitFor } from "@testing-library/react";
import CoworkSidebar from "./CoworkSidebar";
import { ChatProvider } from "../../stores/chatStore";
import { api } from "../../api/client";
import { getActionError, resetActionErrors } from "../../lib/actionErrors";

// The advisor toggle must feel instant and must target the ACTIVE chat's
// server. Two reported symptoms:
//   1. "its clunky" — the checkbox did not move until the PUT + status refetch
//      round trip finished, so a click looked ignored.
//   2. "i thought wasnt working" — the PUT carried no host, so a remote
//      project's session hit the LOCAL server (which cannot resolve that
//      session and 404s), leaving the toggle silently inert.
vi.mock("../../stores/projectStore", () => ({
  findProjectPathForTab: () => undefined,
  useProjectState: () => ({
    activeTabId: "session-1",
    state: { activeProject: null },
    dispatch: vi.fn(),
  }),
}));

// Force a remote host so the sidebar's session-scoped calls must thread it.
vi.mock("../../hooks/useSessionHost", () => ({
  resolveSessionHost: () => "devbox",
  useSessionHost: () => "devbox",
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
    // The server still reports the OLD value while the PUT is in flight.
    getSessionStatus: vi.fn(() => Promise.resolve({ advisor_enabled: false })),
  },
  apiPath: (p: string) => p,
  remoteApiBase: (host?: string) => (host ? `/api/remote/${encodeURIComponent(host)}` : ""),
  authHeaders: () => ({}),
}));

const fetchMock = vi.fn(() => Promise.resolve({ json: () => Promise.resolve({}) } as Response));
vi.stubGlobal("fetch", fetchMock);

afterEach(() => {
  cleanup();
  resetActionErrors();
});
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.setAdvisorEnabled).mockImplementation(() => Promise.resolve({ enabled: true }));
  vi.mocked(api.getSessionStatus).mockImplementation(() => Promise.resolve({ advisor_enabled: false }));
});

function renderSidebar() {
  return render(
    <ChatProvider>
      <CoworkSidebar isOpen onClose={() => {}} activeAgent="build" />
    </ChatProvider>,
  );
}

describe("CoworkSidebar advisor toggle — optimistic + host scoping", () => {
  it("flips the checkbox immediately, before the PUT resolves", async () => {
    // A pending PUT never resolves, so the only way the checkbox can be checked
    // is the optimistic per-session write.
    let resolvePut: (v: { enabled: boolean }) => void = () => {};
    vi.mocked(api.setAdvisorEnabled).mockImplementation(
      () => new Promise((resolve) => (resolvePut = resolve)),
    );
    renderSidebar();
    const toggle = (await screen.findByLabelText("Advisor enabled")) as HTMLInputElement;
    expect(toggle.checked).toBe(false);

    fireEvent.click(toggle);
    await waitFor(() => expect(toggle.checked).toBe(true));

    // Let the PUT settle so the test does not leak a pending promise.
    resolvePut({ enabled: true });
  });

  it("routes the write and the reconciliation to the session's remote host", async () => {
    renderSidebar();
    const toggle = await screen.findByLabelText("Advisor enabled");
    fireEvent.click(toggle);
    await waitFor(() => expect(api.setAdvisorEnabled).toHaveBeenCalled());
    expect(api.setAdvisorEnabled).toHaveBeenCalledWith(true, "session-1", "devbox");
    // The status refetch must also go to that host, or it reads the local
    // server's unrelated state.
    await waitFor(() => expect(api.getSessionStatus).toHaveBeenCalledWith("session-1", "devbox"));
  });

  it("reverts the optimistic flip and refetches when the write fails", async () => {
    vi.mocked(api.setAdvisorEnabled).mockImplementation(() =>
      Promise.reject(new Error("404 session not found")),
    );
    vi.mocked(api.getSessionStatus).mockImplementation(() =>
      Promise.resolve({ advisor_enabled: false }),
    );
    renderSidebar();
    const toggle = (await screen.findByLabelText("Advisor enabled")) as HTMLInputElement;
    fireEvent.click(toggle);

    // After the failure path settles, the checkbox reflects the server truth.
    await waitFor(() => expect(toggle.checked).toBe(false));
    expect(api.getSessionStatus).toHaveBeenCalledWith("session-1", "devbox");
  });

  it("surfaces a failed toggle as a visible action error, not just a console log", async () => {
    // Structurally an ApiError (message + numeric status); this suite mocks the
    // client module, so use a plain object rather than importing the class.
    const notFound = Object.assign(new Error("session not found"), { status: 404 });
    vi.mocked(api.setAdvisorEnabled).mockImplementation(() => Promise.reject(notFound));
    renderSidebar();
    const toggle = await screen.findByLabelText("Advisor enabled");
    fireEvent.click(toggle);

    // The store is the single source the root-mounted ActionErrorToast renders,
    // so asserting it proves the failure is now reachable in the UI.
    await waitFor(() => {
      expect(getActionError()?.message).toBe(
        "Toggling the advisor failed: session not found (HTTP 404)",
      );
    });
  });
});
