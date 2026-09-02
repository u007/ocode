import { fireEvent, render, screen, act, within } from "@testing-library/react";
import { useEffect, useState } from "react";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";

import { api } from "../../api/client";
import { ChatProvider } from "../../stores/chatStore";
import { TerminalProvider, useTerminalState } from "../../stores/terminalStore";
import { BrowserTabsProvider } from "../../stores/browserTabsStore";
import { browserStore, browserActions } from "../../lib/browserStore";
import type { FocusedKind } from "../../lib/viewPersistence";
import UnifiedTabBar from "./UnifiedTabBar";

vi.mock("@/hooks/useTerminalConfig", () => ({
  useTerminalConfig: () => ({ available: true }),
}));

vi.mock("../../api/client", () => ({
  api: { setSessionTitle: vi.fn().mockResolvedValue(undefined), closeSession: vi.fn().mockResolvedValue({ cancelled: true }) },
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

function renderBar(focusedKind: FocusedKind = "chat") {
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

beforeEach(() => {
  window.localStorage.clear();
  // browserStore is a module singleton; without this a leaked tab: slice from
  // an earlier case could satisfy the open-slice assertion by accident.
  browserStore.setState(() => ({ byKey: {} }));
  openSessionTab.mockClear();
  closeSessionTab.mockClear();
  openNewSessionTab.mockClear();
  toggleSessionPicker.mockClear();
  projectDispatch.mockClear();
  (api.closeSession as unknown as ReturnType<typeof vi.fn>).mockClear?.();
  (api.setSessionTitle as unknown as ReturnType<typeof vi.fn>).mockClear?.();
  projectFake = {
    state: { activeProject: { path: "/proj", name: "proj" } },
    tabs: [{ id: "s1", projectPath: "/proj", title: "Chat One", activeSubTab: "chat" }],
    activeTabId: "s1",
  };
});

describe("UnifiedTabBar", () => {
  it("renders a chat pill with the chat emoji", () => {
    renderBar();
    expect(screen.getByText("Chat One")).toBeTruthy();
  });

  it("renders a Browser add button and opens a browser pill", () => {
    const { onFocusKindChange } = renderBar();
    const addBrowser = screen.getByRole("button", { name: /new browser tab/i });
    fireEvent.click(addBrowser);
    expect(onFocusKindChange).toHaveBeenCalledWith("browser");
    expect(screen.getByRole("tab", { name: /new tab/i })).toBeInTheDocument();
    // A new browser tab must also open its live page-state slice, or the
    // full-width BrowserPanel renders nothing. Exactly one slice, freshly
    // created (beforeEach cleared the store).
    const keys = Object.keys(browserStore.state.byKey).filter((k) => k.startsWith("tab:"));
    expect(keys).toHaveLength(1);
    expect(browserStore.state.byKey[keys[0]].panelOpen).toBe(true);
  });

  it("swaps the browser tab globe for a spinner while that tab is loading", () => {
    renderBar();
    fireEvent.click(screen.getByRole("button", { name: /new browser tab/i }));
    const key = Object.keys(browserStore.state.byKey).find((k) => k.startsWith("tab:"))!;
    const pill = screen.getByRole("tab", { name: /new tab/i });
    // Idle → identity globe visible (the add-button's own 🌐 is outside the pill).
    expect(within(pill).getByText("🌐")).toBeInTheDocument();
    // navigate() marks the slice loading → the globe yields to a spinner.
    act(() => browserActions.navigate(key as never, "https://example.com/"));
    expect(within(pill).queryByText("🌐")).not.toBeInTheDocument();
    // Title stays put (the swap is the leading icon slot only).
    expect(screen.getByText("New tab")).toBeInTheDocument();
    // Server nav event resolves the load → globe returns.
    act(() =>
      browserActions.applyNavEvent(key as never, {
        state_key: key,
        url: "https://example.com/",
        status: 200,
        mode: "local",
      }),
    );
    expect(within(pill).getByText("🌐")).toBeInTheDocument();
  });

  it("lists a persisted-but-never-activated terminal as a pill (peek, no pty)", () => {
    window.localStorage.setItem(
      "ocode.ui.terminals.project.v1",
      JSON.stringify({ version: 1, projects: { "/proj": { terminals: [{ id: "term-1-1", title: "Terminal 1" }], activeId: "term-1-1" } } }),
    );
    renderBar();
    expect(screen.getByText("Terminal 1")).toBeTruthy();
  });

  it("💬+ creates a new chat session and switches focus to chat", () => {
    const { onFocusKindChange } = renderBar("terminal");
    fireEvent.click(screen.getByLabelText("New chat session"));
    expect(openNewSessionTab).toHaveBeenCalledTimes(1);
    expect(onFocusKindChange).toHaveBeenCalledWith("chat");
  });

  it("X on a chat tab shows confirmation before closing (confirm -> closes backend)", async () => {
    renderBar();
    fireEvent.click(screen.getByLabelText("Close Chat One"));
    // X should not close immediately — it shows a confirmation dialog
    expect(closeSessionTab).not.toHaveBeenCalled();
    expect(screen.getByText(/Close chat tab\?/)).toBeInTheDocument();
    // Confirming closes it AND terminates the backend session
    fireEvent.click(screen.getByRole("button", { name: "Close tab" }));
    expect(closeSessionTab).toHaveBeenCalledWith("s1");
    // The closed session's backend must be released (cancel + agent teardown),
    // not just hidden — otherwise the server keeps running the turn nobody is
    // viewing. Mock api.closeSession asserts the fire-and-forget call.
    expect(api.closeSession).toHaveBeenCalledWith("s1");
  });

  it("X on a chat tab confirmation Cancel preserves the tab", async () => {
    renderBar();
    fireEvent.click(screen.getByLabelText("Close Chat One"));
    expect(screen.getByText(/Close chat tab\?/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(closeSessionTab).not.toHaveBeenCalled();
    expect(api.closeSession).not.toHaveBeenCalled();
    // Tab still visible
    expect(screen.getByText("Chat One")).toBeInTheDocument();
  });

  it("middle-click on a chat tab closes immediately without confirmation", async () => {
    renderBar();
    const pill = screen.getByRole("tab", { name: "Chat One" });
    fireEvent(pill, new MouseEvent("auxclick", { button: 1, bubbles: true }));
    expect(closeSessionTab).toHaveBeenCalledWith("s1");
    expect(api.closeSession).toHaveBeenCalledWith("s1");
    // No dialog should appear
    expect(screen.queryByText(/Close chat tab\?/)).not.toBeInTheDocument();
  });

  it("middle-click on a browser tab closes immediately without confirmation", async () => {
    renderBar();
    // Browser tab exists from beforeEach (id: b1, title: New tab)
    // The default projectFake has no browser tab; browser tabs come from provider's persisted state.
    // To have a browser tab, create one via the store before render.
    // However renderBar already sets up browser tabs via beforeEach? Let's open one explicitly.
    const addBtn = screen.getByRole("button", { name: /new browser tab/i });
    fireEvent.click(addBtn);
    expect(screen.getByRole("tab", { name: /New tab/ })).toBeInTheDocument();
    const pill = screen.getByRole("tab", { name: /New tab/ });
    fireEvent(pill, new MouseEvent("auxclick", { button: 1, bubbles: true }));
    // Browser close should not trigger chat close
    expect(closeSessionTab).not.toHaveBeenCalled();
    // The specific browser pill should be removed, but chat pill remains.
    // After closing, no browser pill should remain (we opened one and closed it).
    expect(screen.queryByRole("tab", { name: /New tab/ })).not.toBeInTheDocument();
    expect(screen.queryByText(/Close browser tab\?/)).not.toBeInTheDocument();
  });

  it("X on a browser tab shows confirmation before closing", async () => {
    renderBar();
    const addBtn = screen.getByRole("button", { name: /new browser tab/i });
    fireEvent.click(addBtn);
    expect(screen.getByRole("tab", { name: /New tab/ })).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("Close New tab"));
    expect(screen.getByText(/Close browser tab\?/)).toBeInTheDocument();
    // Dialog is open, tab is hidden from accessibility tree while modal is open,
    // so check via hidden-aware query or just ensure close not yet happened.
    expect(screen.queryByText(/Close browser tab\?/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Close tab" }));
    // After confirm, dialog closes and browser pill should be gone
    expect(screen.queryByText(/Close browser tab\?/)).not.toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: /New tab/, hidden: false } as any)).not.toBeInTheDocument();
  });

  it("⌨️+ creates a new terminal (visible as a pill) and switches focus to terminal", () => {
    // Terminal titles come from a module-level counter shared across this
    // whole test file (see terminalStore.tsx's bumpSeqPast) — assert a
    // "Terminal N" pill appeared, not a specific number.
    const { onFocusKindChange } = renderBar("chat");
    fireEvent.click(screen.getByLabelText("New terminal"));
    expect(onFocusKindChange).toHaveBeenCalledWith("terminal");
    expect(screen.getByText(/^Terminal \d+$/)).toBeTruthy();
  });

  it("clicking a peeked terminal pill switches focus to terminal", () => {
    window.localStorage.setItem(
      "ocode.ui.terminals.project.v1",
      JSON.stringify({ version: 1, projects: { "/proj": { terminals: [{ id: "term-1-1", title: "Terminal 1" }], activeId: "term-1-1" } } }),
    );
    const { onFocusKindChange } = renderBar("chat");
    fireEvent.click(screen.getByText("Terminal 1"));
    expect(onFocusKindChange).toHaveBeenCalledWith("terminal");
  });

  it("respects a previously saved merged tab order on render", () => {
    window.localStorage.setItem(
      "ocode.ui.terminals.project.v1",
      JSON.stringify({ version: 1, projects: { "/proj": { terminals: [{ id: "term-1-1", title: "Terminal 1" }], activeId: "term-1-1" } } }),
    );
    window.localStorage.setItem(
      "ocode.ui.tabOrder.v1",
      JSON.stringify({ version: 1, projects: { "/proj": ["term:term-1-1", "chat:s1"] } }),
    );
    renderBar();
    const labels = screen.getAllByText(/Chat One|Terminal 1/).map((el) => el.textContent);
    expect(labels).toEqual(["Terminal 1", "Chat One"]);
  });

  it("shows the Processes pinned tab and All sessions button", () => {
    renderBar();
    expect(screen.getByText("Processes")).toBeTruthy();
    expect(screen.getByText("All sessions")).toBeTruthy();
  });
});

// --- Terminal alert badge auto-clear (3s) timer -------------------------------
// The badge must appear on an alerted terminal and vanish 3s after the
// terminal tab gains focus; switching focus away before then must cancel the
// timer (badge persists), and a fresh bell while focused must restart the 3s
// window. These use fake timers so the 3s wait is instant.
describe("terminal alert badge auto-clear timer", () => {
  let capturedMarkAlerted: ((p: string, id: string) => void) | null = null;

  function SeedHarness() {
    const { activate, markAlerted } = useTerminalState();
    useEffect(() => {
      activate("/proj");
      markAlerted("/proj", "term-1-1");
      capturedMarkAlerted = markAlerted;
    }, [activate, markAlerted]);
    return null;
  }

  function renderFocused(initial: FocusedKind = "terminal") {
    function Wrapper() {
      const [kind, setKind] = useState<FocusedKind>(initial);
      return <UnifiedTabBar focusedKind={kind} onFocusKindChange={setKind} />;
    }
    const utils = render(
      <ChatProvider>
        <TerminalProvider>
          <BrowserTabsProvider>
            <SeedHarness />
            <Wrapper />
          </BrowserTabsProvider>
        </TerminalProvider>
      </ChatProvider>,
    );
    return utils;
  }

  const alertLabel = () => screen.getByRole("tab", { name: /Terminal 1/ }).getAttribute("aria-label") ?? "";

  beforeEach(() => {
    window.localStorage.setItem(
      "ocode.ui.terminals.project.v1",
      JSON.stringify({
        version: 1,
        projects: { "/proj": { terminals: [{ id: "term-1-1", title: "Terminal 1" }], activeId: "term-1-1" } },
      }),
    );
    capturedMarkAlerted = null;
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows the badge on the alerted active terminal and clears it after 3s", () => {
    renderFocused("terminal");
    expect(alertLabel()).toContain("has unread activity");
    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(alertLabel()).not.toContain("has unread activity");
  });

  it("keeps the badge when focus leaves the terminal before the 3s timer fires", () => {
    renderFocused("terminal");
    expect(alertLabel()).toContain("has unread activity");
    act(() => {
      fireEvent.click(screen.getByText("Chat One"));
    });
    act(() => {
      vi.advanceTimersByTime(3000);
    });
    // Timer was cancelled on focus change, so the badge survives the 3s window.
    expect(alertLabel()).toContain("has unread activity");
  });

  it("restarts the 3s window when a fresh bell arrives while the terminal is focused", () => {
    renderFocused("terminal");
    expect(alertLabel()).toContain("has unread activity");
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    // Fresh bell resets the 3s countdown.
    act(() => {
      capturedMarkAlerted?.("/proj", "term-1-1");
    });
    act(() => {
      vi.advanceTimersByTime(2000); // 4s since first bell, 2s since last -> still shown
    });
    expect(alertLabel()).toContain("has unread activity");
    act(() => {
      vi.advanceTimersByTime(1000); // 3s since the last bell -> cleared
    });
    expect(alertLabel()).not.toContain("has unread activity");
  });

  it("keeps the badge when focus switches to a different terminal before the 3s timer fires", () => {
    // Second terminal so switching active terminal is distinct from switching
    // to chat; the timer dependency includes activeTerminalId.
    window.localStorage.setItem(
      "ocode.ui.terminals.project.v1",
      JSON.stringify({
        version: 1,
        projects: {
          "/proj": {
            terminals: [
              { id: "term-1-1", title: "Terminal 1" },
              { id: "term-2-2", title: "Terminal 2" },
            ],
            activeId: "term-1-1",
          },
        },
      }),
    );
    renderFocused("terminal");
    expect(alertLabel()).toContain("has unread activity");
    act(() => {
      vi.advanceTimersByTime(1500);
    });
    // Switch active terminal to Terminal 2 (still focused on terminal kind).
    act(() => {
      fireEvent.click(screen.getByText("Terminal 2"));
    });
    act(() => {
      vi.advanceTimersByTime(3000);
    });
    // Timer was cancelled when activeTerminalId changed; badge on Terminal 1 remains.
    expect(alertLabel()).toContain("has unread activity");
  });
});
