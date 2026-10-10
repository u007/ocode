import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import ChatInput from "./ChatInput";
import { dispatchCommand, type CommandContext } from "./commands";
import { clearQueue, getQueue } from "../../lib/tabQueue";
import { clearCompaction, setCompactionState } from "../../lib/compactionState";
import { __resetSessionActivityForTests } from "../../lib/commandActivity";
import { clearDraft } from "../../lib/tabDrafts";

// /btw is an aside. The TUI has run it mid-stream since 0.8.55 (its
// `isInstantCmd` chain in internal/tui/model.go); the web queued EVERY slash
// command while busy, so an aside typed during a long turn sat invisible until
// the turn ended. These tests pin the web's bypass — and, just as important,
// that it stays narrow.
//
// Bypassing the queue is only sound because the server handles /btw mid-turn by
// injecting the message into the running turn (Handler.tryEnqueueInjection)
// instead of appending to the transcript; see internal/server/handler.go.

const chat = vi.hoisted(() => ({ streaming: false, interrupted: false, permission: null as object | null }));
const sendMessage = vi.fn().mockResolvedValue(true);
const executeShell = vi.fn().mockResolvedValue({ output: "ok", exitCode: 0 });
vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    sendMessage,
    executeShell,
    stop: vi.fn(),
    resume: vi.fn(),
    isStreaming: chat.streaming,
    wasInterrupted: chat.interrupted,
    pendingPermission: chat.permission,
  }),
}));
vi.mock("./SlashCommandMenu", () => ({ default: () => null }));

const btwSession = vi.fn<CommandContext["api"]["btwSession"]>();
const recapSession = vi.fn();
const onSlashCommand = vi.fn((text: string, id?: string | null) =>
  dispatchCommand(text, {
    // commandName/args are required by the type but dispatchCommand re-parses
    // them from `text`; only `api` and `getSessionId` matter here.
    commandName: "",
    args: "",
    api: { btwSession, recapSession } as unknown as CommandContext["api"],
    getSessionId: () => id ?? null,
    host: "user@remote",
  }),
);

const A = "btw-instant-a";

async function submit(text: string) {
  fireEvent.change(screen.getByRole("textbox"), { target: { value: text } });
  await act(async () => { fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" }); });
}

function composer() {
  // The real hook subscribes to chat context; the stub needs a changed prop to
  // get through ChatInput's memo boundary when its simulated state changes.
  return <ChatInput sessionTabId={A} onSlashCommand={onSlashCommand} isActive={!chat.streaming && !chat.interrupted && !chat.permission} />;
}

describe("/btw is dispatched while a turn is busy", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    chat.streaming = false;
    chat.interrupted = false;
    chat.permission = null;
    btwSession.mockResolvedValue({ status: "noted" });
    clearQueue(A);
    clearDraft(A);
    clearCompaction(A);
    __resetSessionActivityForTests();
  });

  it.each(["streaming", "permission", "interrupted"])("runs /btw during %s instead of queueing it", async (barrier) => {
    if (barrier === "streaming") chat.streaming = true;
    if (barrier === "permission") chat.permission = {};
    if (barrier === "interrupted") chat.interrupted = true;
    render(composer());

    await submit("/btw use tabs not spaces");

    expect(btwSession).toHaveBeenCalledExactlyOnceWith(A, "use tabs not spaces", "user@remote");
    expect(getQueue(A)).toEqual([]);
    // An aside is not a model turn: it must never reach sendMessage.
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it("runs the /by-the-way alias during a turn too", async () => {
    chat.streaming = true;
    render(composer());

    await submit("/by-the-way prefer 2-space indent");

    expect(btwSession).toHaveBeenCalledExactlyOnceWith(A, "prefer 2-space indent", "user@remote");
    expect(getQueue(A)).toEqual([]);
  });

  it("still queues /btw while a compaction is running", async () => {
    // Compaction replaces the transcript wholesale when it lands
    // (Handler.replaceSession), so a concurrently recorded aside would be
    // dropped. Instant bypasses turn busyness only, never compaction.
    render(composer());
    // setCompactionState notifies useCompactionState's subscribers directly, so
    // the mounted composer re-renders without an explicit rerender.
    await act(async () => { setCompactionState(A, { status: "active", startedAt: Date.now() }); });

    await submit("/btw use tabs not spaces");

    expect(btwSession).not.toHaveBeenCalled();
    expect(getQueue(A)).toEqual([{ kind: "command", text: "/btw use tabs not spaces" }]);
  });

  it("leaves every other slash command queued while busy", async () => {
    // The bypass must stay narrow: only commands with a server-side mid-turn
    // path may skip the queue.
    chat.streaming = true;
    render(composer());

    await submit("/recap please");

    expect(recapSession).not.toHaveBeenCalled();
    expect(btwSession).not.toHaveBeenCalled();
    expect(getQueue(A)).toEqual([{ kind: "command", text: "/recap please" }]);
  });
});
