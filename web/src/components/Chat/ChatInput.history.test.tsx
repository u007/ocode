import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, act } from "@testing-library/react";
import ChatInput from "./ChatInput";
import { clearInputHistory, pushInputHistory } from "../../lib/tabInputHistory";
import { clearQueue, pushQueued } from "../../lib/tabQueue";
import { clearDraft } from "../../lib/tabDrafts";

// Controllable stand-in for the real useChat hook (mirrors ChatInput.test.tsx).
const sendMessage = vi.fn().mockResolvedValue(true);
const executeShell = vi.fn().mockResolvedValue({ output: "", exitCode: 0, error: "" });
vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    sendMessage,
    executeShell,
    stop: vi.fn(),
    resume: vi.fn(),
    isStreaming: false,
    pendingPermission: null,
  }),
}));

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    state: { activeProject: { path: "/tmp/proj" } },
    dispatch: vi.fn(),
  }),
}));

const TAB = "history-tab";

function ta(): HTMLTextAreaElement {
  return screen.getByPlaceholderText(/Type a message/i) as HTMLTextAreaElement;
}

/** Submit text through the composer's real submit path (Enter). */
async function submit(text: string) {
  fireEvent.change(ta(), { target: { value: text } });
  await act(async () => {
    fireEvent.keyDown(ta(), { key: "Enter" });
    // Let the in-flight submit guard release (queued as a microtask).
    await Promise.resolve();
    await Promise.resolve();
  });
}

function press(key: string, init: Record<string, unknown> = {}) {
  return fireEvent.keyDown(ta(), { key, ...init });
}

describe("ChatInput input history", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    sendMessage.mockClear();
    sendMessage.mockResolvedValue(true);
    clearInputHistory(TAB);
    clearQueue(TAB);
    clearDraft(TAB);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("recalls sent messages with ↑ and walks forward with ↓", async () => {
    render(<ChatInput sessionTabId={TAB} />);
    await submit("first");
    await submit("second");
    expect(ta().value).toBe("");

    press("ArrowUp");
    expect(ta().value).toBe("second");
    press("ArrowUp");
    expect(ta().value).toBe("first");
    // Clamped at the oldest entry.
    press("ArrowUp");
    expect(ta().value).toBe("first");

    press("ArrowDown");
    expect(ta().value).toBe("second");
    press("ArrowDown");
    // Past the newest entry → the (empty) pre-walk draft.
    expect(ta().value).toBe("");
  });

  it("stashes the in-progress draft and restores it past the newest entry", async () => {
    render(<ChatInput sessionTabId={TAB} />);
    await submit("sent");
    fireEvent.change(ta(), { target: { value: "my draft" } });

    press("ArrowUp");
    expect(ta().value).toBe("sent");
    press("ArrowDown");
    expect(ta().value).toBe("my draft");
  });

  it("queued-item recall takes precedence over sent history on an empty box", async () => {
    render(<ChatInput sessionTabId={TAB} />);
    pushInputHistory(TAB, "sent text");
    pushQueued(TAB, { kind: "message", text: "queued text" });

    press("ArrowUp");
    expect(ta().value).toBe("queued text");
    // Next ↑ leaves the queue and enters sent history, stashing the recall.
    press("ArrowUp");
    expect(ta().value).toBe("sent text");
    press("ArrowDown");
    expect(ta().value).toBe("queued text");
  });

  it("does not hijack ↑ when the caret is on a later line of a multi-line draft", () => {
    render(<ChatInput sessionTabId={TAB} />);
    pushInputHistory(TAB, "old sent");
    fireEvent.change(ta(), { target: { value: "line1\nline2" } });
    ta().setSelectionRange(8, 8); // inside "line2"

    const notPrevented = press("ArrowUp");
    expect(notPrevented).toBe(true);
    expect(ta().value).toBe("line1\nline2");
  });

  it("does not hijack ↓ when not walking history", async () => {
    render(<ChatInput sessionTabId={TAB} />);
    pushInputHistory(TAB, "old sent");
    fireEvent.change(ta(), { target: { value: "draft" } });

    const notPrevented = press("ArrowDown");
    expect(notPrevented).toBe(true);
    expect(ta().value).toBe("draft");
  });

  it("ignores arrows carrying a modifier", () => {
    render(<ChatInput sessionTabId={TAB} />);
    pushInputHistory(TAB, "old sent");

    press("ArrowUp", { metaKey: true });
    expect(ta().value).toBe("");
    press("ArrowUp", { shiftKey: true });
    expect(ta().value).toBe("");
  });

  it("excludes `!` shell commands from recall", async () => {
    render(<ChatInput sessionTabId={TAB} />);
    await submit("!echo hi");

    press("ArrowUp");
    expect(ta().value).toBe("");
  });

  it("collapses an immediate duplicate submission", async () => {
    render(<ChatInput sessionTabId={TAB} />);
    await submit("dup");
    await submit("dup");

    press("ArrowUp");
    expect(ta().value).toBe("dup");
    press("ArrowUp");
    // Only one entry, so ↑ stays put rather than showing a stale earlier value.
    expect(ta().value).toBe("dup");
  });
});
