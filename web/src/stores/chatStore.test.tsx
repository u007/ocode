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

  it("MERGE_SNAPSHOT during an active turn preserves streamed messages and live parts", () => {
    // Regression: the server persists a transcript only after the turn's
    // Step returns, so a snapshot reconciled mid-turn (seq gap / bus
    // reconnect) is staler than memory. Applying it used to regress the
    // transcript to the pre-turn state and wipe the live buffer — the
    // "chat stuck after some messages" bug.
    let state = initial();
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "a",
      message: { role: "user", content: "fix the bug" },
    });
    state = chatReducer(state, {
      type: "LIVE_DELTA",
      sessionId: "a",
      kind: "text",
      delta: "working on it",
    });
    state = chatReducer(state, { type: "SET_TURN_STATE", sessionId: "a", turnActive: true });
    // Disk still holds only the opening user message when the reconcile lands.
    state = chatReducer(state, {
      type: "MERGE_SNAPSHOT",
      sessionId: "a",
      messages: [{ role: "user", content: "fix the bug" }],
      total: 1,
    });
    const slice = getSessionSlice(state, "a");
    expect(slice.turnActive).toBe(true);
    expect(slice.live).toHaveLength(1);
    expect(slice.live[0]).toMatchObject({ kind: "text", text: "working on it" });
    expect(slice.messages).toEqual([{ role: "user", content: "fix the bug" }]);
    // Metadata still refreshes so spinners/hasMore stay truthful.
    expect(slice.initialized).toBe(true);
  });

  it("MERGE_SNAPSHOT populates a virgin slice even when a turn is active (fresh page mid-turn)", () => {
    // A freshly loaded page reconciles mid-turn: SET_TURN_STATE(true) lands
    // on a never-populated slice, then the disk snapshot arrives. There is
    // no in-memory state newer than disk, so the snapshot must apply —
    // otherwise the tab stays empty until turn_done.
    let state = initial();
    state = chatReducer(state, { type: "SET_TURN_STATE", sessionId: "a", turnActive: true });
    state = chatReducer(state, {
      type: "MERGE_SNAPSHOT",
      sessionId: "a",
      messages: [
        { role: "user", content: "earlier question" },
        { role: "assistant", content: "earlier answer" },
        { role: "user", content: "follow-up" },
      ],
      total: 3,
    });
    const slice = getSessionSlice(state, "a");
    expect(slice.turnActive).toBe(true);
    expect(slice.messages).toHaveLength(3);
    expect(slice.initialized).toBe(true);
  });

  it("MERGE_SNAPSHOT does not clobber live parts on an empty-messages slice mid-turn", () => {
    // Deltas can land before any committed message exists (page reload during
    // a stream: the mirror reconnects before the history fetch resolves).
    // `live` is protectable state and must survive the merge — but the disk
    // snapshot must still populate `messages`: an empty messages array holds
    // nothing newer than disk, and dropping the snapshot left the reloaded
    // chat blank (no history, input only) until turn_done.
    let state = initial();
    state = chatReducer(state, {
      type: "LIVE_DELTA",
      sessionId: "a",
      kind: "text",
      delta: "streaming",
    });
    state = chatReducer(state, { type: "SET_TURN_STATE", sessionId: "a", turnActive: true });
    state = chatReducer(state, {
      type: "MERGE_SNAPSHOT",
      sessionId: "a",
      messages: [{ role: "user", content: "fix the bug" }],
      total: 1,
    });
    const slice = getSessionSlice(state, "a");
    expect(slice.live).toHaveLength(1);
    expect(slice.live[0]).toMatchObject({ kind: "text", text: "streaming" });
    expect(slice.messages).toEqual([{ role: "user", content: "fix the bug" }]);
    expect(slice.initialized).toBe(true);
  });

  it("MERGE_SNAPSHOT mid-turn keeps committed messages authoritative over the disk page", () => {
    // Once the slice holds committed messages (turn-boundary SET_MESSAGES or
    // a partial commit), a mid-turn disk page is staler than memory and must
    // not regress them.
    let state = initial();
    state = chatReducer(state, {
      type: "SET_MESSAGES",
      sessionId: "a",
      messages: [
        { role: "user", content: "fix the bug" },
        { role: "assistant", content: "working on it" },
      ],
    });
    state = chatReducer(state, { type: "SET_TURN_STATE", sessionId: "a", turnActive: true });
    state = chatReducer(state, {
      type: "MERGE_SNAPSHOT",
      sessionId: "a",
      messages: [{ role: "user", content: "fix the bug" }],
      total: 1,
    });
    const slice = getSessionSlice(state, "a");
    expect(slice.messages).toEqual([
      { role: "user", content: "fix the bug" },
      { role: "assistant", content: "working on it" },
    ]);
  });

  it("MERGE_SNAPSHOT applies fully once the turn ends (recovery after missed turn_done)", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "a",
      message: { role: "user", content: "fix the bug" },
    });
    state = chatReducer(state, {
      type: "LIVE_DELTA",
      sessionId: "a",
      kind: "text",
      delta: "partial",
    });
    state = chatReducer(state, { type: "SET_TURN_STATE", sessionId: "a", turnActive: true });
    // reconcileOpenSessions dispatches SET_TURN_STATE from the authoritative
    // server snapshot BEFORE the merge — a finished turn flips turnActive
    // first, so the recovery merge below must apply (not be blocked).
    state = chatReducer(state, { type: "SET_TURN_STATE", sessionId: "a", turnActive: false });
    state = chatReducer(state, {
      type: "MERGE_SNAPSHOT",
      sessionId: "a",
      messages: [
        { role: "user", content: "fix the bug" },
        { role: "assistant", content: "done" },
      ],
      total: 2,
    });
    const slice = getSessionSlice(state, "a");
    expect(slice.messages).toEqual([
      { role: "user", content: "fix the bug" },
      { role: "assistant", content: "done" },
    ]);
    expect(slice.live).toEqual([]);
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

  it("SET_SESSION_MODEL is per-session, never touches the global model", () => {
    let state = initial();
    state = chatReducer(state, { type: "SET_MODEL", model: "openai/global" });
    state = chatReducer(state, { type: "SET_SESSION_MODEL", sessionId: "a", model: "anthropic/pick" });
    expect(getSessionSlice(state, "a").model).toBe("anthropic/pick");
    // Other tabs keep no pick (undefined → fall back to the global default),
    // and the global model is untouched.
    expect(getSessionSlice(state, "b").model).toBeUndefined();
    expect(state.model).toBe("openai/global");
    // Clearing the draft pick returns the slice to "no pick".
    state = chatReducer(state, { type: "SET_SESSION_MODEL", sessionId: "a", model: undefined });
    expect(getSessionSlice(state, "a").model).toBeUndefined();
  });

  it("REKEY_SESSION carries a draft tab's model pick to the real session id", () => {
    let state = initial();
    state = chatReducer(state, { type: "SET_SESSION_MODEL", sessionId: "new-9", model: "anthropic/pick" });
    state = chatReducer(state, { type: "REKEY_SESSION", oldId: "new-9", newId: "ses_real" });
    expect(getSessionSlice(state, "ses_real").model).toBe("anthropic/pick");
    expect(getSessionSlice(state, "new-9").model).toBeUndefined();
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

describe("live tool output streaming", () => {
  it("LIVE_TOOL_OUTPUT appends chunks to the tool part with the matching callId", () => {
    let state = initialState;
    state = chatReducer(state, {
      type: "LIVE_TOOL_START",
      sessionId: "s",
      tool: "bash",
      callId: "call-1",
      command: "npm test",
    });
    state = chatReducer(state, {
      type: "LIVE_TOOL_OUTPUT",
      sessionId: "s",
      callId: "call-1",
      chunk: "running tests\n",
    });
    state = chatReducer(state, {
      type: "LIVE_TOOL_OUTPUT",
      sessionId: "s",
      callId: "call-1",
      chunk: "all passed\n",
    });

    const part = getSessionSlice(state, "s").live[0];
    expect(part).toMatchObject({
      kind: "tool",
      callId: "call-1",
      stream: "running tests\nall passed\n",
    });
    // Still pending: a stream is progress, not the authoritative result.
    expect((part as { output?: string }).output).toBeUndefined();
  });

  it("routes concurrent tool streams to their own bubbles", () => {
    let state = initialState;
    for (const [tool, callId] of [
      ["bash", "call-a"],
      ["read", "call-b"],
    ]) {
      state = chatReducer(state, {
        type: "LIVE_TOOL_START",
        sessionId: "s",
        tool,
        callId,
      });
    }
    state = chatReducer(state, {
      type: "LIVE_TOOL_OUTPUT",
      sessionId: "s",
      callId: "call-b",
      chunk: "file contents",
    });

    const live = getSessionSlice(state, "s").live;
    expect((live[0] as { stream?: string }).stream).toBeUndefined();
    expect((live[1] as { stream?: string }).stream).toBe("file contents");
  });

  it("LIVE_TOOL_RESULT pairs by callId even when results arrive out of order", () => {
    let state = initialState;
    for (const [tool, callId] of [
      ["bash", "call-a"],
      ["read", "call-b"],
    ]) {
      state = chatReducer(state, {
        type: "LIVE_TOOL_START",
        sessionId: "s",
        tool,
        callId,
      });
    }
    // Second call finishes first — the positional heuristic would mis-pair.
    state = chatReducer(state, {
      type: "LIVE_TOOL_RESULT",
      sessionId: "s",
      callId: "call-b",
      output: "b output",
    });
    state = chatReducer(state, {
      type: "LIVE_TOOL_RESULT",
      sessionId: "s",
      callId: "call-a",
      output: "a output",
    });

    const live = getSessionSlice(state, "s").live;
    expect(live[0]).toMatchObject({ callId: "call-a", output: "a output" });
    expect(live[1]).toMatchObject({ callId: "call-b", output: "b output" });
  });

  it("still pairs a result that carries no callId (legacy positional path)", () => {
    let state = initialState;
    state = chatReducer(state, {
      type: "LIVE_TOOL_START",
      sessionId: "s",
      tool: "bash",
    });
    state = chatReducer(state, {
      type: "LIVE_TOOL_RESULT",
      sessionId: "s",
      output: "legacy output",
    });
    expect(getSessionSlice(state, "s").live[0]).toMatchObject({
      output: "legacy output",
    });
  });
});

describe("chatStore permission dialog lifecycle", () => {
  const ask = {
    tool: "bash",
    command: "rm -rf build",
    request_id: "call-1",
    scope: "bash_prefix",
    prefix: "rm",
  };

  function withPendingPermission(): ChatState {
    let state = initial();
    state = chatReducer(state, {
      type: "PERMISSION_REQUEST",
      sessionId: "a",
      permission: ask,
    });
    return state;
  }

  it("PERMISSION_REQUEST sets and PERMISSION_RESOLVED clears the dialog (auto-close)", () => {
    expect(getSessionSlice(withPendingPermission(), "a").pendingPermission).toEqual(ask);

    const state = chatReducer(withPendingPermission(), {
      type: "PERMISSION_RESOLVED",
      sessionId: "a",
      requestId: "call-1",
    });
    expect(getSessionSlice(state, "a").pendingPermission).toBeNull();
  });

  it("a permission_resolved for an older request never closes a newer dialog", () => {
    let state = withPendingPermission();
    state = chatReducer(state, {
      type: "PERMISSION_REQUEST",
      sessionId: "a",
      permission: { ...ask, tool: "delete", request_id: "call-2" },
    });

    state = chatReducer(state, {
      type: "PERMISSION_RESOLVED",
      sessionId: "a",
      requestId: "call-1",
    });
    // The stale dismissal must not close the newer ask.
    expect(getSessionSlice(state, "a").pendingPermission?.request_id).toBe("call-2");

    state = chatReducer(state, {
      type: "PERMISSION_RESOLVED",
      sessionId: "a",
      requestId: "call-2",
    });
    expect(getSessionSlice(state, "a").pendingPermission).toBeNull();
  });

  it("resurfaces a queued ask once the currently-shown one is resolved", () => {
    // Two tool calls in the same round both need approval: call-1 shows
    // first, call-2 supersedes it as the visible dialog, and answering
    // call-2 must bring call-1 back instead of leaving the turn stuck.
    let state = withPendingPermission(); // call-1 shown
    state = chatReducer(state, {
      type: "PERMISSION_REQUEST",
      sessionId: "a",
      permission: { ...ask, tool: "delete", request_id: "call-2" },
    });
    expect(getSessionSlice(state, "a").pendingPermission?.request_id).toBe("call-2");
    expect(getSessionSlice(state, "a").permissionQueue).toHaveLength(1);

    state = chatReducer(state, {
      type: "PERMISSION_RESOLVED",
      sessionId: "a",
      requestId: "call-2",
    });
    expect(getSessionSlice(state, "a").pendingPermission?.request_id).toBe("call-1");
    expect(getSessionSlice(state, "a").permissionQueue).toHaveLength(0);

    state = chatReducer(state, {
      type: "PERMISSION_RESOLVED",
      sessionId: "a",
      requestId: "call-1",
    });
    expect(getSessionSlice(state, "a").pendingPermission).toBeNull();
  });

  it("carries scope/prefix/out_of_scope_path for always-allow availability", () => {
    const slice = getSessionSlice(
      chatReducer(initial(), {
        type: "PERMISSION_REQUEST",
        sessionId: "a",
        permission: { ...ask, out_of_scope_path: "/var/log" },
      }),
      "a",
    );
    expect(slice.pendingPermission?.scope).toBe("bash_prefix");
    expect(slice.pendingPermission?.prefix).toBe("rm");
    expect(slice.pendingPermission?.out_of_scope_path).toBe("/var/log");
  });
});

describe("chatStore reload rehydration from transcript snapshot", () => {
  const makeAskMsg = (id: string, toolName = "bash", command = "rm -rf /tmp/x") => ({
    role: "tool" as const,
    content:
      "PERMISSION_ASK:" +
      JSON.stringify({
        tool_name: toolName,
        command,
        rule: `bash.prefix.${toolName}`,
        scope: "bash_prefix",
        prefix: toolName,
      }),
    tool_call_id: id,
  });
  const makeQuestionMsg = (id: string) => ({
    role: "tool" as const,
    content:
      "QUESTION_PROMPT:" +
      JSON.stringify([{ header: "h", question: "q?", options: [{ label: "a" }] }]) +
      "\n\nWAITING_FOR_USER_RESPONSE",
    tool_call_id: id,
  });

  it("SET_MESSAGES rehydrates a single pending permission after reload", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "SET_MESSAGES",
      sessionId: "a",
      messages: [
        { role: "user", content: "hi" },
        { role: "assistant", content: "", tool_calls: [{ id: "call-1", type: "function", function: { name: "bash", arguments: "{}" } }] },
        makeAskMsg("call-1"),
      ],
    });
    const slice = getSessionSlice(state, "a");
    expect(slice.pendingPermission?.request_id).toBe("call-1");
    expect(slice.pendingPermission?.tool).toBe("bash");
    expect(slice.permissionQueue).toHaveLength(0);
  });

  it("MERGE_SNAPSHOT rehydrates multiple trailing asks with queue ordering", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "MERGE_SNAPSHOT",
      sessionId: "a",
      messages: [
        { role: "user", content: "hi" },
        { role: "assistant", content: "", tool_calls: [{ id: "c1", type: "function", function: { name: "bash", arguments: "{}" } }, { id: "c2", type: "function", function: { name: "delete", arguments: "{}" } }] },
        makeAskMsg("c1", "bash", "rm a"),
        makeAskMsg("c2", "delete", "delete b"),
      ],
      total: 4,
    });
    const slice = getSessionSlice(state, "a");
    expect(slice.pendingPermission?.request_id).toBe("c2");
    expect(slice.permissionQueue).toHaveLength(1);
    expect(slice.permissionQueue[0].request_id).toBe("c1");
    // resolving newest should resurface older
    state = chatReducer(state, { type: "PERMISSION_RESOLVED", sessionId: "a", requestId: "c2" });
    expect(getSessionSlice(state, "a").pendingPermission?.request_id).toBe("c1");
  });

  it("rehydrates a pending question prompt", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "SET_MESSAGES",
      sessionId: "a",
      messages: [
        { role: "user", content: "hi" },
        { role: "assistant", content: "", tool_calls: [{ id: "q1", type: "function", function: { name: "question", arguments: "{}" } }] },
        makeQuestionMsg("q1"),
      ],
    });
    expect(getSessionSlice(state, "a").pendingQuestion?.request_id).toBe("q1");
    expect(getSessionSlice(state, "a").pendingQuestion?.questions[0].question).toBe("q?");
  });

  it("rehydrates both permission and question when both trail", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "SET_MESSAGES",
      sessionId: "a",
      messages: [
        { role: "user", content: "hi" },
        { role: "assistant", content: "", tool_calls: [{ id: "c1", type: "function", function: { name: "bash", arguments: "{}" } }, { id: "q1", type: "function", function: { name: "question", arguments: "{}" } }] },
        makeAskMsg("c1"),
        makeQuestionMsg("q1"),
      ],
    });
    expect(getSessionSlice(state, "a").pendingPermission?.request_id).toBe("c1");
    expect(getSessionSlice(state, "a").pendingQuestion?.request_id).toBe("q1");
  });

  it("ignores malformed sentinel and non-trailing asks", () => {
    let state = initial();
    // ask not at trailing run (followed by assistant) + malformed second ask
    state = chatReducer(state, {
      type: "SET_MESSAGES",
      sessionId: "a",
      messages: [
        { role: "user", content: "hi" },
        makeAskMsg("old"),
        { role: "assistant", content: "done" },
        { role: "tool", content: "PERMISSION_ASK:not-json", tool_call_id: "bad" },
      ],
    });
    expect(getSessionSlice(state, "a").pendingPermission).toBeNull();
    expect(getSessionSlice(state, "a").pendingQuestion).toBeNull();
  });

  it("clears stale pending when snapshot has no sentinel", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "PERMISSION_REQUEST",
      sessionId: "a",
      permission: { tool: "bash", command: "rm", request_id: "c1", scope: "bash_prefix", prefix: "rm" },
    });
    expect(getSessionSlice(state, "a").pendingPermission?.request_id).toBe("c1");
    // server resolved it — new snapshot without sentinel should clear
    state = chatReducer(state, {
      type: "SET_MESSAGES",
      sessionId: "a",
      messages: [
        { role: "user", content: "hi" },
        { role: "assistant", content: "done" },
      ],
    });
    expect(getSessionSlice(state, "a").pendingPermission).toBeNull();
    expect(getSessionSlice(state, "a").permissionQueue).toHaveLength(0);
  });

  it("MERGE_SNAPSHOT mid-turn guard preserves live and does not clobber pending", () => {
    let state = initial();
    state = chatReducer(state, { type: "SET_TURN_STATE", sessionId: "a", turnActive: true });
    state = chatReducer(state, {
      type: "PERMISSION_REQUEST",
      sessionId: "a",
      permission: { tool: "bash", command: "rm", request_id: "live-c1", scope: "bash_prefix", prefix: "rm" },
    });
    // give slice some committed messages so guard triggers
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "a",
      message: { role: "user", content: "hi" },
    });
    const before = getSessionSlice(state, "a").pendingPermission?.request_id;
    state = chatReducer(state, {
      type: "MERGE_SNAPSHOT",
      sessionId: "a",
      messages: [{ role: "user", content: "old disk" }],
      total: 1,
    });
    expect(getSessionSlice(state, "a").pendingPermission?.request_id).toBe(before);
  });

  // Regression: an already-initialized slice (messages present, turn armed by
  // applyReconcileState) whose live question event was missed during a
  // disconnect must still surface the question dialog from a later transcript
  // snapshot. The reconcile path dispatches SET_TURN_STATE(true) BEFORE
  // MERGE_SNAPSHOT, so the mid-turn guard tripped and never re-derived
  // pendingQuestion — the sentinel was the only recovery source.
  it("MERGE_SNAPSHOT surfaces a pending question for an already-initialized slice that missed the live event", () => {
    let state = initial();
    state = chatReducer(state, { type: "SET_TURN_STATE", sessionId: "a", turnActive: true });
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "a",
      message: { role: "user", content: "hi" },
    });
    expect(getSessionSlice(state, "a").pendingQuestion).toBeNull();
    state = chatReducer(state, {
      type: "MERGE_SNAPSHOT",
      sessionId: "a",
      messages: [
        { role: "user", content: "hi" },
        { role: "assistant", content: "", tool_calls: [{ id: "q1", type: "function", function: { name: "question", arguments: "{}" } }] },
        makeQuestionMsg("q1"),
      ],
      total: 3,
    });
    expect(getSessionSlice(state, "a").pendingQuestion?.request_id).toBe("q1");
    expect(getSessionSlice(state, "a").pendingQuestion?.questions[0].question).toBe("q?");
  });
});
