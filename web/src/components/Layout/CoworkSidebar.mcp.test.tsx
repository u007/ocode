import { describe, expect, it, vi, afterEach, beforeEach } from "vitest";
import { fireEvent, render, cleanup, screen, waitFor } from "@testing-library/react";
import CoworkSidebar from "./CoworkSidebar";
import { ChatProvider } from "../../stores/chatStore";
import { api } from "../../api/client";
import type { MCPStatus } from "../../api/types";

// The MCP section mirrors the server's per-chat toggle: it seeds from
// GET /api/mcp (scoped to the active session so it reflects that chat's
// overrides) and flips a server with PUT /api/mcp/{name}/enable|disable
// carrying the session id, which the server uses to rebuild only this chat.
vi.mock("../../stores/projectStore", () => ({
  findProjectPathForTab: () => undefined,
  useProjectState: () => ({
    activeTabId: "session-1",
    state: { activeProject: null },
    dispatch: vi.fn(),
  }),
}));

const SERVERS: MCPStatus[] = [
  { name: "alpha", type: "local", enabled: true },
  { name: "beta", type: "remote", enabled: false },
];

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
    setDiscoveryConfig: vi.fn(),
    getSessionStatus: vi.fn(() => Promise.resolve({})),
    getMCP: vi.fn(() => Promise.resolve(SERVERS)),
    setMCPEnabled: vi.fn(() => Promise.resolve({ name: "beta", status: "enabled" })),
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
  // The sidebar persists its expanded-section layout; clear it so each test
  // starts from the documented default (MCP collapsed).
  window.localStorage.clear();
  vi.mocked(api.getMCP).mockResolvedValue(SERVERS);
  vi.mocked(api.setMCPEnabled).mockResolvedValue({ name: "beta", status: "enabled" });
});

function renderSidebar() {
  return render(
    <ChatProvider>
      <CoworkSidebar isOpen onClose={() => {}} activeAgent="build" />
    </ChatProvider>,
  );
}

// The section is collapsed by default, so open it before asserting rows. The
// expanded/collapsed choice persists to localStorage, so a later test may find
// it already open — only click the header when the rows are not yet there.
async function openMcpSection() {
  if (screen.queryByLabelText("MCP server alpha")) return;
  fireEvent.click(screen.getByText("MCP"));
  await waitFor(() => {
    expect(screen.getByLabelText("MCP server alpha")).toBeTruthy();
  });
}

describe("CoworkSidebar MCP toggle", () => {
  it("renders each MCP server with its enabled state", async () => {
    renderSidebar();
    await openMcpSection();

    const alpha = screen.getByLabelText("MCP server alpha") as HTMLInputElement;
    const beta = screen.getByLabelText("MCP server beta") as HTMLInputElement;
    expect(alpha.checked).toBe(true);
    expect(beta.checked).toBe(false);
  });

  it("queries the list scoped to the active session", async () => {
    renderSidebar();
    await openMcpSection();
    expect(api.getMCP).toHaveBeenCalledWith(undefined, "session-1");
  });

  it("toggles a server scoped to the active session and refetches", async () => {
    renderSidebar();
    await openMcpSection();

    fireEvent.click(screen.getByLabelText("MCP server beta"));

    await waitFor(() => {
      expect(api.setMCPEnabled).toHaveBeenCalledWith("beta", true, undefined, "session-1");
    });
    // The list is re-fetched with the same session scope so the switch reflects
    // the server's authoritative state.
    await waitFor(() => {
      expect(vi.mocked(api.getMCP).mock.calls.length).toBeGreaterThanOrEqual(2);
    });
  });

  it("rolls the switch back when the toggle request fails", async () => {
    vi.mocked(api.setMCPEnabled).mockRejectedValueOnce(new Error("nope"));
    renderSidebar();
    await openMcpSection();

    const beta = screen.getByLabelText("MCP server beta") as HTMLInputElement;
    fireEvent.click(beta);

    // Optimistic flip then rollback: beta ends up back at unchecked.
    await waitFor(() => {
      const after = screen.getByLabelText("MCP server beta") as HTMLInputElement;
      expect(after.checked).toBe(false);
    });
  });
});
