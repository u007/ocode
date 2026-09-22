import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import ChatInput from "./ChatInput";

// Controllable stand-in for useChat: the composer only needs to know whether a
// Stop (wasInterrupted) or an LLM-loop error (turnError) is pending, and to be
// able to fire the retry action.
const chat = vi.hoisted(() => ({
  interrupted: false,
  turnError: false,
  streaming: false,
}));
const retryLastTurn = vi.fn().mockResolvedValue(true);

vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    sendMessage: vi.fn().mockResolvedValue(true),
    executeShell: vi.fn().mockResolvedValue({ output: "", exitCode: 0, error: "" }),
    stop: vi.fn(),
    resume: vi.fn(),
    retryLastTurn,
    isStreaming: chat.streaming,
    wasInterrupted: chat.interrupted,
    turnError: chat.turnError,
    pendingPermission: null,
    hasConversation: true,
  }),
}));

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    state: { activeProject: { path: "/tmp/proj" } },
    dispatch: vi.fn(),
  }),
}));

vi.mock("./SlashCommandMenu", () => ({ default: () => null }));

const retryBtn = () => screen.queryByRole("button", { name: "Retry the last message" });

function renderComposer() {
  return render(<ChatInput sessionTabId="ses-1" isActive />);
}

describe("composer retry action", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    chat.interrupted = false;
    chat.turnError = false;
    chat.streaming = false;
  });
  afterEach(() => vi.clearAllMocks());

  it("offers Retry after a user Stop", () => {
    chat.interrupted = true;
    renderComposer();
    expect(retryBtn()).toBeInTheDocument();
  });

  it("offers Retry after an LLM-loop error", () => {
    chat.turnError = true;
    renderComposer();
    expect(retryBtn()).toBeInTheDocument();
  });

  it("hides Retry when an idle turn had no stop or error", () => {
    renderComposer();
    expect(retryBtn()).not.toBeInTheDocument();
  });

  it("hides Retry while a turn is running", () => {
    chat.turnError = true;
    chat.streaming = true;
    renderComposer();
    expect(retryBtn()).not.toBeInTheDocument();
  });

  it("re-runs the last turn in place when clicked", () => {
    chat.turnError = true;
    renderComposer();
    fireEvent.click(retryBtn()!);
    expect(retryLastTurn).toHaveBeenCalledTimes(1);
  });

  it("retries a stopped conversation", () => {
    chat.interrupted = true;
    renderComposer();
    fireEvent.click(retryBtn()!);
    expect(retryLastTurn).toHaveBeenCalledTimes(1);
  });
});
