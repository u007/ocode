import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, act } from "@testing-library/react";
import ChatInput from "./ChatInput";
import { clearDraft } from "../../lib/tabDrafts";
import { patchViewerState, resetViewerStates, setViewerDoc } from "../../lib/viewerContext";

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
  findTabForSession: () => undefined,
  useProjectState: () => ({
    state: { activeProject: { path: "/tmp/proj" } },
    dispatch: vi.fn(),
  }),
}));

const KEY = "tab-1";
const CHIP_X = "Remove viewer context from this message";
const VIEWER_BLOCK = '@docs/a.md (markdown, § Install) selected: "foo"';

function getTextarea(): HTMLTextAreaElement {
  return screen.getByPlaceholderText(/Type a message/i) as HTMLTextAreaElement;
}

/** Types and submits, then runs past the 1.5 s idle debounce so the message is sent. */
async function send(text: string) {
  await act(async () => {
    const ta = getTextarea();
    fireEvent.change(ta, { target: { value: text } });
    fireEvent.keyDown(ta, { key: "Enter" });
  });
  await act(async () => {
    vi.advanceTimersByTime(1500);
  });
}

describe("ChatInput viewer context", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    sendMessage.mockClear();
    sendMessage.mockResolvedValue(true);
    resetViewerStates();
    clearDraft(KEY);
    setViewerDoc(KEY, { path: "docs/a.md", kind: "markdown" });
    patchViewerState(KEY, { path: "docs/a.md", location: { heading: "Install" }, selection: "foo" });
  });

  afterEach(() => {
    vi.useRealTimers();
    resetViewerStates();
  });

  it("prepends the viewer block to the message", async () => {
    render(<ChatInput sessionTabId={KEY} />);
    await send("why?");
    expect(sendMessage).toHaveBeenCalledTimes(1);
    expect(sendMessage).toHaveBeenCalledWith(`${VIEWER_BLOCK} why?`);
  });

  it("drops the viewer block for one message when its chip is removed, then includes it again", async () => {
    render(<ChatInput sessionTabId={KEY} />);
    fireEvent.click(screen.getByRole("button", { name: CHIP_X }));
    expect(screen.queryByRole("button", { name: CHIP_X })).toBeNull();

    await send("first");
    expect(sendMessage).toHaveBeenNthCalledWith(1, "first");

    expect(screen.getByRole("button", { name: CHIP_X })).toBeTruthy();
    await send("second");
    expect(sendMessage).toHaveBeenNthCalledWith(2, `${VIEWER_BLOCK} second`);
  });

  it("does not repeat the preview excerpt when it equals the live selection", async () => {
    render(
      <ChatInput
        sessionTabId={KEY}
        previewContext={{ path: "docs/a.md", label: "§ Install", excerpt: "foo" }}
      />,
    );
    await send("why?");
    const msg = sendMessage.mock.calls[0][0] as string;
    expect(msg.split("@docs/a.md").length - 1).toBe(1);
    expect(msg).toBe(`${VIEWER_BLOCK} why?`);
  });

  it("sends both the preview excerpt and a different live selection", async () => {
    patchViewerState(KEY, { path: "docs/a.md", selection: "beta" });
    render(
      <ChatInput
        sessionTabId={KEY}
        previewContext={{ path: "docs/a.md", label: "§ Install", excerpt: "alpha" }}
      />,
    );
    await send("why?");
    expect(sendMessage).toHaveBeenCalledWith(
      '@docs/a.md (markdown, § Install) selected: "beta" @docs/a.md (§ Install): "alpha" why?',
    );
  });
});
