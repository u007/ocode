import { describe, expect, it, vi, afterEach } from "vitest";
import { useEffect } from "react";
import { render, cleanup, screen, fireEvent, waitFor, within, act } from "@testing-library/react";
import CoworkSidebar from "./CoworkSidebar";
import { EMPTY_COMPACT_CONFIG } from "../../lib/compactConfig";
import { DEFAULT_SPEECH_SUMMARY_CONFIG } from "../../lib/speechSummaryConfig";
import type { CompactConfig } from "../../api/client";
import type { SpeechSummaryConfig } from "../../api/client";
import { api } from "../../api/client";
import { ChatProvider, useChatDispatch } from "../../stores/chatStore";
import type { TUIStatus } from "../../api/types";

/**
 * The "Extra Dirs" section (removed in cbe28b6c, restored here) lists the
 * session's pre-authorized `extra_allowed_paths`. These tests pin the restored
 * contract: collapsed by default, count badge, expand-to-list, empty state, the
 * persisted-config fallback for a snapshot-less tab, and host threading.
 */
const mockState = vi.hoisted(() => ({ host: null as string | null }));

vi.mock("../../stores/projectStore", () => ({
  findProjectPathForTab: () => undefined,
  // resolveSessionHost (CoworkSidebar.tsx) calls this directly — it is NOT
  // mocked, so it is the REAL implementation running against the stub state.
  findTabForSession: () => undefined,
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
    getSmallModelWithEnabled: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getExplorerModel: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getContextModel: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getAutoContinue: vi.fn(() => Promise.resolve({ enabled: false, model: "" })),
    setAutoContinue: vi.fn(() => Promise.resolve({ enabled: false, model: "" })),
    getDiscoveryConfig: vi.fn(() => Promise.resolve(null)),
    setDiscoveryConfig: vi.fn(() => Promise.resolve(null)),
    getPathsConfig: vi.fn(() => Promise.resolve({ extra_allowed_paths: [], upload_dir: "" })),
    // The compaction summary row (CoworkSidebar's "Summary" pair) reads/writes
    // this block; without it the mount Promise.all throws on the missing method.
    getCompactConfig: vi.fn(() => Promise.resolve(EMPTY_COMPACT_CONFIG)),
    setCompactConfig: vi.fn((patch: Partial<CompactConfig>) =>
      Promise.resolve({ ...EMPTY_COMPACT_CONFIG, ...patch })),
    // The Summary row reads/writes the speech-summary block; without it the
    // mount Promise.all throws on the missing method and the effect dies.
    getSpeechSummaryConfig: vi.fn(() =>
      Promise.resolve(DEFAULT_SPEECH_SUMMARY_CONFIG)),
    setSpeechSummaryConfig: vi.fn((patch: Partial<SpeechSummaryConfig>) =>
      Promise.resolve({ ...DEFAULT_SPEECH_SUMMARY_CONFIG, ...patch })),
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
  vi.clearAllMocks();
  mockState.host = null;
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

function renderSidebar(status?: TUIStatus) {
  return render(
    <ChatProvider>
      {status !== undefined && <SeedStatus status={status} />}
      <CoworkSidebar isOpen onClose={() => {}} activeAgent="build" />
    </ChatProvider>,
  );
}

describe("CoworkSidebar Extra Dirs section", () => {
  it("renders collapsed by default with a count, then lists the paths when expanded", async () => {
    renderSidebar({ extra_allowed_paths: ["/srv/data", "/opt/shared"] });
    const header = await screen.findByText("Extra Dirs");
    // Collapsed: the paths are not in the DOM yet, but the count badge is.
    expect(screen.queryByText("/srv/data")).toBeNull();
    expect(within(header).getByText("2")).toBeTruthy();
    fireEvent.click(header);
    expect(await screen.findByText("/srv/data")).toBeTruthy();
    expect(screen.getByText("/opt/shared")).toBeTruthy();
  });

  it("shows an empty state when there are no extra dirs", async () => {
    renderSidebar({ extra_allowed_paths: [] });
    fireEvent.click(await screen.findByText("Extra Dirs"));
    expect(await screen.findByText("No extra dirs")).toBeTruthy();
  });

  it("seeds the list from the persisted config when the session has no snapshot", async () => {
    vi.mocked(api.getPathsConfig).mockResolvedValueOnce({
      extra_allowed_paths: ["/config/extra"],
      upload_dir: "",
    });
    // No snapshot at all → fall back to the config read.
    renderSidebar();
    fireEvent.click(await screen.findByText("Extra Dirs"));
    expect(await screen.findByText("/config/extra")).toBeTruthy();
  });

  it("does not resurrect a removed dir from stale config when the snapshot list is empty", async () => {
    vi.mocked(api.getPathsConfig).mockResolvedValueOnce({
      extra_allowed_paths: ["/removed"],
      upload_dir: "",
    });
    // A snapshot exists but carries no extra_allowed_paths (the server omits an
    // empty list via json `omitempty`). That must render the empty state — not
    // fall through to the stale mount-time config value.
    renderSidebar({});
    // Let the mount config fetch resolve/apply first, so a fallback to the stale
    // value would be visible and this test would catch it.
    await act(async () => {});
    fireEvent.click(screen.getByText("Extra Dirs"));
    expect(screen.getByText("No extra dirs")).toBeTruthy();
    expect(screen.queryByText("/removed")).toBeNull();
  });

  it("reads the paths config for the active session's host", async () => {
    mockState.host = "devbox";
    renderSidebar({});
    await waitFor(() => expect(api.getPathsConfig).toHaveBeenCalledWith("devbox"));
  });
});
