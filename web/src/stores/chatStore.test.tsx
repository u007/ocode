import { describe, expect, it } from "vitest";
import { chatReducer, getSessionSlice, getTurnState, initialState } from "./chatStore";
import type { ChatState } from "./chatStore";

function initial(): ChatState {
  return initialState;
}

describe("chatStore per-session isolation", () => {
  it("ADD_MESSAGE only affects the targeted session", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "a",
      message: { role: "user", content: "hi a" },
    });
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "b",
      message: { role: "user", content: "hi b" },
    });
    expect(getSessionSlice(state, "a").messages).toEqual([
      { role: "user", content: "hi a" },
    ]);
    expect(getSessionSlice(state, "b").messages).toEqual([
      { role: "user", content: "hi b" },
    ]);
  });

  it("getSessionSlice returns the empty default for an unknown or null session", () => {
    const state = initial();
    expect(getSessionSlice(state, "unknown").messages).toEqual([]);
    expect(getSessionSlice(state, null).messages).toEqual([]);
  });

  it("RESET drops only the targeted session's slice", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "a",
      message: { role: "user", content: "hi a" },
    });
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "b",
      message: { role: "user", content: "hi b" },
    });
    state = chatReducer(state, { type: "RESET", sessionId: "a" });
    expect(getSessionSlice(state, "a").messages).toEqual([]);
    expect(getSessionSlice(state, "b").messages).toEqual([
      { role: "user", content: "hi b" },
    ]);
  });

  it("REKEY_SESSION moves a slice's content to the new id and drops the old key", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "new-123",
      message: { role: "user", content: "first turn" },
    });
    state = chatReducer(state, {
      type: "REKEY_SESSION",
      oldId: "new-123",
      newId: "sess-real",
    });
    expect(getSessionSlice(state, "sess-real").messages).toEqual([
      { role: "user", content: "first turn" },
    ]);
    expect(state.sessions["new-123"]).toBeUndefined();
  });

  it("REKEY_SESSION is a no-op when the old id has no slice (idempotent under a race)", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "REKEY_SESSION",
      oldId: "new-123",
      newId: "sess-real",
    });
    expect(state.sessions["new-123"]).toBeUndefined();
    expect(state.sessions["sess-real"]).toBeUndefined();
  });

  it("global fields (e.g. model) are unaffected by per-session actions", () => {
    let state = initial();
    state = chatReducer(state, { type: "SET_MODEL", model: "claude-sonnet-5" });
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "a",
      message: { role: "user", content: "hi" },
    });
    expect(state.model).toBe("claude-sonnet-5");
  });

  it("MERGE_SNAPSHOT marks the session initialized", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "MERGE_SNAPSHOT",
      sessionId: "a",
      messages: [{ role: "user", content: "hi" }],
      total: 1,
    });
    expect(getSessionSlice(state, "a").initialized).toBe(true);
  });

  it("SET_MESSAGES (mirror snapshot) commits history AND marks the session initialized", () => {
    let state = initial();
    // A mirror "messages" snapshot arriving before the history fetch resolves
    // must clear the tab spinner — the spinner keys off `initialized`.
    state = chatReducer(state, {
      type: "SET_MESSAGES",
      sessionId: "a",
      messages: [{ role: "user", content: "hi" }, { role: "assistant", content: "hello" }],
    });
    const slice = getSessionSlice(state, "a");
    expect(slice.messages).toHaveLength(2);
    expect(slice.initialized).toBe(true);
    expect(slice.live).toEqual([]);
  });

  it("MARK_INITIALIZED clears the loading flag without touching content", () => {
    let state = initial();
    // ChatPanel's initial-load effect hits this when the mirror already
    // populated the slice (live parts) before the fetch resolved — it must
    // mark the slice initialized without wiping the newer live state.
    state = chatReducer(state, {
      type: "LIVE_DELTA",
      sessionId: "a",
      kind: "text",
      delta: "hi",
    });
    expect(getSessionSlice(state, "a").initialized).toBe(false);
    state = chatReducer(state, { type: "MARK_INITIALIZED", sessionId: "a" });
    const slice = getSessionSlice(state, "a");
    expect(slice.initialized).toBe(true);
    expect(slice.live).toEqual([{ kind: "text", text: "hi" }]);
  });
});

describe("chatStore Part 05 turn/status state", () => {
  it("SET_TURN_STATE true activates the turn and stamps a heartbeat", () => {
    let state = initial();
    state = chatReducer(state, { type: "SET_TURN_STATE", sessionId: "a", turnActive: true });
    const s = getSessionSlice(state, "a");
    expect(s.turnActive).toBe(true);
    expect(s.lastHeartbeatAt).not.toBeNull();
    expect(s.turnStalled).toBe(false);
  });

  it("SET_TURN_STATE false clears turn state and the streaming flag", () => {
    let state = initial();
    state = chatReducer(state, { type: "SET_TURN_STATE", sessionId: "a", turnActive: true });
    state = chatReducer(state, { type: "SET_STREAMING", sessionId: "a", isStreaming: true });
    state = chatReducer(state, { type: "SET_TURN_STATE", sessionId: "a", turnActive: false });
    const s = getSessionSlice(state, "a");
    expect(s.turnActive).toBe(false);
    expect(s.lastHeartbeatAt).toBeNull();
    expect(s.isStreaming).toBe(false);
  });

  it("SET_TURN_HEARTBEAT refreshes lastHeartbeatAt and clears a stall", () => {
    let state = initial();
    state = chatReducer(state, { type: "SET_TURN_STATE", sessionId: "a", turnActive: true });
    state = chatReducer(state, { type: "SET_TURN_STALLED", sessionId: "a", stalled: true });
    state = chatReducer(state, { type: "SET_TURN_HEARTBEAT", sessionId: "a" });
    const s = getSessionSlice(state, "a");
    expect(s.lastHeartbeatAt).not.toBeNull();
    expect(s.turnStalled).toBe(false);
  });

  it("SET_BOOTSTRAP_STAGE records the stage", () => {
    let state = initial();
    state = chatReducer(state, { type: "SET_BOOTSTRAP_STAGE", sessionId: "a", stage: "mcp" });
    expect(getSessionSlice(state, "a").bootstrapStage).toBe("mcp");
  });

  it("SET_TUI_STATUS is per-session and marks status ready", () => {
    let state = initial();
    state = chatReducer(state, { type: "SET_TUI_STATUS", sessionId: "a", status: { session_title: "T" } });
    expect(getSessionSlice(state, "a").tuiStatus?.session_title).toBe("T");
    expect(getSessionSlice(state, "b").tuiStatus).toBeNull();
    expect(state.tuiStatusReady).toBe(true);
  });

  it("SET_STATUS_LOADING is per-session", () => {
    let state = initial();
    state = chatReducer(state, { type: "SET_STATUS_LOADING", sessionId: "a", loading: true });
    expect(getSessionSlice(state, "a").statusLoading).toBe(true);
    expect(getSessionSlice(state, "b").statusLoading).toBe(false);
  });

  it("getTurnState selector returns the session's turn fields", () => {
    let state = initial();
    state = chatReducer(state, { type: "SET_TURN_STATE", sessionId: "a", turnActive: true });
    state = chatReducer(state, { type: "SET_BOOTSTRAP_STAGE", sessionId: "a", stage: "ready" });
    const turn = getTurnState(state, "a");
    expect(turn.turnActive).toBe(true);
    expect(turn.bootstrapStage).toBe("ready");
    expect(turn.turnStalled).toBe(false);
  });
});
