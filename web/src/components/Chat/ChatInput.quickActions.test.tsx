import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import ChatInput from "./ChatInput";
import { clearQueue, getQueue } from "../../lib/tabQueue";
import { clearDraft } from "../../lib/tabDrafts";
import { clearCompaction, setCompactionState } from "../../lib/compactionState";
import { SEED_CHIPS } from "../../lib/quickActions";
import type { QuickActionChip } from "@/api/types";

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

// The strip is config-driven now, so the store is mocked with a state object
// BUILT HERE. Every other export stays the real one, so `visibleChips`,
// `chipDispatchKind` and the icon map are the production helpers the composer
// itself calls — a test that mocked those too would prove nothing.
const quickActions = vi.hoisted(() => ({ chips: [] as QuickActionChip[] }));
vi.mock("../../lib/quickActions", async () => {
  const actual = await vi.importActual<typeof import("../../lib/quickActions")>("../../lib/quickActions");
  return {
    ...actual,
    useQuickActions: () => ({
      chips: quickActions.chips,
      loading: false,
      error: null,
      revision: "r",
    }),
  };
});

// Slash dispatch stub: the composer only needs to prove it routed the command
// through the shared pipeline with the right session id.
const onSlashCommand = vi.fn((_text: string, _id?: string | null) => ({ handled: true, accepted: true, startedTurn: false }));

const A = "quick-a";

/** Fresh copies, so a test that mutates one never leaks into the next. */
const seeds = (): QuickActionChip[] => SEED_CHIPS.map((c) => ({ ...c }));
const chip = (over: Partial<QuickActionChip> & { id: string; label: string; message: string }): QuickActionChip => ({
  icon: "zap",
  mode: "send",
  ...over,
});

// Look the pills up by their FULL accessible name (aria-label === title). The
// titles are now `${label} — ${message.trim()}`, i.e. the user's own label and
// message. Matching exactly matters twice over: the send row has its own
// "Resume" button, so a /^Resume/ match is ambiguous, and a loose regex is how
// a pill silently stops being clickable in production.
const compactBtn = () => screen.getByRole("button", { name: "Compact — /compact" });
const continueBtn = () => screen.getByRole("button", { name: "Continue — continue" });
const quickResumeBtn = () => screen.getByRole("button", { name: "Continue — resume the interrupted turn" });
const recapBtn = () => screen.getByRole("button", { name: "Recap — /recap" });
const strip = () => screen.queryByRole("toolbar", { name: "Quick actions" });

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
    quickActions.chips = seeds();
    clearQueue(A);
    clearDraft(A);
    clearCompaction(A);
  });
  afterEach(() => { vi.clearAllMocks(); });

  it("renders the configured chips below the composer", () => {
    render(composer());
    expect(strip()).toBeInTheDocument();
    expect(compactBtn()).toBeInTheDocument();
    expect(continueBtn()).toBeInTheDocument();
    expect(recapBtn()).toBeInTheDocument();
  });

  it("renders the configured chips in configured order, not in seed order", () => {
    quickActions.chips = [
      chip({ id: "z", label: "Zed", icon: "bug", message: "z" }),
      chip({ id: "a", label: "Ay", icon: "zap", message: "a" }),
    ];
    render(composer());
    const names = screen.getAllByRole("button").map((b) => b.textContent ?? "");
    expect(names.indexOf("Zed")).toBeGreaterThanOrEqual(0);
    expect(names.indexOf("Ay")).toBeGreaterThanOrEqual(0);
    expect(names.indexOf("Zed")).toBeLessThan(names.indexOf("Ay"));
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

  it("resumes (not sends) when the turn was interrupted, and keeps the configured label", async () => {
    chat.interrupted = true;
    render(composer());
    expect(screen.queryByRole("button", { name: "Continue — continue" })).not.toBeInTheDocument();
    expect(quickResumeBtn()).toBeInTheDocument();
    // The seed governs the BEHAVIOUR; the label stays as the user configured it
    // (the hint moves to the tooltip).
    expect(quickResumeBtn()).toHaveTextContent("Continue");
    await act(async () => { fireEvent.click(quickResumeBtn()); });
    expect(resume).toHaveBeenCalledTimes(1);
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it("lets the continue seed resume a fill-mode chip instead of filling the box", async () => {
    chat.interrupted = true;
    quickActions.chips = [chip({ id: "continue", label: "Continue", icon: "play", message: "keep going", mode: "fill", seed: "continue" })];
    render(composer());
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Continue — resume the interrupted turn" })); });
    expect(resume).toHaveBeenCalledTimes(1);
    expect(sendMessage).not.toHaveBeenCalled();
    expect(getQueue(A)).toEqual([]);
    // Resume wins over fill: it is a turn-lifecycle action, so putting the
    // message in the box would be the wrong outcome here.
    expect(screen.getByRole("textbox")).toHaveValue("");
  });

  it("a fill chip populates the composer without sending or queueing", async () => {
    quickActions.chips = [chip({ id: "x", label: "Draft it", message: "write a test for quick_actions", mode: "fill" })];
    render(composer());
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Draft it — write a test for quick_actions" })); });
    expect(screen.getByRole("textbox")).toHaveValue("write a test for quick_actions");
    expect(sendMessage).not.toHaveBeenCalled();
    expect(onSlashCommand).not.toHaveBeenCalled();
    expect(getQueue(A)).toEqual([]);
  });

  it("a send chip dispatches immediately", async () => {
    quickActions.chips = [chip({ id: "x", label: "Go", message: "go test ./..." })];
    render(composer());
    fireEvent.click(screen.getByRole("button", { name: "Go — go test ./..." }));
    await waitFor(() => expect(sendMessage).toHaveBeenCalledWith("go test ./..."));
    expect(onSlashCommand).not.toHaveBeenCalled();
  });

  it("routes a slash-command chip through the command pipeline, not as a message", async () => {
    quickActions.chips = [chip({ id: "x", label: "Review", message: "/review" })];
    render(composer());
    fireEvent.click(screen.getByRole("button", { name: "Review — /review" }));
    await waitFor(() => expect(onSlashCommand).toHaveBeenCalledWith("/review", A));
    expect(sendMessage).not.toHaveBeenCalled();
  });

  // Review Focus #3: the queue holds the RESOLVED TEXT, not a chip id, so
  // editing or deleting the chip mid-queue cannot corrupt the pending dispatch.
  it("queues the resolved text, so deleting a chip mid-queue does not cancel it", async () => {
    chat.streaming = true;
    quickActions.chips = [chip({ id: "x", label: "Run tests", message: "go test ./..." })];
    const view = render(composer());
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Run tests — go test ./..." })); });
    expect(onSlashCommand).not.toHaveBeenCalled();
    expect(getQueue(A)).toEqual([{ kind: "message", text: "go test ./..." }]);

    // The chip is gone from the config; the queued dispatch must still run.
    quickActions.chips = [];
    chat.streaming = false;
    await act(async () => { view.rerender(composer()); });
    expect(sendMessage).toHaveBeenCalledWith("go test ./...");
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

  it("disables a /compact chip while a compaction is already running", async () => {
    quickActions.chips = [chip({ id: "c", label: "Compact", icon: "archive", message: "/compact" })];
    await act(async () => { setCompactionState(A, { status: "active", startedAt: Date.now() }); });
    render(composer());
    const btn = screen.getByRole("button", { name: "Compact — /compact" });
    expect(btn).toBeDisabled();
    fireEvent.click(btn);
    expect(onSlashCommand).not.toHaveBeenCalled();
  });

  it("leaves a non-compacting chip enabled while a compaction is running", async () => {
    await act(async () => { setCompactionState(A, { status: "active", startedAt: Date.now() }); });
    render(composer());
    expect(compactBtn()).toBeDisabled();
    expect(recapBtn()).toBeEnabled();
  });

  it("hides seeded chips on an empty session but still shows a custom chip", () => {
    // The wrapper gate cannot suppress a chip that is always useful: an
    // always-visible custom chip must survive an empty session.
    quickActions.chips = [
      ...seeds(),
      chip({ id: "x", label: "Run tests", icon: "flask-conical", message: "run tests", mode: "fill" }),
    ];
    chat.hasConversation = false;
    render(composer());
    expect(screen.queryByRole("button", { name: "Compact — /compact" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Continue — continue" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Recap — /recap" })).not.toBeInTheDocument();
    expect(strip()).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Run tests — run tests" })).toBeInTheDocument();
  });

  it("unmounts the strip when nothing is visible", () => {
    quickActions.chips = seeds();
    chat.hasConversation = false;
    render(composer());
    expect(strip()).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Compact — /compact" })).not.toBeInTheDocument();
  });

  it("unmounts the strip when the configured list is empty", () => {
    quickActions.chips = [];
    render(composer());
    expect(strip()).not.toBeInTheDocument();
  });

  it("reveals the seeded chips once the session has conversation content", () => {
    chat.hasConversation = false;
    const empty = render(composer());
    expect(strip()).not.toBeInTheDocument();
    empty.unmount();
    chat.hasConversation = true;
    render(composer());
    expect(strip()).toBeInTheDocument();
    expect(compactBtn()).toBeInTheDocument();
  });
});