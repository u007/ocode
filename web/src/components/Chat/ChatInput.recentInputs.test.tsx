import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import ChatInput from "./ChatInput";
import { clearQueue } from "../../lib/tabQueue";
import { clearDraft } from "../../lib/tabDrafts";
import { clearCompaction } from "../../lib/compactionState";

// The composer reads its transcript + scroll signal through useChat, so this
// harness drives them from a mutable stand-in — the same shape as
// ChatInput.quickActions.test.tsx.
const chat = vi.hoisted(() => ({
  streaming: false,
  interrupted: false,
  permission: null as object | null,
  hasConversation: true,
  transcriptScrolledUp: false,
  recentInputs: [] as string[],
}));
vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    sendMessage: vi.fn().mockResolvedValue(true),
    executeShell: vi.fn().mockResolvedValue({ output: "ok", exitCode: 0 }),
    stop: vi.fn(),
    resume: vi.fn(),
    retryLastTurn: vi.fn(),
    isStreaming: chat.streaming,
    wasInterrupted: chat.interrupted,
    pendingPermission: chat.permission,
    hasConversation: chat.hasConversation,
    transcriptScrolledUp: chat.transcriptScrolledUp,
    recentInputs: chat.recentInputs,
  }),
}));
vi.mock("./SlashCommandMenu", () => ({ default: () => null }));

const A = "recent-inputs-a";
const onSlashCommand = vi.fn(() => ({ handled: true, accepted: true, startedTurn: false }));

function composer() {
  return <ChatInput sessionTabId={A} onSlashCommand={onSlashCommand} isActive />;
}

const strip = () => screen.queryByTestId("recent-inputs");
const stripLines = () =>
  Array.from(screen.getByTestId("recent-inputs").querySelectorAll("li")).map((li) => li.getAttribute("title"));

describe("composer recent-inputs strip", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    chat.streaming = false;
    chat.interrupted = false;
    chat.permission = null;
    chat.hasConversation = true;
    chat.transcriptScrolledUp = false;
    chat.recentInputs = [];
    clearQueue(A);
    clearDraft(A);
    clearCompaction(A);
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("is hidden while the transcript is at the tail (the default view)", () => {
    chat.recentInputs = ["an earlier ask"];
    chat.transcriptScrolledUp = false;
    render(composer());
    expect(strip()).toBeNull();
  });

  it("appears once the transcript is scrolled away from the tail", () => {
    chat.recentInputs = ["an earlier ask", "the latest ask"];
    chat.transcriptScrolledUp = true;
    render(composer());
    expect(strip()).toBeInTheDocument();
    expect(stripLines()).toEqual(["an earlier ask", "the latest ask"]);
  });

  it("stays hidden when scrolled up but the session has no prior input", () => {
    // A brand-new `new-*` tab: the strip must not reserve any chrome.
    chat.recentInputs = [];
    chat.transcriptScrolledUp = true;
    render(composer());
    expect(strip()).toBeNull();
  });

  it("renders above the attach/context pills, as the first row of the composer", () => {
    chat.recentInputs = ["an earlier ask"];
    chat.transcriptScrolledUp = true;
    render(composer());
    const stripEl = screen.getByTestId("recent-inputs");
    // The composer block is the strip's parent; the strip must precede the
    // textarea's wrapper so the recall band sits at the top of the stack.
    const block = stripEl.parentElement as HTMLElement;
    const textarea = block.querySelector("textarea");
    expect(textarea).toBeTruthy();
    const children = Array.from(block.children);
    expect(children.indexOf(stripEl)).toBeLessThan(
      children.findIndex((c) => c.contains(textarea)),
    );
  });

  it("keeps the strip out of the way of the file-attachment drop overlay ordering", () => {
    // Regression guard: the drag overlay is absolutely positioned and must stay
    // the last-painted layer, so the strip is asserted to be a plain in-flow
    // sibling rather than something absolutely positioned over the composer.
    chat.recentInputs = ["an earlier ask"];
    chat.transcriptScrolledUp = true;
    render(composer());
    const stripEl = screen.getByTestId("recent-inputs");
    expect(getComputedStyle(stripEl).position).not.toBe("absolute");
  });
});
