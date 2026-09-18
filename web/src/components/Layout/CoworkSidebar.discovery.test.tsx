import { describe, expect, it, vi, afterEach, beforeEach } from "vitest";
import { fireEvent, render, cleanup, screen, waitFor, within } from "@testing-library/react";
import CoworkSidebar from "./CoworkSidebar";
import { ChatProvider } from "../../stores/chatStore";
import { api } from "../../api/client";
import type { DiscoveryConfig } from "../../api/client";

// The discovery row mirrors the TUI sidebar's `discover:` toggle: it seeds
// from GET /api/config/ocode/discovery and flips `enabled` with a full-config
// PUT (the endpoint replaces the whole block, so embedding model/backend,
// pinned skills and ignore paths must survive the toggle).
vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    activeTabId: "session-1",
    state: { activeProject: null },
    dispatch: vi.fn(),
  }),
}));

const DISCOVERY_ON: DiscoveryConfig = {
  enabled: true,
  embedding_model: "bge-m3",
  embedding_backend: "http",
  local_model_status: "none",
  local_server_url: "",
  pinned_skills: ["skill-a"],
  ignore_paths: ["vendor/"],
};

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
    getDiscoveryConfig: vi.fn(() => Promise.resolve(DISCOVERY_ON)),
    setDiscoveryConfig: vi.fn((cfg: DiscoveryConfig) => Promise.resolve(cfg)),
    getSessionStatus: vi.fn(() => Promise.resolve({})),
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
  vi.mocked(api.getDiscoveryConfig).mockResolvedValue(DISCOVERY_ON);
  vi.mocked(api.setDiscoveryConfig).mockImplementation((cfg: DiscoveryConfig) => Promise.resolve(cfg));
});

function renderSidebar() {
  return render(
    <ChatProvider>
      <CoworkSidebar isOpen onClose={() => {}} activeAgent="build" />
    </ChatProvider>,
  );
}

// The discovery row is the <label> wrapping the "Discovery" caption text.
function discoveryRow(): HTMLElement {
  return screen.getByText("Discovery").closest("label") as HTMLElement;
}

describe("CoworkSidebar discovery toggle", () => {
  it("renders the discovery row from the persisted config", async () => {
    renderSidebar();
    const toggle = await screen.findByLabelText("Discovery enabled");
    expect((toggle as HTMLInputElement).checked).toBe(true);
    // On + http backend shows the embedding model, mirroring the TUI row.
    expect(screen.getByText("●on bge-m3")).toBeTruthy();
  });

  it("flips discovery.enabled without clobbering the rest of the config", async () => {
    renderSidebar();
    const toggle = await screen.findByLabelText("Discovery enabled");
    fireEvent.click(toggle);

    await waitFor(() =>
      expect(api.setDiscoveryConfig).toHaveBeenCalledWith({ ...DISCOVERY_ON, enabled: false }),
    );
    // The other fields must be carried through untouched.
    const sent = vi.mocked(api.setDiscoveryConfig).mock.calls[0][0];
    expect(sent.embedding_model).toBe("bge-m3");
    expect(sent.embedding_backend).toBe("http");
    expect(sent.pinned_skills).toEqual(["skill-a"]);
    expect(sent.ignore_paths).toEqual(["vendor/"]);

    // Scope to the discovery row: every other toggle also renders "○off".
    await waitFor(() =>
      expect(within(discoveryRow()).getByText("○off")).toBeTruthy(),
    );
  });

  it("renders ○off when discovery is disabled", async () => {
    vi.mocked(api.getDiscoveryConfig).mockResolvedValue({ ...DISCOVERY_ON, enabled: false });
    renderSidebar();
    const toggle = await screen.findByLabelText("Discovery enabled");
    expect((toggle as HTMLInputElement).checked).toBe(false);
    expect(within(discoveryRow()).getByText("○off")).toBeTruthy();
  });
});
