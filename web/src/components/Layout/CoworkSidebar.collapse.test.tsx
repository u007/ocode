import { describe, expect, it, vi, afterEach } from "vitest";
import { fireEvent, render, cleanup, screen, waitFor } from "@testing-library/react";
import CoworkSidebar from "./CoworkSidebar";
import { ChatProvider } from "../../stores/chatStore";

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
  },
  apiPath: (p: string) => p,
  remoteApiBase: (host?: string) => (host ? `/api/remote/${encodeURIComponent(host)}` : ""),
  authHeaders: () => ({}),
}));

const fetchMock = vi.fn(() =>
  Promise.resolve({ json: () => Promise.resolve({}) } as Response),
);
vi.stubGlobal("fetch", fetchMock);

afterEach(() => {
  cleanup();
});

function renderSidebar(props: Partial<React.ComponentProps<typeof CoworkSidebar>> = {}) {
  return render(
    <ChatProvider>
      <CoworkSidebar
        isOpen
        onClose={() => {}}
        activeAgent="build"
        {...props}
      />
    </ChatProvider>,
  );
}

describe("CoworkSidebar collapse (React error #300 regression)", () => {
  it("renders open on desktop without throwing", () => {
    expect(() => renderSidebar({ isOpen: true })).not.toThrow();
  });

  it("transitions open → collapsed → open without a hooks mismatch", () => {
    const { rerender, container } = renderSidebar({ isOpen: true });
    // Desktop collapse unmounts to null — this exact transition used to throw
    // "Rendered fewer hooks than expected" (minified React error #300) because
    // a useState lived below the early return.
    expect(() => rerender(
      <ChatProvider>
        <CoworkSidebar isOpen={false} onClose={() => {}} activeAgent="build" />
      </ChatProvider>,
    )).not.toThrow();
    expect(container.textContent ?? "").toBeDefined();

    // And reopening must work too.
    expect(() => rerender(
      <ChatProvider>
        <CoworkSidebar isOpen onClose={() => {}} activeAgent="build" />
      </ChatProvider>,
    )).not.toThrow();
  });

  it("stays mounted on mobile when closed (slide-off layout)", () => {
    const { container } = renderSidebar({ isOpen: false, isMobile: true });
    // Mobile keeps the surface mounted off-screen instead of returning null.
    expect(container.firstChild).not.toBeNull();
  });

  it("uses a constrained scroll layout on desktop (vertical scroll regression)", () => {
    const { container } = renderSidebar({ isOpen: true });
    const aside = container.querySelector("aside");
    expect(aside).not.toBeNull();
    // The aside must fill the fixed-height App wrapper and allow shrinking.
    expect(aside!.className).toMatch(/flex-1/);
    expect(aside!.className).toMatch(/min-h-0/);
    // Header/title rows stay fixed; only the section body scrolls.
    const header = container.querySelector("h2");
    expect(header?.parentElement?.className ?? "").toMatch(/shrink-0/);
    const scroller = Array.from(container.querySelectorAll("div")).find((d) =>
      d.className.includes("overflow-y-auto"),
    );
    expect(scroller).toBeDefined();
    expect(scroller!.className).toMatch(/flex-1/);
    expect(scroller!.className).toMatch(/min-h-0/);
  });
});

// The sidebar mirrors the TUI's pinned model rows (advisor/small/explorer/
// context/autocont); the auto-continue row is the web's only toggle surface
// for this feature besides the /autocontinue command.
describe("CoworkSidebar auto-continue row", () => {
  it("renders the autocont row (off + step-limit placeholder) and toggles the gate via setAutoContinue", async () => {
    const { api } = await import("../../api/client");
    const onModelClick = vi.fn();
    const { container } = renderSidebar({ onModelClick });

    // The row exists with the TUI-matching placeholder before any snapshot.
    expect(container.textContent).toContain("Auto-continue");
    expect(container.textContent).toContain("(step-limit only)");
    expect(container.textContent).toContain("○off");

    // Toggling fires the persisted global gate write (enabled only — the
    // judge model must not be touched by the checkbox).
    const checkbox = screen.getByRole("checkbox", { name: "Auto-continue enabled" });
    fireEvent.click(checkbox);
    await waitFor(() => expect(api.setAutoContinue).toHaveBeenCalledWith({ enabled: true }));

    // The model-name button opens the judge picker for the autocontinue purpose.
    fireEvent.click(screen.getByText("(step-limit only)"));
    expect(onModelClick).toHaveBeenCalledWith("autocontinue");
  });
});