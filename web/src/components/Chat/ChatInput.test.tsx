import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, act } from "@testing-library/react";
import ChatInput from "./ChatInput";

// Controllable stand-in for the real useChat hook so we can assert exactly how
// many times a message is submitted (the double-send bug under test).
const sendMessage = vi.fn().mockResolvedValue(true);
const executeShell = vi.fn().mockResolvedValue({ output: "", exitCode: 0, error: "" });

vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    sendMessage,
    executeShell,
    stop: vi.fn(),
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

function getTextarea(): HTMLTextAreaElement {
  return screen.getByPlaceholderText(/Type a message/i) as HTMLTextAreaElement;
}

async function tick() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("ChatInput submission", () => {
  beforeEach(() => {
    sendMessage.mockClear();
    sendMessage.mockResolvedValue(true);
  });

  it("sends exactly once on a single Enter", async () => {
    render(<ChatInput sessionTabId="new-1" />);
    const ta = getTextarea();
    fireEvent.change(ta, { target: { value: "hello" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
    });
    await tick();
    expect(sendMessage).toHaveBeenCalledTimes(1);
    expect(sendMessage).toHaveBeenCalledWith("hello");
  });

  it("does not double-send when Enter fires twice in the same tick (accidental duplicate)", async () => {
    render(<ChatInput sessionTabId="new-1" />);
    const ta = getTextarea();
    fireEvent.change(ta, { target: { value: "hello" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
      fireEvent.keyDown(ta, { key: "Enter" });
    });
    await tick();
    expect(sendMessage).toHaveBeenCalledTimes(1);
  });

  it("sends two distinct messages typed in sequence", async () => {
    render(<ChatInput sessionTabId="new-1" />);
    const ta = getTextarea();

    fireEvent.change(ta, { target: { value: "first" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
    });
    await tick();

    fireEvent.change(ta, { target: { value: "second" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
    });
    await tick();

    expect(sendMessage).toHaveBeenCalledTimes(2);
    expect(sendMessage).toHaveBeenNthCalledWith(1, "first");
    expect(sendMessage).toHaveBeenNthCalledWith(2, "second");
  });

  it("ignores empty/whitespace input", async () => {
    render(<ChatInput sessionTabId="new-1" />);
    const ta = getTextarea();
    fireEvent.change(ta, { target: { value: "   " } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
    });
    await tick();
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it("does not send while IME composition is confirming", async () => {
    render(<ChatInput sessionTabId="new-1" />);
    const ta = getTextarea();
    fireEvent.change(ta, { target: { value: "konnichiwa" } });
    const ev = new KeyboardEvent("keydown", { key: "Enter" });
    Object.defineProperty(ev, "isComposing", { value: true });
    await act(async () => {
      fireEvent(ta, ev);
    });
    await tick();
    expect(sendMessage).not.toHaveBeenCalled();
  });

  // --- Context file pill removal (per-message exclusion, loop-preserving) ---

  it("attaches context file paths and active editor as @path refs by default", async () => {
    render(
      <ChatInput
        sessionTabId="new-1"
        activeEditorContext={{ path: "src/active.ts" }}
        contextFilePaths={["src/active.ts", "src/other.ts"]}
      />,
    );
    const ta = getTextarea();
    fireEvent.change(ta, { target: { value: "hi" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
    });
    await tick();
    // active editor + the other context file (active deduped, appears once).
    expect(sendMessage).toHaveBeenCalledTimes(1);
    expect(sendMessage).toHaveBeenCalledWith("@src/active.ts @src/other.ts hi");
  });

  it("X on the active-editor pill drops it from this message's payload", async () => {
    render(
      <ChatInput
        sessionTabId="new-1"
        activeEditorContext={{ path: "src/active.ts" }}
        contextFilePaths={["src/other.ts"]}
      />,
    );
    // The active-editor chip is present and removable.
    const removeActive = screen.getByRole("button", {
      name: /remove src\/active\.ts from this message/i,
    });
    await act(async () => {
      fireEvent.click(removeActive);
    });

    const ta = getTextarea();
    fireEvent.change(ta, { target: { value: "hi" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
    });
    await tick();
    // active editor excluded; only the other context file remains.
    expect(sendMessage).toHaveBeenCalledWith("@src/other.ts hi");
    expect(sendMessage).not.toHaveBeenCalledWith(expect.stringContaining("@src/active.ts"));
  });

  it("X on a context-file pill drops only that file from the payload", async () => {
    render(
      <ChatInput
        sessionTabId="new-1"
        contextFilePaths={["src/a.ts", "src/b.ts", "src/c.ts"]}
      />,
    );
    // X the middle pill.
    const removeB = screen.getByRole("button", {
      name: /remove src\/b\.ts from this message/i,
    });
    await act(async () => {
      fireEvent.click(removeB);
    });

    const ta = getTextarea();
    fireEvent.change(ta, { target: { value: "go" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
    });
    await tick();
    expect(sendMessage).toHaveBeenCalledTimes(1);
    const payload = sendMessage.mock.calls[0][0];
    expect(payload).toContain("@src/a.ts");
    expect(payload).toContain("@src/c.ts");
    expect(payload).not.toContain("@src/b.ts");
  });

  it("excluded file re-attaches on the next message (loop inclusion untouched)", async () => {
    render(
      <ChatInput
        sessionTabId="new-1"
        contextFilePaths={["src/a.ts", "src/b.ts"]}
      />,
    );

    // First message: X out src/b.ts, then send.
    const removeB = screen.getByRole("button", {
      name: /remove src\/b\.ts from this message/i,
    });
    await act(async () => {
      fireEvent.click(removeB);
    });
    const ta = getTextarea();
    fireEvent.change(ta, { target: { value: "first" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
    });
    await tick();
    expect(sendMessage).toHaveBeenNthCalledWith(1, "@src/a.ts first");

    // Second message: no X — src/b.ts is back (excludedPaths cleared on send).
    fireEvent.change(ta, { target: { value: "second" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
    });
    await tick();
    expect(sendMessage).toHaveBeenNthCalledWith(2, "@src/a.ts @src/b.ts second");
  });
});
