import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { AssistantToggleButton, PulseAssistantWindow, usePulseAssistantHotkey } from "./PulseAssistantWindow";
import {
  ASSISTANT_BAR_HEIGHT,
  ASSISTANT_EDGE,
  ASSISTANT_MIN_WIDTH,
  clampRect,
  defaultRect,
  isNarrowViewport,
  parseRect,
  resetAssistantPrefsForTests,
  setAssistantMode,
  setAssistantOpen,
  setAssistantRect,
  toggleAssistantWindow,
  usePulseAssistantPrefs,
} from "./pulseAssistantPrefs";
import { OPEN_PULSE_ASSISTANT_SETTINGS_EVENT } from "../../lib/pulseAssistant";

const mockGetPulseAssistant = vi.fn();
const mockSetPulseModel = vi.fn();
const mockNewPulseChat = vi.fn();
const mockSelectPulseChat = vi.fn();
const mockListPulseChats = vi.fn();
vi.mock("../../api/client", () => ({
  api: {
    getPulseAssistant: (...a: unknown[]) => mockGetPulseAssistant(...a),
    setPulseModel: (...a: unknown[]) => mockSetPulseModel(...a),
    newPulseChat: (...a: unknown[]) => mockNewPulseChat(...a),
    selectPulseChat: (...a: unknown[]) => mockSelectPulseChat(...a),
    listPulseChats: (...a: unknown[]) => mockListPulseChats(...a),
  },
}));

// The real chat surface is covered by its own suites; here only what the drawer
// hands it is under test.
vi.mock("../Chat/ChatPanel", () => ({
  default: ({ sessionId, host }: { sessionId: string; host?: string }) => (
    <div data-testid="chat-panel" data-session={sessionId} data-host={host === undefined ? "none" : host} />
  ),
}));
vi.mock("../Chat/ChatInput", () => ({
  default: ({
    sessionTabId,
    onSlashCommand,
    quickActions,
  }: {
    sessionTabId: string;
    onSlashCommand?: unknown;
    quickActions?: boolean;
  }) => (
    <div
      data-testid="chat-input"
      data-quick-actions={quickActions === false ? "off" : "on"}
      data-session={sessionTabId}
      data-has-slash={onSlashCommand === undefined ? "no" : "yes"}
    />
  ),
}));
vi.mock("../Chat/PermissionDialog", () => ({ default: () => null }));
vi.mock("../Chat/QuestionDialog", () => ({ default: () => null }));
// The ask state is controllable so the minimise and auto-restore tests can raise
// an ask on the assistant session while the window is in any mode.
const chat = vi.hoisted(() => ({ pendingPermission: null as unknown, pendingQuestion: null as unknown }));
vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    pendingPermission: chat.pendingPermission,
    pendingQuestion: chat.pendingQuestion,
    hiddenQuestionRequestId: null,
    askContext: null,
    resolvePermission: vi.fn(),
    submitQuestionAnswers: vi.fn(),
    cancelQuestion: vi.fn(),
    hideQuestion: vi.fn(),
    hydratePendingAsks: vi.fn().mockResolvedValue(undefined),
  }),
}));
const mockWatchdog = vi.fn();
vi.mock("../../hooks/useTurnWatchdog", () => ({
  useTurnWatchdogAll: (...a: unknown[]) => mockWatchdog(...a),
}));
vi.mock("../Layout/ModelDialog", () => ({
  default: ({
    open,
    purpose,
    currentValues,
    onPick,
  }: {
    open: boolean;
    purpose: string;
    currentValues?: Record<string, string>;
    onPick: (p: string, id: string) => void;
  }) =>
    open ? (
      <div data-testid="model-dialog" data-purpose={purpose} data-current={currentValues?.[purpose]}>
        <button onClick={() => onPick(purpose, "anthropic/claude-x")}>pick x</button>
        <button onClick={() => onPick(purpose, "")}>clear</button>
      </div>
    ) : null,
}));

/** Opens the window through the shared store, then renders it. */
function mountDrawer() {
  setAssistantOpen(true);
  return render(<PulseAssistantWindow />);
}

beforeEach(() => {
  resetAssistantPrefsForTests();
  chat.pendingPermission = null;
  chat.pendingQuestion = null;
  mockGetPulseAssistant.mockReset();
  mockSetPulseModel.mockReset();
  mockWatchdog.mockReset();
  mockGetPulseAssistant.mockResolvedValue({ session_id: "pulse_abc", model: "openai/gpt-x" });
  window.localStorage.clear();
  vi.spyOn(console, "error").mockImplementation(() => {});
  vi.spyOn(console, "warn").mockImplementation(() => {});
});
afterEach(() => vi.restoreAllMocks());

describe("PulseAssistantWindow chat history", () => {
  it("New chat swaps the drawer to the fresh chat's session", async () => {
    mockNewPulseChat.mockResolvedValue({ session_id: "pulse_new", model: "openai/gpt-x" });
    mountDrawer();
    await screen.findByTestId("chat-panel");

    fireEvent.click(screen.getByRole("button", { name: "New chat" }));

    await waitFor(() =>
      expect(screen.getByTestId("chat-panel")).toHaveAttribute("data-session", "pulse_new"),
    );
    expect(mockNewPulseChat).toHaveBeenCalledTimes(1);
  });

  it("shows the server's refusal and keeps the current chat when New chat is busy", async () => {
    mockNewPulseChat.mockRejectedValue(new Error("session pulse_abc is mid-turn; try again when it finishes"));
    mountDrawer();
    await screen.findByTestId("chat-panel");

    fireEvent.click(screen.getByRole("button", { name: "New chat" }));

    expect(await screen.findByTestId("pulse-assistant-chat-error")).toHaveTextContent("mid-turn");
    expect(screen.getByTestId("chat-panel")).toHaveAttribute("data-session", "pulse_abc");
  });

  it("lists earlier chats and switches to the one picked", async () => {
    mockListPulseChats.mockResolvedValue({
      chats: [
        { session_id: "pulse_abc", title: "Pulse assistant", created_at: "", updated_at: "" },
        { session_id: "pulse_old", title: "Pulse assistant", created_at: "", updated_at: "" },
      ],
      total: 2,
      offset: 0,
      limit: 20,
      current: "pulse_abc",
    });
    mockSelectPulseChat.mockResolvedValue({ session_id: "pulse_old", model: "openai/gpt-x" });
    mountDrawer();
    await screen.findByTestId("chat-panel");

    fireEvent.click(screen.getByRole("button", { name: "Chat history" }));
    const rows = await screen.findAllByRole("button", { name: /Pulse assistant/ });
    fireEvent.click(rows[1]);

    await waitFor(() =>
      expect(screen.getByTestId("chat-panel")).toHaveAttribute("data-session", "pulse_old"),
    );
    expect(mockSelectPulseChat).toHaveBeenCalledWith("pulse_old");
    expect(mockListPulseChats).toHaveBeenCalledWith(0, 20);
  });
});

describe("PulseAssistantWindow content", () => {
  it("shows a starting line, then the real chat surface for the assistant session", async () => {
    mountDrawer();
    expect(screen.getByText("Starting assistant…")).toBeInTheDocument();

    const panel = await screen.findByTestId("chat-panel");
    expect(panel).toHaveAttribute("data-session", "pulse_abc");
    // Local-only: no host is ever passed.
    expect(panel).toHaveAttribute("data-host", "none");
    expect(screen.getByTestId("chat-input")).toHaveAttribute("data-session", "pulse_abc");
    // No onSlashCommand: every leading `/` is sent to the model as typed.
    expect(screen.getByTestId("chat-input")).toHaveAttribute("data-has-slash", "no");
    expect(screen.getByRole("button", { name: "gpt-x" })).toBeInTheDocument();
  });

  it("hides the composer's quick-actions bar", async () => {
    mountDrawer();
    expect(await screen.findByTestId("chat-input")).toHaveAttribute("data-quick-actions", "off");
  });

  it("labels the model button with the segment after the last slash, full id in the title", async () => {
    mockGetPulseAssistant.mockResolvedValue({
      session_id: "pulse_abc",
      model: "openrouter/thinkingmachines/inkling:free",
    });
    mountDrawer();

    const button = await screen.findByRole("button", { name: "inkling:free" });
    expect(button.title).toContain("openrouter/thinkingmachines/inkling:free");
    expect(button.querySelector(".truncate")).not.toBeNull();
  });

  it("calls an unset model the default", async () => {
    mockGetPulseAssistant.mockResolvedValue({ session_id: "pulse_abc", model: "" });
    mountDrawer();

    expect(await screen.findByRole("button", { name: "default model" })).toBeInTheDocument();
  });

  it("registers the assistant with the stall watchdog, since it has no tab", async () => {
    mountDrawer();
    await screen.findByTestId("chat-panel");

    const last = mockWatchdog.mock.calls[mockWatchdog.mock.calls.length - 1][0] as Set<string>;
    expect([...last]).toEqual(["pulse_abc"]);
  });

  it("shows the error with a Retry that tries again, never a blank drawer", async () => {
    mockGetPulseAssistant.mockRejectedValueOnce(new Error("503 unavailable"));
    mountDrawer();

    expect(await screen.findByTestId("pulse-assistant-error")).toHaveTextContent("503 unavailable");
    expect(screen.queryByTestId("chat-panel")).toBeNull();
    expect(console.error).toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(await screen.findByTestId("chat-panel")).toBeInTheDocument();
    expect(mockGetPulseAssistant).toHaveBeenCalledTimes(2);
  });

  it("closes from the X button and unmounts the window", async () => {
    mountDrawer();
    await screen.findByTestId("chat-panel");

    fireEvent.click(screen.getByRole("button", { name: "Close assistant" }));

    expect(screen.queryByTestId("pulse-assistant-window")).toBeNull();
    expect(window.localStorage.getItem("pulse.assistant.open")).toBe("0");
  });
});

describe("PulseAssistantWindow settings entry", () => {
  it("asks the app to open Settings on the Pulse assistant section", async () => {
    const heard = vi.fn();
    window.addEventListener(OPEN_PULSE_ASSISTANT_SETTINGS_EVENT, heard);
    mountDrawer();
    await screen.findByTestId("chat-panel");

    fireEvent.click(screen.getByRole("button", { name: "Assistant settings" }));

    expect(heard).toHaveBeenCalledTimes(1);
    window.removeEventListener(OPEN_PULSE_ASSISTANT_SETTINGS_EVENT, heard);
  });
});

describe("PulseAssistantWindow model chooser", () => {
  it("opens the chooser for the assistant slot and persists a pick, then re-reads the effective model", async () => {
    mockSetPulseModel.mockResolvedValue({ model: "anthropic/claude-x" });
    mountDrawer();
    fireEvent.click(await screen.findByRole("button", { name: "gpt-x" }));

    const dialog = screen.getByTestId("model-dialog");
    expect(dialog).toHaveAttribute("data-purpose", "pulse");
    expect(dialog).toHaveAttribute("data-current", "openai/gpt-x");

    mockGetPulseAssistant.mockResolvedValue({ session_id: "pulse_abc", model: "anthropic/claude-x" });
    fireEvent.click(screen.getByText("pick x"));

    await waitFor(() => expect(mockSetPulseModel).toHaveBeenCalledWith("anthropic/claude-x"));
    expect(await screen.findByRole("button", { name: "claude-x" })).toBeInTheDocument();
  });

  it("clears the slot with an empty model", async () => {
    mockSetPulseModel.mockResolvedValue({ model: "" });
    mountDrawer();
    fireEvent.click(await screen.findByRole("button", { name: "gpt-x" }));

    fireEvent.click(screen.getByText("clear"));

    await waitFor(() => expect(mockSetPulseModel).toHaveBeenCalledWith(""));
  });

  it("shows a failed model change inline and logs it", async () => {
    mockSetPulseModel.mockRejectedValue(new Error("nope"));
    mountDrawer();
    fireEvent.click(await screen.findByRole("button", { name: "gpt-x" }));

    fireEvent.click(screen.getByText("pick x"));

    expect(await screen.findByTestId("pulse-assistant-model-error")).toHaveTextContent("nope");
    expect(console.error).toHaveBeenCalled();
  });
});

describe("PulseAssistantWindow window modes", () => {
  it("minimises to its bar and keeps the chat mounted, so the draft and asks survive", async () => {
    mountDrawer();
    await screen.findByTestId("chat-panel");

    fireEvent.click(screen.getByRole("button", { name: "Minimise assistant" }));

    const frame = screen.getByTestId("pulse-assistant-window");
    expect(frame).toHaveAttribute("data-mode", "minimized");
    expect(frame.style.height).toBe(`${ASSISTANT_BAR_HEIGHT}px`);
    // Hidden, not unmounted: the transcript and composer are still there.
    expect(screen.getByTestId("chat-panel")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Restore assistant" }));
    expect(screen.getByTestId("pulse-assistant-window")).toHaveAttribute("data-mode", "normal");
  });

  it("maximises to the app inset and restores the saved rectangle", async () => {
    setAssistantRect({ x: 100, y: 90, width: 500, height: 600 });
    mountDrawer();
    await screen.findByTestId("chat-panel");
    expect(screen.getByTestId("pulse-assistant-window").style.left).toBe("100px");

    fireEvent.click(screen.getByRole("button", { name: "Maximise assistant" }));
    const maximised = screen.getByTestId("pulse-assistant-window");
    expect(maximised).toHaveAttribute("data-mode", "maximized");
    expect(maximised.style.left).toBe(`${ASSISTANT_EDGE}px`);
    expect(maximised.style.top).toBe(`${ASSISTANT_EDGE}px`);

    fireEvent.click(screen.getByRole("button", { name: "Restore size" }));
    const restored = screen.getByTestId("pulse-assistant-window");
    expect(restored).toHaveAttribute("data-mode", "normal");
    expect(restored.style.left).toBe("100px");
    expect(restored.style.width).toBe("500px");
  });

  it("is an inset sheet below the narrow breakpoint, with no drag and no resize", async () => {
    const original = window.innerWidth;
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 500 });
    try {
      mountDrawer();
      await screen.findByTestId("chat-panel");
      expect(screen.queryByTestId("pulse-assistant-grip")).toBeNull();
      expect(screen.queryByTestId("pulse-assistant-resize")).toBeNull();
      const frame = screen.getByTestId("pulse-assistant-window");
      expect(frame.style.left).toBe(`${ASSISTANT_EDGE}px`);
      expect(frame.style.right).toBe(`${ASSISTANT_EDGE}px`);
    } finally {
      Object.defineProperty(window, "innerWidth", { configurable: true, value: original });
    }
  });

  it("resizes from the keyboard on the corner handle, and the size is what the store keeps", async () => {
    setAssistantRect({ x: 100, y: 90, width: 420, height: 560 });
    mountDrawer();
    await screen.findByTestId("chat-panel");

    fireEvent.keyDown(screen.getByTestId("pulse-assistant-resize"), { key: "ArrowRight" });
    fireEvent.keyDown(screen.getByTestId("pulse-assistant-resize"), { key: "ArrowDown" });

    const frame = screen.getByTestId("pulse-assistant-window");
    expect(frame.style.width).toBe("436px");
    expect(frame.style.height).toBe("576px");
    expect(parseRect(window.localStorage.getItem("pulse.assistant.rect"))).toEqual({
      x: 100,
      y: 90,
      width: 436,
      height: 576,
    });
  });

  it("brings the window back from the bar when an ask arrives while it is minimised", async () => {
    const { rerender } = mountDrawer();
    await screen.findByTestId("chat-panel");
    fireEvent.click(screen.getByRole("button", { name: "Minimise assistant" }));
    expect(screen.getByTestId("pulse-assistant-window")).toHaveAttribute("data-mode", "minimized");

    chat.pendingPermission = { tool: "session_send", request_id: "req-1" };
    rerender(<PulseAssistantWindow />);

    await waitFor(() =>
      expect(screen.getByTestId("pulse-assistant-window")).toHaveAttribute("data-mode", "normal"),
    );
  });

  it("badges the bar when the window was minimised by hand with an ask already pending", async () => {
    chat.pendingQuestion = { request_id: "q-1", questions: [] };
    mountDrawer();
    await screen.findByTestId("chat-panel");
    fireEvent.click(screen.getByRole("button", { name: "Minimise assistant" }));

    expect(screen.getByTestId("pulse-assistant-window")).toHaveAttribute("data-mode", "minimized");
    expect(screen.getByTestId("pulse-assistant-ask-badge")).toHaveTextContent("Needs approval");
  });
});

describe("PulseAssistantWindow drag", () => {
  // jsdom has no PointerEvent, so the synthetic pointer events would carry no
  // coordinates and dnd-kit's PointerSensor would never activate. A MouseEvent
  // subclass with the pointer fields gives it the shape a browser sends.
  const original = window.PointerEvent;
  beforeEach(() => {
    class TestPointerEvent extends MouseEvent {
      pointerId: number;
      isPrimary: boolean;
      constructor(type: string, init: MouseEventInit & { pointerId?: number; isPrimary?: boolean } = {}) {
        super(type, init);
        this.pointerId = init.pointerId ?? 1;
        this.isPrimary = init.isPrimary ?? true;
      }
    }
    window.PointerEvent = TestPointerEvent as unknown as typeof PointerEvent;
  });
  afterEach(() => {
    window.PointerEvent = original;
  });

  it("moves the window by its grip and saves the new position on release", async () => {
    setAssistantRect({ x: 100, y: 90, width: 420, height: 560 });
    mountDrawer();
    await screen.findByTestId("chat-panel");
    const grip = screen.getByTestId("pulse-assistant-grip");
    const frame = screen.getByTestId("pulse-assistant-window");

    fireEvent.pointerDown(grip, { clientX: 200, clientY: 200, pointerId: 1, button: 0, isPrimary: true });
    // The first move past the sensor's distance activates the drag; the next one carries it.
    fireEvent.pointerMove(document, { clientX: 230, clientY: 215, pointerId: 1, isPrimary: true });
    fireEvent.pointerMove(document, { clientX: 260, clientY: 230, pointerId: 1, isPrimary: true });
    // dnd-kit applies the drag as a transform while the pointer is down.
    await waitFor(() => expect(frame.style.transform).toContain("translate3d(60px, 30px"));
    fireEvent.pointerUp(document, { clientX: 260, clientY: 230, pointerId: 1, isPrimary: true });
    // dnd-kit keeps its document click-suppression listener for 50 ms after a
    // drag ends (a setTimeout in its sensor's detach). A click inside that window
    // is swallowed, so wait it out before the next test clicks anything.
    await act(() => new Promise((resolve) => setTimeout(resolve, 60)));

    await waitFor(() =>
      expect(parseRect(window.localStorage.getItem("pulse.assistant.rect"))).toEqual({
        x: 160,
        y: 120,
        width: 420,
        height: 560,
      }),
    );
  });
});

describe("AssistantToggleButton and toggleAssistantWindow", () => {
  beforeEach(() => {
    resetAssistantPrefsForTests();
    window.localStorage.clear();
  });

  it("starts closed, opens on click, and closes on the next click", () => {
    render(<AssistantToggleButton />);
    const button = screen.getByRole("button", { name: "Toggle assistant" });
    expect(button).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(button);
    expect(button).toHaveAttribute("aria-pressed", "true");
    expect(window.localStorage.getItem("pulse.assistant.open")).toBe("1");

    fireEvent.click(button);
    expect(button).toHaveAttribute("aria-pressed", "false");
  });

  it("names the Ctrl shortcut off Apple platforms", () => {
    render(<AssistantToggleButton />);
    expect(screen.getByRole("button", { name: "Toggle assistant" })).toHaveAttribute(
      "title",
      "Toggle assistant (Ctrl+Shift+A)",
    );
  });

  it("restores a minimised window instead of closing it, so the key on the bar does not hide it", () => {
    setAssistantOpen(true);
    setAssistantMode("minimized");
    toggleAssistantWindow();
    expect(usePulseAssistantPrefsSnapshot()).toEqual({ open: true, mode: "normal" });

    toggleAssistantWindow();
    expect(usePulseAssistantPrefsSnapshot()).toEqual({ open: false, mode: "normal" });
  });
});

function usePulseAssistantPrefsSnapshot() {
  const { result } = renderHook(() => usePulseAssistantPrefs());
  return { open: result.current.open, mode: result.current.mode };
}

describe("Pulse assistant layout geometry", () => {
  const vp = { width: 1200, height: 800 };

  it("keeps the header on screen when a rectangle is dragged off the edge", () => {
    const r = clampRect({ x: 5000, y: 5000, width: 420, height: 560 }, vp);
    expect(r.x).toBe(vp.width - 420 - ASSISTANT_EDGE);
    expect(r.y).toBe(vp.height - ASSISTANT_BAR_HEIGHT - ASSISTANT_EDGE);

    const negative = clampRect({ x: -50, y: -50, width: 420, height: 560 }, vp);
    expect(negative.x).toBe(ASSISTANT_EDGE);
    expect(negative.y).toBe(ASSISTANT_EDGE);
  });

  it("floors the size at the minimum and caps it at the viewport", () => {
    const tiny = clampRect({ x: 0, y: 0, width: 10, height: 10 }, vp);
    expect(tiny.width).toBe(ASSISTANT_MIN_WIDTH);
    const huge = clampRect({ x: 0, y: 0, width: 5000, height: 5000 }, vp);
    expect(huge.width).toBe(vp.width - 2 * ASSISTANT_EDGE);
    expect(huge.height).toBe(vp.height - 2 * ASSISTANT_EDGE);
  });

  it("defaults to the bottom-right corner of the viewport, and the narrow breakpoint is 640px", () => {
    const d = defaultRect(vp);
    expect(d.x + d.width).toBeLessThanOrEqual(vp.width);
    expect(d.y + d.height).toBeLessThanOrEqual(vp.height);
    expect(isNarrowViewport({ width: 639, height: 800 })).toBe(true);
    expect(isNarrowViewport({ width: 640, height: 800 })).toBe(false);
  });
});

describe("usePulseAssistantPrefs", () => {
  it("defaults to closed, in normal mode, with no saved rectangle", () => {
    const { result } = renderHook(() => usePulseAssistantPrefs());
    expect(result.current.open).toBe(false);
    expect(result.current.mode).toBe("normal");
    expect(result.current.rect).toBeNull();
  });

  it("persists open, mode and rectangle, and restores them after a reset", () => {
    setAssistantOpen(true);
    setAssistantRect({ x: 40, y: 50, width: 480, height: 600 });
    window.localStorage.setItem("pulse.assistant.mode", "minimized");
    resetAssistantPrefsForTests();

    const { result } = renderHook(() => usePulseAssistantPrefs());
    expect(result.current.open).toBe(true);
    expect(result.current.mode).toBe("minimized");
    expect(result.current.rect).toEqual({ x: 40, y: 50, width: 480, height: 600 });
  });

  it("ignores a malformed stored rectangle and an unknown mode", () => {
    window.localStorage.setItem("pulse.assistant.rect", '{"x":"left","y":1,"width":2,"height":3}');
    window.localStorage.setItem("pulse.assistant.mode", "sideways");
    resetAssistantPrefsForTests();

    const { result } = renderHook(() => usePulseAssistantPrefs());
    expect(result.current.rect).toBeNull();
    expect(result.current.mode).toBe("normal");
  });

  it("survives a throwing localStorage, warning with the key", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("quota");
    });
    setAssistantOpen(true);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining("pulse.assistant.open"), expect.anything());
  });

  it("is one shared store: a write through one hook is seen by every other", () => {
    const first = renderHook(() => usePulseAssistantPrefs());
    const second = renderHook(() => usePulseAssistantPrefs());
    act(() => first.result.current.setOpen(true));
    expect(second.result.current.open).toBe(true);
  });
});

describe("usePulseAssistantHotkey", () => {
  it("toggles on `a`", () => {
    const toggle = vi.fn();
    renderHook(() => usePulseAssistantHotkey(toggle));

    fireEvent.keyDown(document.body, { key: "a" });

    expect(toggle).toHaveBeenCalledTimes(1);
  });

  it("is ignored in a textarea, an input, a dialog and with modifiers", () => {
    const toggle = vi.fn();
    renderHook(() => usePulseAssistantHotkey(toggle));
    const area = document.createElement("textarea");
    const input = document.createElement("input");
    const dialog = document.createElement("div");
    dialog.setAttribute("role", "dialog");
    const inner = document.createElement("button");
    dialog.appendChild(inner);
    document.body.append(area, input, dialog);

    fireEvent.keyDown(area, { key: "a" });
    fireEvent.keyDown(input, { key: "a" });
    fireEvent.keyDown(inner, { key: "a" });
    fireEvent.keyDown(document.body, { key: "a", metaKey: true });
    fireEvent.keyDown(document.body, { key: "a", ctrlKey: true });

    expect(toggle).not.toHaveBeenCalled();
    area.remove();
    input.remove();
    dialog.remove();
  });

  it("stops listening on unmount", () => {
    const toggle = vi.fn();
    const { unmount } = renderHook(() => usePulseAssistantHotkey(toggle));
    unmount();

    fireEvent.keyDown(document.body, { key: "a" });

    expect(toggle).not.toHaveBeenCalled();
  });
});
