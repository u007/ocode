import { describe, expect, it } from "vitest";
import {
  chatReducer,
  getSessionSlice,
  initialState,
  type ChatAction,
} from "./chatStore";

const setLastDispatchedModel = (sessionId: string, model: string): ChatAction =>
  ({ type: "SET_LAST_DISPATCHED_MODEL", sessionId, model }) as ChatAction;

describe("per-session last dispatched model", () => {
  it("stores the model for only the targeted session", () => {
    let state = chatReducer(initialState, setLastDispatchedModel("a", "openai/gpt-4o"));
    state = chatReducer(state, setLastDispatchedModel("b", "anthropic/claude"));

    expect(getSessionSlice(state, "a").lastDispatchedModel).toBe("openai/gpt-4o");
    expect(getSessionSlice(state, "b").lastDispatchedModel).toBe("anthropic/claude");
  });

  it("does not let a status/sidebar model change overwrite the dispatched model", () => {
    let state = chatReducer(initialState, setLastDispatchedModel("s1", "openai/gpt-4o"));
    state = chatReducer(state, {
      type: "SET_TUI_STATUS",
      sessionId: "s1",
      status: { main_model: "anthropic/claude" },
    });

    expect(getSessionSlice(state, "s1").lastDispatchedModel).toBe("openai/gpt-4o");
    expect(getSessionSlice(state, "s1").tuiStatus?.main_model).toBe("anthropic/claude");
  });

  it("preserves the dispatched model when a temporary session is rekeyed", () => {
    let state = chatReducer(initialState, setLastDispatchedModel("new-1", "openai/gpt-4o"));
    state = chatReducer(state, {
      type: "REKEY_SESSION",
      oldId: "new-1",
      newId: "ses-real",
    });

    expect(getSessionSlice(state, "ses-real").lastDispatchedModel).toBe("openai/gpt-4o");
    expect(state.sessions["new-1"]).toBeUndefined();
  });

  it("accepts the response after an SSE-first rekey without recreating the temporary slice", () => {
    let state = chatReducer(initialState, {
      type: "REKEY_SESSION",
      oldId: "new-1",
      newId: "ses-real",
    });
    state = chatReducer(state, setLastDispatchedModel("ses-real", "openai/gpt-4o"));

    expect(getSessionSlice(state, "ses-real").lastDispatchedModel).toBe("openai/gpt-4o");
    expect(state.sessions["new-1"]).toBeUndefined();
  });
});
