import { describe, expect, it, vi, afterEach, beforeEach } from "vitest";
import { fireEvent, render, cleanup, screen, waitFor } from "@testing-library/react";
import CoworkSidebar from "./CoworkSidebar";
import { ChatProvider } from "../../stores/chatStore";
import { api } from "../../api/client";
import type { SpeechSummaryConfig } from "../../api/client";

// The speech-summary row is a GLOBAL persisted config (the speech-summary
// block), not a per-session TUIStatus field, so unlike the rows above it has no
// tuiStatus fallback chain: the block is read once and the toggle writes it
// back through a single-key patch so the chosen model survives.
//
// This is NOT the compaction gate. `compact.enabled` decides whether a turn
// compacts itself; `speech_summary_enabled` decides whether text is shortened
// before TTS reads it. The row is about the latter.
vi.mock("../../stores/projectStore", () => ({
  findProjectPathForTab: () => undefined,
  // resolveSessionHost (CoworkSidebar.tsx:161) calls this directly — it is NOT
  // mocked, so it is the REAL implementation running against the stub state.
  findTabForSession: () => undefined,
  useProjectState: () => ({
    activeTabId: "session-1",
    state: { activeProject: null },
    dispatch: vi.fn(),
  }),
}));

// Force the resolved host for the active session. `null` = a local session.
const mockState = vi.hoisted(() => ({ host: null as string | null }));
vi.mock("../../hooks/useSessionHost", () => ({
  resolveSessionHost: () => mockState.host ?? undefined,
  useSessionHost: () => mockState.host ?? undefined,
}));

const SPEECH_ON: SpeechSummaryConfig = {
  enabled: true,
  model: "anthropic/claude-haiku-4-5",
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
    getDiscoveryConfig: vi.fn(() => Promise.resolve(null)),
    setDiscoveryConfig: vi.fn(() => Promise.resolve(null)),
    getSessionStatus: vi.fn(() => Promise.resolve({})),
    getSpeechSummaryConfig: vi.fn(() => Promise.resolve({ ...SPEECH_ON })),
    setSpeechSummaryConfig: vi.fn((patch: Partial<SpeechSummaryConfig>) =>
      Promise.resolve({ ...SPEECH_ON, ...patch })),
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
  mockState.host = null;
  vi.mocked(api.getSpeechSummaryConfig).mockImplementation(async () => ({ ...SPEECH_ON }));
  // Echo the server's MERGE: the endpoint merges onto what is on disk, so a
  // patch must not read as a whole-block replace.
  vi.mocked(api.setSpeechSummaryConfig).mockImplementation(
    async (patch: Partial<SpeechSummaryConfig>) => ({ ...SPEECH_ON, ...patch }),
  );
});

function renderSidebar(props: Partial<React.ComponentProps<typeof CoworkSidebar>> = {}) {
  return render(
    <ChatProvider>
      <CoworkSidebar isOpen onClose={() => {}} activeAgent="build" {...props} />
    </ChatProvider>,
  );
}

// The ●on/○off pill of the Speech summary row. Several rows render the same
// "○off" text, so every badge assertion must be scoped to this button.
function summaryPill(): HTMLElement {
  return screen.getByTitle("Pick the model that summarises text before it is spoken");
}

describe("CoworkSidebar summary model row", () => {
  it("renders the stored summary model and the speech on/off gate", async () => {
    renderSidebar();

    const toggle = await screen.findByLabelText("Speech summary enabled");
    expect((toggle as HTMLInputElement).checked).toBe(true);
    expect(summaryPill().textContent).toContain("●on");
    expect(screen.getByText("anthropic/claude-haiku-4-5")).toBeTruthy();
  });

  it("explains the auto fallback when no summary model is configured", async () => {
    vi.mocked(api.getSpeechSummaryConfig).mockImplementation(async () => ({ ...SPEECH_ON, model: "" }));
    renderSidebar();

    await screen.findByLabelText("Speech summary enabled");
    expect(screen.getByText("(auto: small model, then main)")).toBeTruthy();
  });

  it("opens the model picker for the speechsummary purpose", async () => {
    // A distinct purpose from "summary", which CompactForm still owns for
    // COMPACTION. Sharing one would make the dialog write the wrong config.
    const onModelClick = vi.fn();
    renderSidebar({ onModelClick });

    const button = await screen.findByTitle("Pick the model that summarises text before it is spoken");
    fireEvent.click(button);

    expect(onModelClick).toHaveBeenCalledExactlyOnceWith("speechsummary");
  });

  it("sends ONLY the gate, so a picked model is never cleared by the toggle", async () => {
    renderSidebar();
    const toggle = await screen.findByLabelText("Speech summary enabled");
    fireEvent.click(toggle);

    // A single-key body. The model is NOT echoed back: HandleSetSpeechSummary
    // Config merges onto the block it reads fresh from disk, so a client-held
    // snapshot could not clobber a concurrent writer.
    await waitFor(() =>
      expect(api.setSpeechSummaryConfig).toHaveBeenCalledExactlyOnceWith({ enabled: false }, undefined),
    );
    const sent = vi.mocked(api.setSpeechSummaryConfig).mock.calls[0][0];
    expect(Object.keys(sent)).toEqual(["enabled"]);
    // …and the client must not have fetched the block just to build that body.
    expect(vi.mocked(api.getSpeechSummaryConfig).mock.calls.length).toBe(1); // mount only
  });

  it("keeps the checkbox checked while the write is in flight, then follows the server", async () => {
    let release!: (cfg: SpeechSummaryConfig) => void;
    vi.mocked(api.setSpeechSummaryConfig).mockImplementation(
      () => new Promise<SpeechSummaryConfig>((resolve) => { release = resolve; }),
    );
    renderSidebar();
    const toggle = await screen.findByLabelText("Speech summary enabled");
    fireEvent.click(toggle);

    // Optimistic: the pill flips immediately so the click feels answered.
    await waitFor(() => expect(summaryPill().textContent).toContain("○off"));
    expect((toggle as HTMLInputElement).checked).toBe(false);

    // A server that comes back with the OLD value wins — never trust the guess.
    release({ ...SPEECH_ON, enabled: true });
    await waitFor(() => expect((toggle as HTMLInputElement).checked).toBe(true));
    // The merged block is also where the row picks up another writer's model.
    expect(summaryPill().textContent).toContain("anthropic/claude-haiku-4-5");
  });

  it("leaves the gate untouched when the write fails", async () => {
    vi.mocked(api.setSpeechSummaryConfig).mockRejectedValue(new Error("500 boom"));
    renderSidebar();
    const toggle = await screen.findByLabelText("Speech summary enabled");
    fireEvent.click(toggle);

    await waitFor(() => expect(api.setSpeechSummaryConfig).toHaveBeenCalledTimes(1));
    // Roll back to the value in effect, and report through the app-wide toast.
    await waitFor(() => expect((toggle as HTMLInputElement).checked).toBe(true));
  });

  it("reads the compact block from the LOCAL server for a local session", async () => {
    renderSidebar();
    await screen.findByLabelText("Speech summary enabled");
    expect(api.getSpeechSummaryConfig).toHaveBeenCalledExactlyOnceWith(undefined);
  });
});

describe("CoworkSidebar summary row host threading", () => {
  beforeEach(() => {
    mockState.host = "devbox";
  });

  it("reads and writes the compact block on the session's host", async () => {
    // A remote project's compaction settings live on the host that runs the
    // session. Reading/writing the local block would show and save the wrong
    // summary model — the same bug class as the small/explorer/context rows.
    renderSidebar();
    await screen.findByLabelText("Speech summary enabled");
    expect(api.getSpeechSummaryConfig).toHaveBeenCalledExactlyOnceWith("devbox");

    fireEvent.click(screen.getByLabelText("Speech summary enabled"));
    await waitFor(() =>
      expect(api.setSpeechSummaryConfig).toHaveBeenCalledExactlyOnceWith({ enabled: false }, "devbox"),
    );
  });
});
