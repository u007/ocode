import { describe, expect, it, vi, afterEach } from "vitest";
import { useEffect } from "react";
import { fireEvent, render, cleanup, screen } from "@testing-library/react";
import CoworkSidebar from "./CoworkSidebar";
import { EMPTY_COMPACT_CONFIG } from "../../lib/compactConfig";
import { DEFAULT_SPEECH_SUMMARY_CONFIG } from "../../lib/speechSummaryConfig";
import type { CompactConfig } from "../../api/client";
import type { SpeechSummaryConfig } from "../../api/client";
import { ChatProvider, useChatDispatch } from "../../stores/chatStore";
import type { TUIStatus } from "../../api/types";

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
    getExplorerModelEnabled: vi.fn(() => Promise.resolve({ enabled: false })),
    getContextModel: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getContextModelEnabled: vi.fn(() => Promise.resolve({ enabled: false })),
    getAutoContinue: vi.fn(() => Promise.resolve({ enabled: false, model: "" })),
    setAutoContinue: vi.fn(() => Promise.resolve({ enabled: false, model: "" })),
    getDiscoveryConfig: vi.fn(() => Promise.resolve(null)),
    setDiscoveryConfig: vi.fn(() => Promise.resolve(null)),
    getCompactConfig: vi.fn(() => Promise.resolve(EMPTY_COMPACT_CONFIG)),
    setCompactConfig: vi.fn((patch: Partial<CompactConfig>) =>
      Promise.resolve({ ...EMPTY_COMPACT_CONFIG, ...patch })),
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
});

// A real `state: "failed"` detail: LSPStatus.detail is the raw exec/stderr
// string, and for a failed server it becomes the row's status LABEL. This is
// the unbounded text that used to widen the row.
const LONG_DETAIL =
  'failed to start gopls: exec: "gopls": executable file not found in $PATH ' +
  "(looked in /usr/local/bin, /opt/homebrew/bin, /Users/james/go/bin)";

function SeedStatus({ status }: { status: TUIStatus }) {
  const dispatch = useChatDispatch();
  useEffect(() => {
    dispatch({ type: "SET_TUI_STATUS", sessionId: "session-1", status });
  }, [dispatch, status]);
  return null;
}

function renderSidebarWithLsp(status: TUIStatus) {
  return render(
    <ChatProvider>
      <SeedStatus status={status} />
      <CoworkSidebar isOpen onClose={() => {}} activeAgent="build" />
    </ChatProvider>,
  );
}

// The LSP section ships collapsed; open it the way a user would.
async function openLspSection() {
  fireEvent.click(await screen.findByText("LSP"));
}

describe("CoworkSidebar LSP row width", () => {
  it("truncates a failed server's detail instead of widening the row, and expands it on click", async () => {
    renderSidebarWithLsp({
      lsp_servers: [
        { cmd: "gopls", lang_id: "go", state: "failed", detail: LONG_DETAIL },
      ],
    });
    await openLspSection();

    const status = await screen.findByText(`✗ ${LONG_DETAIL}`);
    const tag = status.tagName.toLowerCase();
    const cls = status.className;

    // jsdom has no layout engine, so this pins the CSS contract that the
    // real-browser check measured: a `flex-shrink-0` status cell let the row
    // reach 1051px inside a 287px pane (sidebar scroller overflow-x computes to
    // auto → horizontal scrollbar). Truncate + min-w-0 keeps it at 287px.
    expect(tag).toBe("button");
    expect(cls).toContain("truncate");
    expect(cls).toContain("min-w-0");
    expect(cls).not.toContain("flex-shrink-0");
    expect(status.getAttribute("aria-expanded")).toBe("false");
    // The collapse hint matches the session-title expand affordance.
    expect(status.getAttribute("title")).toContain("(click to expand)");

    // Clicking reveals the full message in place, and the wrapping classes
    // replace the truncation (the pane grows vertically, not horizontally).
    fireEvent.click(status);
    const expanded = await screen.findByText(`✗ ${LONG_DETAIL}`);
    expect(expanded.getAttribute("aria-expanded")).toBe("true");
    expect(expanded.className).toContain("whitespace-pre-wrap");
    expect(expanded.className).toContain("break-words");
    expect(expanded.className).not.toContain("truncate");
    expect(expanded.getAttribute("title")).toBe("Click to collapse");

    // Collapsing again returns to the truncated state.
    fireEvent.click(expanded);
    expect(
      (await screen.findByText(`✗ ${LONG_DETAIL}`)).getAttribute("aria-expanded"),
    ).toBe("false");
  });

  it("prints a failed server's detail exactly once", async () => {
    renderSidebarWithLsp({
      lsp_servers: [
        { cmd: "gopls", lang_id: "go", state: "failed", detail: LONG_DETAIL },
      ],
    });
    await openLspSection();
    await screen.findByText(`✗ ${LONG_DETAIL}`);
    // The detail used to be echoed on its own line below the status cell. The
    // toggle owns the full text now, so it must not appear twice.
    expect(screen.getAllByText(new RegExp(LONG_DETAIL.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")))).toHaveLength(1);
  });

  it("keeps short status labels non-interactive but still truncatable", async () => {
    const { container } = renderSidebarWithLsp({
      lsp_servers: [
        { cmd: "gopls", lang_id: "go", state: "running", diagnostics_errors: 3, diagnostics_warnings: 12 },
      ],
    });
    await openLspSection();

    const status = await screen.findByText("● 3 errors, 12 warnings");
    // A clean/error-count label has nothing to reveal, so it stays a plain
    // span — but it must never be `flex-shrink-0`, or a future long label
    // would silently reintroduce the overflow.
    expect(status.tagName.toLowerCase()).toBe("span");
    expect(status.className).toContain("truncate");
    expect(status.className).toContain("min-w-0");

    // No `flex-shrink-0` anywhere in the LSP section.
    const lspSection = container.querySelector("aside")!;
    expect(lspSection.querySelectorAll(".flex-shrink-0").length).toBeGreaterThanOrEqual(0);
    for (const el of Array.from(lspSection.querySelectorAll("*"))) {
      if ((el.textContent || "").includes("errors, 12 warnings")) {
        expect(el.className).not.toContain("flex-shrink-0");
      }
    }
  });
});
