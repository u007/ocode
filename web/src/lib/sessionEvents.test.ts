import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  routeBusEnvelope,
  reconcileOpenSessions,
  RECONCILE_PAGE_SIZE,
  LIVE_DELTA_FLUSH_MS,
  cancelLiveDeltas,
  __resetLastAppliedSeqForTests,
  type SessionEventRouter,
} from "./sessionEvents";
import type { BusEnvelope } from "./eventBus";
import type { ChatAction, ChatState } from "../stores/chatStore";
import { chatReducer, initialState } from "../stores/chatStore";
import { clearDraft, getDraft, setDraft } from "./tabDrafts";
import { clearQueue, getQueue, pushQueued } from "./tabQueue";

const mockGetSessionState = vi.fn();
const mockGetSession = vi.fn();
vi.mock("../api/client", () => ({
  api: {
    getSessionState: (...a: unknown[]) => mockGetSessionState(...a),
    getSession: (...a: unknown[]) => mockGetSession(...a),
  },
}));

function env(event: string, over: Partial<BusEnvelope> = {}): BusEnvelope {
  return { event, project: "/proj", session_id: "s1", seq: 1, data: {}, ...over };
}

function makeRouter(openIds: string[] = ["s1"]) {
  const actions: ChatAction[] = [];
  const projectActions: unknown[] = [];
  let state: ChatState = initialState;
  const router: SessionEventRouter = {
    openSessionIds: new Set(openIds),
    dispatch: (a) => {
      actions.push(a);
      state = chatReducer(state, a);
    },
    projectDispatch: (a) => void projectActions.push(a),
    getState: () => state,
  };
  return { router, actions, projectActions, getState: () => state };
}

describe("routeBusEnvelope", () => {
  beforeEach(() => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
  });
  afterEach(() => vi.restoreAllMocks());

  it("routes chat events to the open session's slice", () => {
    vi.useFakeTimers();
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(env("user_message", { data: { content: "hi" } }), router);
    routeBusEnvelope(env("text", { data: { delta: "hel" } }), router);
    routeBusEnvelope(env("text", { data: { delta: "lo" } }), router);
    // isStreaming flips synchronously; the delta text itself is coalesced.
    expect(getState().sessions["s1"].isStreaming).toBe(true);
    vi.advanceTimersByTime(LIVE_DELTA_FLUSH_MS);
    const slice = getState().sessions["s1"];
    expect(slice.messages[0]).toMatchObject({ role: "user", content: "hi" });
    expect(slice.live).toEqual([{ kind: "text", text: "hello" }]);
    vi.useRealTimers();
  });

  // Guards the fix for the desktop-app CPU spike: a reasoning stream can
  // emit far more, smaller deltas than plain text — without coalescing, each
  // one dispatched (and re-rendered) individually. See TODO.md's TUI
  // precedent for the identical class of bug.
  describe("thinking/text delta coalescing", () => {
    afterEach(() => vi.useRealTimers());

    it("coalesces many rapid deltas into a single dispatch per flush interval", () => {
      vi.useFakeTimers();
      const { router, actions, getState } = makeRouter(["s1"]);
      for (let i = 0; i < 50; i++) {
        routeBusEnvelope(env("thinking", { data: { delta: `${i} ` } }), router);
      }
      const liveDeltaCountBeforeFlush = actions.filter((a) => a.type === "LIVE_DELTA").length;
      expect(liveDeltaCountBeforeFlush).toBe(0);
      vi.advanceTimersByTime(LIVE_DELTA_FLUSH_MS);
      const liveDeltaCountAfterFlush = actions.filter((a) => a.type === "LIVE_DELTA").length;
      expect(liveDeltaCountAfterFlush).toBe(1);
      const expected = Array.from({ length: 50 }, (_, i) => `${i} `).join("");
      expect(getState().sessions["s1"].live).toEqual([{ kind: "thinking", text: expected }]);
    });

    it("flushes a pending delta immediately when the turn ends, losing no text", () => {
      vi.useFakeTimers();
      const { router, getState } = makeRouter(["s1"]);
      routeBusEnvelope(env("thinking", { data: { delta: "partial" } }), router);
      // turn_done fires before the flush timer would have — must not drop it.
      routeBusEnvelope(env("turn_done", { data: {} }), router);
      expect(getState().sessions["s1"].live).toEqual([{ kind: "thinking", text: "partial" }]);
      expect(getState().sessions["s1"].isStreaming).toBe(false);
    });

    it("flushes a pending delta immediately when a messages snapshot lands", () => {
      vi.useFakeTimers();
      const { router, getState } = makeRouter(["s1"]);
      routeBusEnvelope(env("text", { data: { delta: "partial" } }), router);
      routeBusEnvelope(env("messages", { data: [{ role: "assistant", content: "final" }] }), router);
      // The turn-boundary snapshot clears `live` regardless — this asserts the
      // buffered delta's now-cancelled timer doesn't fire later and resurrect
      // a stray live block after the snapshot committed.
      const slice = getState().sessions["s1"];
      expect(slice.live).toEqual([]);
      vi.advanceTimersByTime(LIVE_DELTA_FLUSH_MS * 2);
      expect(getState().sessions["s1"].live).toEqual([]);
    });

    it("cancelLiveDeltas drops a pending buffer without dispatching (tab close)", () => {
      vi.useFakeTimers();
      const { router, actions } = makeRouter(["s1"]);
      routeBusEnvelope(env("thinking", { data: { delta: "x" } }), router);
      cancelLiveDeltas("s1");
      vi.advanceTimersByTime(LIVE_DELTA_FLUSH_MS * 2);
      expect(actions.some((a) => a.type === "LIVE_DELTA")).toBe(false);
    });
  });

  it("permission_check surfaces and clears a status live part", () => {
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(
      env("permission_check", { data: { tool: "bash", model: "local/qwen3-4b", active: true } }),
      router,
    );
    expect(getState().sessions["s1"].live).toEqual([
      { kind: "status", text: "Checking permission for bash (local/qwen3-4b)…" },
    ]);
    routeBusEnvelope(
      env("permission_check", { data: { tool: "bash", model: "local/qwen3-4b", active: false } }),
      router,
    );
    expect(getState().sessions["s1"].live).toEqual([]);
  });

  it("advisor_checkpoint surfaces and clears a status live part", () => {
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(env("advisor_checkpoint", { data: { kind: "plan", active: true } }), router);
    expect(getState().sessions["s1"].live).toEqual([
      { kind: "status", text: "Advisor plan checkpoint — reviewing…" },
    ]);
    routeBusEnvelope(env("advisor_checkpoint", { data: { kind: "plan", active: false } }), router);
    expect(getState().sessions["s1"].live).toEqual([]);
  });

  it("turn lifecycle drives turn state (streaming flag)", () => {
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(env("turn_started", { data: { session_id: "s1" } }), router);
    expect(getState().sessions["s1"].turnActive).toBe(true);
    routeBusEnvelope(env("turn_done", { data: { session_id: "s1" } }), router);
    const slice = getState().sessions["s1"];
    expect(slice.turnActive).toBe(false);
    expect(slice.isStreaming).toBe(false);
    expect(slice.lastHeartbeatAt).toBeNull();
  });

  it("turn_error surfaces the error and clears turn state", () => {
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(env("turn_error", { data: { error: "boom" } }), router);
    const slice = getState().sessions["s1"];
    expect(slice.turnActive).toBe(false);
    expect(slice.error).toBe("boom");
  });

  // A failed turn must stop the spinner as well as the turn state: turn_error
  // is the only terminal frame the permission/question continuation paths emit
  // on the bus, so leaving isStreaming set makes a failed turn look like one
  // that is still streaming forever.
  it("turn_error stops the streaming spinner", () => {
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(env("user_message", { data: { content: "hi" } }), router);
    expect(getState().sessions["s1"].isStreaming).toBe(true);
    routeBusEnvelope(env("turn_error", { data: { error: "boom" } }), router);
    expect(getState().sessions["s1"].isStreaming).toBe(false);
  });

  it("session_bootstrap records the stage", () => {
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(env("session_bootstrap", { data: { stage: "mcp" } }), router);
    expect(getState().sessions["s1"].bootstrapStage).toBe("mcp");
  });

  it("session_started rekeys a new-* tab and keeps the routing set in sync", () => {
    const { router, actions, projectActions } = makeRouter(["new-123"]);
    setDraft("new-123", "draft");
    pushQueued("new-123", { kind: "message", text: "queued" });
    routeBusEnvelope(
      env("session_started", { session_id: "s9", data: { session_id: "s9", request_id: "new-123" } }),
      router,
    );
    expect(actions.some((a) => a.type === "REKEY_SESSION" && a.oldId === "new-123" && a.newId === "s9")).toBe(true);
    expect(projectActions).toEqual([
      expect.objectContaining({ type: "UPDATE_TAB_ID", oldId: "new-123", newId: "s9" }),
    ]);
    expect(router.openSessionIds.has("new-123")).toBe(false);
    expect(router.openSessionIds.has("s9")).toBe(true);
    expect(getDraft("new-123")).toBe("");
    expect(getDraft("s9")).toBe("draft");
    expect(getQueue("new-123")).toEqual([]);
    expect(getQueue("s9")).toEqual([{ kind: "message", text: "queued" }]);
    clearDraft("s9");
    clearQueue("s9");
  });

  it("does not create a slice for a never-opened session (memory-leak guard)", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(env("text", { session_id: "other", data: { delta: "x" } }), router);
    expect(warn).not.toHaveBeenCalled();
    expect(getState().sessions["other"]).toBeUndefined();
  });

  it("keeps updating an existing slice after its tab closes (late turn tail)", () => {
    const { router, getState } = makeRouter(["s1", "old"]);
    routeBusEnvelope(env("user_message", { session_id: "old", data: { content: "hi" } }), router);
    // Tab closes: the id leaves the routing set, but the slice shell remains
    // until RESET — late turn-tail events must still land on it.
    router.openSessionIds.delete("old");
    routeBusEnvelope(env("turn_done", { session_id: "old", data: {} }), router);
    expect(getState().sessions["old"]).toBeDefined();
    expect(getState().sessions["old"]?.turnActive).toBe(false);
  });

  it("status events do not create a slice for an unknown session but still patch process-level globals — and never the global model", () => {
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(
      env("status", { session_id: "ghost", data: { advisor_enabled: true, main_model: "m" } }),
      router,
    );
    expect(getState().sessions["ghost"]).toBeUndefined();
    expect(getState().advisorEnabled).toBe(true);
    // A status snapshot's main_model is that session's effective model (which
    // may be a per-session override). Feeding it into the global model is what
    // made one tab's model leak across every tab — the global stays at its
    // startup (config) value.
    expect(getState().model).toBeNull();
  });

  it("warns for a session-scoped event without any session id", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const { router } = makeRouter(["s1"]);
    routeBusEnvelope(env("text", { session_id: "", data: { delta: "x" } }), router);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining("without a session id"));
  });

  it("status events patch the open session's slice (per-session model included) and update global fields", () => {
    const { router, getState, actions } = makeRouter(["s1"]);
    routeBusEnvelope(
      env("status", {
        session_id: "s1",
        data: { session_id: "s1", session_title: "T", main_model: "m1", small_model: "sm" },
      }),
      router,
    );
    const slice = getState().sessions["s1"];
    expect(slice.tuiStatus?.session_title).toBe("T");
    // Per-session model lives on the slice snapshot only — no global SET_MODEL
    // is dispatched, so a model change on s1 never appears on other tabs.
    expect(slice.tuiStatus?.main_model).toBe("m1");
    expect(getState().model).toBeNull();
    expect(actions.some((a) => a.type === "SET_MODEL")).toBe(false);
    expect(getState().smallModel).toBe("sm");
  });

  it("messages snapshot replaces the transcript and clears live parts", () => {
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(env("messages", { data: [{ role: "assistant", content: "done" }] }), router);
    const slice = getState().sessions["s1"];
    expect(slice.messages).toEqual([{ role: "assistant", content: "done" }]);
    expect(slice.initialized).toBe(true);
    expect(slice.live).toEqual([]);
  });

  it("paginated prefix is preserved when a live snapshot extends loaded history", () => {
    const { router, getState } = makeRouter(["s1"]);
    // Seed a session with hasMore via MERGE_SNAPSHOT on a paginated page.
    router.dispatch({
      type: "MERGE_SNAPSHOT",
      sessionId: "s1",
      messages: [{ role: "user", content: "old1" }],
      total: 5,
    });
    // Live snapshot extends the loaded prefix by one.
    routeBusEnvelope(
      env("messages", {
        data: [
          { role: "user", content: "old1" },
          { role: "assistant", content: "new1" },
        ],
      }),
      router,
    );
    const slice = getState().sessions["s1"];
    expect(slice.messages).toHaveLength(2);
    expect(slice.messages[0].content).toBe("old1");
    expect(slice.messages[1].content).toBe("new1");
  });
});

describe("reconcileOpenSessions", () => {
  beforeEach(() => {
    mockGetSessionState.mockReset();
    mockGetSession.mockReset();
  });

  it("refetches state + transcript for every real open session, skipping new-* tabs", async () => {
    mockGetSessionState.mockResolvedValue({ bootstrap_stage: "ready", turn_active: false, last_seq: 10 });
    mockGetSession.mockResolvedValue({ messages: [{ role: "assistant", content: "rec" }], total: 1 });
    const { router, actions } = makeRouter(["s1"]);
    await reconcileOpenSessions(new Set(["s1", "new-9"]), router);

    expect(mockGetSessionState).toHaveBeenCalledTimes(1);
    expect(mockGetSessionState).toHaveBeenCalledWith("s1");
    expect(mockGetSession).toHaveBeenCalledWith("s1", { limit: RECONCILE_PAGE_SIZE });
    expect(actions.some((a) => a.type === "SET_TURN_STATE" && a.sessionId === "s1" && !a.turnActive)).toBe(true);
    expect(actions.some((a) => a.type === "MERGE_SNAPSHOT" && a.sessionId === "s1")).toBe(true);
  });

  it("continues when one session's reconcile fails", async () => {
    mockGetSessionState.mockRejectedValueOnce(new Error("404")).mockResolvedValueOnce({
      bootstrap_stage: "",
      turn_active: false,
      last_seq: 1,
    });
    mockGetSession.mockResolvedValue({ messages: [], total: 0 });
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const { router, actions } = makeRouter(["bad", "good"]);
    await reconcileOpenSessions(new Set(["bad", "good"]), router);
    expect(actions.filter((a) => a.type === "MERGE_SNAPSHOT").length).toBe(1);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining("bad"), expect.any(Error));
  });

  it("replays buffered live_frames through routeBusEnvelope, deduped against frames already applied live", async () => {
    __resetLastAppliedSeqForTests();
    vi.useFakeTimers();
    const { router, getState } = makeRouter(["s1"]);
    // A live envelope already landed for seq 1 during the reconnect race
    // (chatStore.tsx's MERGE_SNAPSHOT comment documents this ordering).
    routeBusEnvelope(env("text", { session_id: "s1", seq: 1, data: { delta: "hel" } }), router);
    vi.advanceTimersByTime(LIVE_DELTA_FLUSH_MS);

    mockGetSessionState.mockResolvedValue({
      bootstrap_stage: "",
      turn_active: true,
      last_seq: 2,
      live_frames: [
        { event: "text", seq: 1, data: { delta: "hel" } }, // already applied — must not duplicate
        { event: "text", seq: 2, data: { delta: "lo" } },
      ],
    });
    mockGetSession.mockResolvedValue({ messages: [], total: 0 });

    await reconcileOpenSessions(new Set(["s1"]), router);
    vi.advanceTimersByTime(LIVE_DELTA_FLUSH_MS);
    vi.useRealTimers();

    // Deduped: seq 1 skipped, so this is one merged bubble ("hel" + "lo"),
    // not "hel" replayed again ahead of "lo".
    expect(getState().sessions["s1"].live).toEqual([{ kind: "text", text: "hello" }]);
  });

  it("replays a buffered tool_start frame with no prior live activity", async () => {
    __resetLastAppliedSeqForTests();
    const { router, getState } = makeRouter(["s1"]);
    mockGetSessionState.mockResolvedValue({
      bootstrap_stage: "",
      turn_active: true,
      last_seq: 1,
      live_frames: [{ event: "tool_start", seq: 1, data: { tool: "bash", call_id: "c1" } }],
    });
    mockGetSession.mockResolvedValue({ messages: [], total: 0 });

    await reconcileOpenSessions(new Set(["s1"]), router);

    const live = getState().sessions["s1"].live;
    expect(live).toEqual([{ kind: "tool", tool: "bash", callId: "c1", command: undefined }]);
  });
});

describe("tool output streaming events", () => {
  it("routes tool_output to the call that produced it, not the newest bubble", () => {
    const { router, getState } = makeRouter();
    // Two calls in flight. The positional fallback attaches to the LAST pending
    // tool, so output for the first one is the case that catches a lost call_id.
    routeBusEnvelope(
      env("tool_start", { data: { tool: "bash", call_id: "c1", command: "ls" } }),
      router,
    );
    routeBusEnvelope(
      env("tool_start", { data: { tool: "read", call_id: "c2" } }),
      router,
    );
    routeBusEnvelope(
      env("tool_output", { data: { call_id: "c1", chunk: "partial…" } }),
      router,
    );

    const live = getState().sessions["s1"].live;
    expect(live[0]).toMatchObject({ callId: "c1", stream: "partial…" });
    expect(live[1]).toMatchObject({ callId: "c2" });
    expect((live[1] as { stream?: string }).stream).toBeUndefined();
  });

  it("carries call_id from tool_start and tool_result through to the store", () => {
    const { router, getState } = makeRouter();
    routeBusEnvelope(
      env("tool_start", { data: { tool: "bash", call_id: "c1" } }),
      router,
    );
    routeBusEnvelope(
      env("tool_start", { data: { tool: "read", call_id: "c2" } }),
      router,
    );
    // Result for the OLDER call — the fallback would misfile this on c2.
    routeBusEnvelope(
      env("tool_result", { data: { call_id: "c1", output: "done" } }),
      router,
    );

    const live = getState().sessions["s1"].live;
    expect(live[0]).toMatchObject({ callId: "c1", output: "done" });
    expect((live[1] as { output?: string }).output).toBeUndefined();
  });
});
