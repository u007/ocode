import { describe, expect, it, vi, afterEach, beforeEach } from "vitest";
import { fireEvent, render, cleanup, screen, waitFor } from "@testing-library/react";
import CoworkSidebar from "./CoworkSidebar";
import { ChatProvider } from "../../stores/chatStore";
import { api } from "../../api/client";

/**
 * Remote-SSH host threading for the sidebar model rows.
 *
 * Reported bug: "on remote ssh, i cant toggle the small model off, nothing
 * happen." The PUT reached the LOCAL server (no `host`), so it flipped the
 * local config; the sidebar then refetched the REMOTE session status, which
 * was unchanged — the toggle looked completely inert. The same omission
 * affected the explorer / context / permission / auto-continue gates and the
 * reasoning level, and the initial config read (so a remote project rendered
 * the local machine's models).
 *
 * These tests pin that every session-scoped config read/write carries the
 * active session's host.
 */
const mockState = vi.hoisted(() => ({ host: "devbox" as string | null }));

vi.mock("../../stores/projectStore", () => ({
  findProjectPathForTab: () => undefined,
  useProjectState: () => ({
    activeTabId: "session-1",
    state: { activeProject: null },
    dispatch: vi.fn(),
  }),
}));

// Force the resolved host for the active session. `null` = a local session.
vi.mock("../../hooks/useSessionHost", () => ({
  resolveSessionHost: () => mockState.host ?? undefined,
  useSessionHost: () => mockState.host ?? undefined,
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
    setSmallModelEnabled: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getExplorerModel: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    setExplorerModelEnabled: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getContextModel: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    setContextModelEnabled: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getAutoContinue: vi.fn(() => Promise.resolve({ enabled: false, model: "" })),
    setAutoContinue: vi.fn(() => Promise.resolve({ enabled: false, model: "" })),
    setPermissionModelEnabled: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getDiscoveryConfig: vi.fn(() => Promise.resolve(null)),
    setDiscoveryConfig: vi.fn(() => Promise.resolve(null)),
    getSessionStatus: vi.fn(() => Promise.resolve({})),
  },
  apiPath: (p: string) => p,
  remoteApiBase: (host?: string) => (host ? `/api/remote/${encodeURIComponent(host)}` : ""),
  authHeaders: () => ({}),
}));

vi.stubGlobal("fetch", vi.fn(() => Promise.resolve({ json: () => Promise.resolve({}) } as Response)));

afterEach(() => {
  cleanup();
});
beforeEach(() => {
  vi.clearAllMocks();
  mockState.host = "devbox";
});

function renderSidebar() {
  return render(
    <ChatProvider>
      <CoworkSidebar isOpen onClose={() => {}} activeAgent="build" />
    </ChatProvider>,
  );
}

describe("CoworkSidebar remote-host model threading", () => {
  it("seeds every model row from the session's host, not the local server", async () => {
    renderSidebar();
    await waitFor(() => expect(api.getSmallModelWithEnabled).toHaveBeenCalledWith("devbox"));
    expect(api.getConfigModel).toHaveBeenCalledWith("devbox");
    expect(api.getThinkingBudget).toHaveBeenCalledWith("devbox");
    expect(api.getExplorerModel).toHaveBeenCalledWith("devbox");
    expect(api.getContextModel).toHaveBeenCalledWith("devbox");
    expect(api.getPermissionModel).toHaveBeenCalledWith("devbox");
    expect(api.getAutoContinue).toHaveBeenCalledWith("devbox");
    expect(api.getRecapConfig).toHaveBeenCalledWith("devbox");
    expect(api.getAdvisor).toHaveBeenCalledWith("devbox");
  });

  it("routes the small-model toggle to the session's host", async () => {
    renderSidebar();
    const toggle = await screen.findByLabelText("Small model enabled");
    fireEvent.click(toggle);
    await waitFor(() =>
      expect(api.setSmallModelEnabled).toHaveBeenCalledWith(true, "devbox"),
    );
  });

  it("routes the explorer and context gates to the session's host", async () => {
    renderSidebar();
    fireEvent.click(await screen.findByLabelText("Explorer model enabled"));
    await waitFor(() =>
      expect(api.setExplorerModelEnabled).toHaveBeenCalledWith(true, "devbox"),
    );
    fireEvent.click(await screen.findByLabelText("Context model enabled"));
    await waitFor(() =>
      expect(api.setContextModelEnabled).toHaveBeenCalledWith(true, "devbox"),
    );
  });

  it("routes auto-continue and the permission gate to the session's host", async () => {
    renderSidebar();
    fireEvent.click(await screen.findByLabelText("Auto-continue enabled"));
    await waitFor(() =>
      expect(api.setAutoContinue).toHaveBeenCalledWith({ enabled: true }, "devbox"),
    );
    fireEvent.click(await screen.findByLabelText("Auto-permission"));
    await waitFor(() =>
      expect(api.setPermissionModelEnabled).toHaveBeenCalledWith(true, "devbox"),
    );
  });

  it("keeps local (no-host) calls byte-identical for a non-remote session", async () => {
    mockState.host = null;
    renderSidebar();
    await waitFor(() => expect(api.getSmallModelWithEnabled).toHaveBeenCalledWith(undefined));
    const toggle = await screen.findByLabelText("Small model enabled");
    fireEvent.click(toggle);
    await waitFor(() =>
      expect(api.setSmallModelEnabled).toHaveBeenCalledWith(true, undefined),
    );
  });
});
