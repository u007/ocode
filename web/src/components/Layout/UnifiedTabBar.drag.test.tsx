import { fireEvent, render, screen, act } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";

// jsdom has no PointerEvent, which dnd-kit's PointerSensor relies on to activate
// a drag. Polyfill a minimal one so drag-and-drop can be simulated in tests.
if (typeof window.PointerEvent === "undefined") {
  class PointerEventPolyfill extends MouseEvent {
    pointerId: number;
    isPrimary: boolean;
    constructor(type: string, params: PointerEventInit = {}) {
      super(type, params);
      this.pointerId = params.pointerId ?? 0;
      this.isPrimary = params.isPrimary ?? true;
    }
  }
  // @ts-expect-error assigning polyfill to the global/window
  window.PointerEvent = PointerEventPolyfill;
}

import { ChatProvider } from "../../stores/chatStore";
import { TerminalProvider } from "../../stores/terminalStore";
import { BrowserTabsProvider } from "../../stores/browserTabsStore";
import UnifiedTabBar from "./UnifiedTabBar";

vi.mock("@/hooks/useTerminalConfig", () => ({
  useTerminalConfig: () => ({ available: true }),
}));

vi.mock("../../api/client", () => ({
  api: { setSessionTitle: vi.fn().mockResolvedValue(undefined) },
}));

let projectFake: {
  state: { activeProject: { path: string; name: string } | null };
  tabs: { id: string; projectPath: string; title: string; activeSubTab: "chat" }[];
  activeTabId: string | null;
};
const openSessionTab = vi.fn();
const closeSessionTab = vi.fn();
const openNewSessionTab = vi.fn(() => "new-1");
const toggleSessionPicker = vi.fn();
const projectDispatch = vi.fn();

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    state: projectFake.state,
    tabs: projectFake.tabs,
    activeTabId: projectFake.activeTabId,
    openSessionTab,
    closeSessionTab,
    openNewSessionTab,
    toggleSessionPicker,
    dispatch: projectDispatch,
  }),
}));

function renderBar(focusedKind: "chat" | "terminal" = "chat") {
  const onFocusKindChange = vi.fn();
  const utils = render(
    <ChatProvider>
      <TerminalProvider>
        <BrowserTabsProvider>
          <UnifiedTabBar focusedKind={focusedKind} onFocusKindChange={onFocusKindChange} />
        </BrowserTabsProvider>
      </TerminalProvider>
    </ChatProvider>,
  );
  return { onFocusKindChange, ...utils };
}

describe("UnifiedTabBar drag reorder", () => {
  beforeEach(() => {
    window.localStorage.clear();
    openSessionTab.mockClear();
    closeSessionTab.mockClear();
    openNewSessionTab.mockClear();
    toggleSessionPicker.mockClear();
    projectDispatch.mockClear();
    projectFake = {
      state: { activeProject: { path: "/proj", name: "proj" } },
      tabs: [
        { id: "s1", projectPath: "/proj", title: "Chat One", activeSubTab: "chat" },
        { id: "s2", projectPath: "/proj", title: "Chat Two", activeSubTab: "chat" },
      ],
      activeTabId: "s1",
    };
  });

  it("reorders tabs via drag and registers the new order immediately (does not snap back)", () => {
    const { container, unmount } = renderBar();

    const pills = () => screen.getAllByRole("tab").map((el) => el.getAttribute("aria-label"));
    expect(pills()).toEqual(["Chat One", "Chat Two"]);

    // jsdom has no layout, so give each sortable pill a real horizontal rect so
    // dnd-kit's collision detection can resolve the drop target.
    const tabs = screen.getAllByRole("tab") as HTMLElement[];
    tabs.forEach((el, i) => {
      el.getBoundingClientRect = () =>
        ({
          x: i * 100,
          y: 0,
          left: i * 100,
          top: 0,
          right: i * 100 + 100,
          bottom: 30,
          width: 100,
          height: 30,
          toJSON: () => {},
        }) as DOMRect;
    });

    const first = screen.getAllByRole("tab")[0] as HTMLElement;
    // Pointer drag with a PointerEvent polyfill (jsdom lacks PointerEvent, which
    // is what makes dnd-kit's PointerSensor activate). Pointerdown on the pill,
    // then move/up dispatched on document where the sensor attaches its listeners.
    act(() => {
      fireEvent.pointerDown(first, { clientX: 10, clientY: 5, pointerId: 1, isPrimary: true });
    });
    act(() => {
      fireEvent.pointerMove(document, { clientX: 60, clientY: 5, pointerId: 1, isPrimary: true });
    });
    act(() => {
      fireEvent.pointerMove(document, { clientX: 150, clientY: 5, pointerId: 1, isPrimary: true });
    });
    act(() => {
      fireEvent.pointerUp(document, { clientX: 150, clientY: 5, pointerId: 1, isPrimary: true });
    });

    // The order must reflect the drag immediately (state-driven), not snap back.
    expect(pills()).toEqual(["Chat Two", "Chat One"]);
    // ...and survive a reload via localStorage.
    expect(
      JSON.parse(window.localStorage.getItem("ocode.ui.tabOrder.v1")!).projects["/proj"],
    ).toEqual(["chat:s2", "chat:s1"]);
    // Sanity: container still rendered both pills.
    expect(container.querySelectorAll('[role="tab"]').length).toBe(2);

    // Hydration after reload: unmount and remount from the same localStorage so
    // the bar re-seeds its order from the persisted value (not from React state).
    act(() => {
      unmount();
    });
    const { container: reloaded } = renderBar();
    expect(pills()).toEqual(["Chat Two", "Chat One"]);
    expect(reloaded.querySelectorAll('[role="tab"]').length).toBe(2);
  });
});
