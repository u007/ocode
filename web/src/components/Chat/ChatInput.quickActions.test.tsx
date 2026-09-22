import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import ChatInput from "./ChatInput";
import { clearQueue, getQueue } from "../../lib/tabQueue";
import { clearDraft } from "../../lib/tabDrafts";
import { clearCompaction, setCompactionState } from "../../lib/compactionState";

// Controllable stand-in for useChat so the context-aware Continue action can be
// driven between "idle" and "interrupted" without touching the real store.
const chat = vi.hoisted(() => ({ streaming: false, interrupted: false, permission: null as object | null, hasConversation: true }));
const sendMessage = vi.fn().mockResolvedValue(true);
const executeShell = vi.fn().mockResolvedValue({ output: "ok", exitCode: 0 });
const resume = vi.fn();
vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    sendMessage,
    executeShell,
    stop: vi.fn(),
    resume,
    isStreaming: chat.streaming,
    wasInterrupted: chat.interrupted,
    pendingPermission: chat.permission,
    hasConversation: chat.hasConversation,
  }),
}));
vi.mock("./SlashCommandMenu", () => ({ default: () => null }));

// Slash dispatch stub: the composer only needs to prove it routed the command
// through the shared pipeline with the right session id.
const onSlashCommand = vi.fn((_text: string, _id?: string | null) => ({ handled: true, accepted: true, startedTurn: false }));

const A = "quick-a";

// Look the pills up by their FULL accessible name (aria-label === title) — the
// send row has its own "Resume" button, so a /^Resume/ match would be ambiguous.
const compactBtn = () => screen.getByRole("button", { name: "Compact conversation context (/compact)" });
const continueBtn = () => screen.getByRole("button", { name: "Send 'continue' to keep the agent going" });
const quickResumeBtn = () => screen.getByRole("button", { name: "Resume the interrupted turn" });
const recapBtn = () => screen.getByRole("button", { name: "Generate session recap (/recap)" });

function composer(id = A) {
  // isActive changes with the simulated chat state so the rerender gets through
  // ChatInput's memo boundary, mirroring the compaction test's harness.
  return <ChatInput sessionTabId={id} onSlashCommand={onSlashCommand} isActive={!chat.streaming && !chat.interrupted && !chat.permission} />;
}

describe("composer quick actions", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    chat.streaming = false;
    chat.interrupted = false;
    chat.permission = null;
    chat.hasConversation = true;
    sendMessage.mockResolvedValue(true);
    clearQueue(A);
    clearDraft(A);
    clearCompaction(A);
  });
  afterEach(() => { vi.clearAllMocks(); });

  it("renders Compact, Continue and Recap below the composer", () => {
    render(composer());
    expect(screen.getByRole("toolbar", { name: "Quick actions" })).toBeInTheDocument();
    expect(compactBtn()).toBeInTheDocument();
    expect(continueBtn()).toBeInTheDocument();
    expect(recapBtn()).toBeInTheDocument();
  });

  it("places the strip below the send row", () => {
    render(composer());
    const send = screen.getByRole("button", { name: "Send" });
    const toolbar = screen.getByRole("toolbar", { name: "Quick actions" });
    // The toolbar must follow the send row in document order (i.e. render below it).
    expect(send.compareDocumentPosition(toolbar) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("routes /compact and /recap through the slash-command pipeline", async () => {
    render(composer());
    await act(async () => { fireEvent.click(compactBtn()); });
    expect(onSlashCommand).toHaveBeenCalledWith("/compact", A);
    await act(async () => { fireEvent.click(recapBtn()); });
    expect(onSlashCommand).toHaveBeenCalledWith("/recap", A);
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it("sends the literal 'continue' message and leaves the draft untouched", async () => {
    render(composer());
    const ta = screen.getByRole("textbox");
    fireEvent.change(ta, { target: { value: "half-written draft" } });
    await act(async () => { fireEvent.click(continueBtn()); });
    expect(sendMessage).toHaveBeenCalledWith("continue");
    expect(onSlashCommand).not.toHaveBeenCalled();
    expect(ta).toHaveValue("half-written draft");
  });

  it("resumes (not sends) when the turn was interrupted", async () => {
    chat.interrupted = true;
    render(composer());
    expect(screen.queryByRole("button", { name: "Send 'continue' to keep the agent going" })).not.toBeInTheDocument();
    await act(async () => { fireEvent.click(quickResumeBtn()); });
    expect(resume).toHaveBeenCalledTimes(1);
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it("queues /compact while streaming and runs it once the turn frees up", async () => {
    chat.streaming = true;
    const view = render(composer());
    await act(async () => { fireEvent.click(compactBtn()); });
    expect(onSlashCommand).not.toHaveBeenCalled();
    expect(getQueue(A)).toEqual([{ kind: "command", text: "/compact" }]);
    chat.streaming = false;
    await act(async () => { view.rerender(composer()); });
    expect(onSlashCommand).toHaveBeenCalledWith("/compact", A);
  });

  it("disables Compact while a compaction is already running", () => {
    setCompactionState(A, { status: "active", startedAt: Date.now() });
    render(composer());
    const btn = screen.getByRole("button", { name: "Compaction already in progress" });
    expect(btn).toBeDisabled();
    fireEvent.click(btn);
    expect(onSlashCommand).not.toHaveBeenCalled();
  });

  it("hides the whole strip on a new or empty session", () => {
    chat.hasConversation = false;
    render(composer());
    // The strip is gone entirely, not just individually disabled: there is
    // nothing to compact, continue, or recap yet.
    expect(screen.queryByRole("toolbar", { name: "Quick actions" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Compact conversation context (/compact)" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Send 'continue' to keep the agent going" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Generate session recap (/recap)" })).not.toBeInTheDocument();
  });

  it("reveals the strip once the session has conversation content", () => {
    chat.hasConversation = false;
    const empty = render(composer());
    expect(screen.queryByRole("toolbar", { name: "Quick actions" })).not.toBeInTheDocument();
    empty.unmount();
    chat.hasConversation = true;
    render(composer());
    expect(screen.getByRole("toolbar", { name: "Quick actions" })).toBeInTheDocument();
    expect(compactBtn()).toBeInTheDocument();
  });
});
