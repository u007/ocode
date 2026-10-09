import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import {
  ASSISTANT_DEFAULT_WIDTH,
  ASSISTANT_MIN_WIDTH,
  PulseAssistantDrawer,
  usePulseAssistantHotkey,
  usePulseAssistantPrefs,
} from "./PulseAssistantDrawer";
import { OPEN_PULSE_ASSISTANT_SETTINGS_EVENT } from "../../lib/pulseAssistant";

const mockGetPulseAssistant = vi.fn();
const mockSetPulseModel = vi.fn();
vi.mock("../../api/client", () => ({
  api: {
    getPulseAssistant: (...a: unknown[]) => mockGetPulseAssistant(...a),
    setPulseModel: (...a: unknown[]) => mockSetPulseModel(...a),
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
vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    pendingPermission: null,
    pendingQuestion: null,
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

function mountDrawer(props: Partial<React.ComponentProps<typeof PulseAssistantDrawer>> = {}) {
  const onClose = vi.fn();
  const onWidthChange = vi.fn();
  const utils = render(
    <PulseAssistantDrawer width={420} onWidthChange={onWidthChange} onClose={onClose} {...props} />,
  );
  return { onClose, onWidthChange, ...utils };
}

beforeEach(() => {
  mockGetPulseAssistant.mockReset();
  mockSetPulseModel.mockReset();
  mockWatchdog.mockReset();
  mockGetPulseAssistant.mockResolvedValue({ session_id: "pulse_abc", model: "openai/gpt-x" });
  window.localStorage.clear();
  vi.spyOn(console, "error").mockImplementation(() => {});
  vi.spyOn(console, "warn").mockImplementation(() => {});
});
afterEach(() => vi.restoreAllMocks());

describe("PulseAssistantDrawer content", () => {
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

  it("closes from the X button", async () => {
    const { onClose } = mountDrawer();
    await screen.findByTestId("chat-panel");

    fireEvent.click(screen.getByRole("button", { name: "Close assistant" }));

    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

describe("PulseAssistantDrawer settings entry", () => {
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

describe("PulseAssistantDrawer model chooser", () => {
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

describe("PulseAssistantDrawer resize", () => {
  it("applies the width with a 60% ceiling and a 320px floor", async () => {
    mountDrawer({ width: 500 });
    await screen.findByTestId("chat-panel");

    const drawer = screen.getByTestId("pulse-assistant-drawer");
    expect(drawer.style.width).toBe("500px");
    expect(drawer.style.minWidth).toBe(`${ASSISTANT_MIN_WIDTH}px`);
    expect(drawer.style.maxWidth).toBe("60%");
  });

  it("widens and narrows from the keyboard on the drag handle", async () => {
    const { onWidthChange } = mountDrawer({ width: 400 });
    await screen.findByTestId("chat-panel");
    const handle = screen.getByTestId("pulse-assistant-resize");

    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    fireEvent.keyDown(handle, { key: "ArrowRight" });

    expect(onWidthChange).toHaveBeenNthCalledWith(1, 416);
    expect(onWidthChange).toHaveBeenNthCalledWith(2, 384);
  });
});

describe("usePulseAssistantPrefs", () => {
  it("defaults to closed at the default width", () => {
    const { result } = renderHook(() => usePulseAssistantPrefs());
    expect(result.current.open).toBe(false);
    expect(result.current.width).toBe(ASSISTANT_DEFAULT_WIDTH);
  });

  it("persists open state and width under pulse.assistant.* and restores them on remount", () => {
    const first = renderHook(() => usePulseAssistantPrefs());
    act(() => {
      first.result.current.setOpen(true);
      first.result.current.setWidth(555);
    });
    expect(window.localStorage.getItem("pulse.assistant.open")).toBe("1");
    expect(window.localStorage.getItem("pulse.assistant.width")).toBe("555");
    first.unmount();

    const second = renderHook(() => usePulseAssistantPrefs());

    expect(second.result.current.open).toBe(true);
    expect(second.result.current.width).toBe(555);
  });

  it("never persists a width below the floor, and ignores a stored one", () => {
    const first = renderHook(() => usePulseAssistantPrefs());
    act(() => first.result.current.setWidth(100));
    expect(first.result.current.width).toBe(ASSISTANT_MIN_WIDTH);
    first.unmount();

    window.localStorage.setItem("pulse.assistant.width", "50");
    const second = renderHook(() => usePulseAssistantPrefs());
    expect(second.result.current.width).toBe(ASSISTANT_DEFAULT_WIDTH);
  });

  it("survives a throwing localStorage, warning with the key", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });

    const { result } = renderHook(() => usePulseAssistantPrefs());
    act(() => result.current.setOpen(true));

    expect(result.current.open).toBe(true);
    const warned = vi.mocked(console.warn).mock.calls.map((c) => c.map(String).join(" ")).join("\n");
    expect(warned).toContain("pulse.assistant.open");
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
