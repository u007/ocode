import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  routeBusEnvelope,
  reconcileOpenSessions,
  RECONCILE_PAGE_SIZE,
  type SessionEventRouter,
} from "./sessionEvents";
import type { BusEnvelope } from "./eventBus";
import type { ChatAction, ChatState } from "../stores/chatStore";
import { chatReducer, initialState } from "../stores/chatStore";

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
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(env("user_message", { data: { content: "hi" } }), router);
    routeBusEnvelope(env("text", { data: { delta: "hel" } }), router);
    routeBusEnvelope(env("text", { data: { delta: "lo" } }), router);
    const slice = getState().sessions["s1"];
    expect(slice.messages[0]).toMatchObject({ role: "user", content: "hi" });
    expect(slice.live).toEqual([{ kind: "text", text: "hello" }]);
    expect(slice.isStreaming).toBe(true);
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

  it("session_bootstrap records the stage", () => {
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(env("session_bootstrap", { data: { stage: "mcp" } }), router);
    expect(getState().sessions["s1"].bootstrapStage).toBe("mcp");
  });

  it("session_started rekeys a new-* tab and keeps the routing set in sync", () => {
    const { router, actions, projectActions } = makeRouter(["new-123"]);
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
  });

  it("warns loudly for chat events of an unknown session (never a silent drop)", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(env("text", { session_id: "other", data: { delta: "x" } }), router);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining("other"));
    expect(getState().sessions["other"]).toBeUndefined();
  });

  it("warns for a session-scoped event without any session id", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const { router } = makeRouter(["s1"]);
    routeBusEnvelope(env("text", { session_id: "", data: { delta: "x" } }), router);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining("without a session id"));
  });

  it("status events patch the open session's slice and update global fields", () => {
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(
      env("status", {
        session_id: "s1",
        data: { session_id: "s1", session_title: "T", main_model: "m1", small_model: "sm" },
      }),
      router,
    );
    const slice = getState().sessions["s1"];
    expect(slice.tuiStatus?.session_title).toBe("T");
    expect(getState().model).toBe("m1");
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
    const actions: ChatAction[] = [];
    await reconcileOpenSessions(new Set(["s1", "new-9"]), (a) => actions.push(a));

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
    const actions: ChatAction[] = [];
    await reconcileOpenSessions(new Set(["bad", "good"]), (a) => actions.push(a));
    expect(actions.filter((a) => a.type === "MERGE_SNAPSHOT").length).toBe(1);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining("bad"), expect.any(Error));
  });
});
