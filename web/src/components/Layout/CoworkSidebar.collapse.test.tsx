import { describe, expect, it, vi, afterEach } from "vitest";
import { render, cleanup } from "@testing-library/react";
import CoworkSidebar from "./CoworkSidebar";
import { ChatProvider } from "../../stores/chatStore";

vi.mock("../../stores/projectStore", () => ({
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
  },
  apiPath: (p: string) => p,
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
});
