import { describe, expect, it, vi } from "vitest";
import { routeBusEnvelope, type SessionEventRouter } from "./sessionEvents";
import { chatReducer, initialState, type ChatAction, type ChatState } from "../stores/chatStore";
import type { BusEnvelope } from "./eventBus";

vi.mock("../api/client", () => ({
  api: {
    closeSession: vi.fn(),
    getSession: vi.fn(),
    getSessionState: vi.fn(),
  },
}));

function envelope(data: Record<string, unknown>): BusEnvelope {
  return {
    event: "turn_started",
    project: "/project",
    session_id: "s1",
    seq: 1,
    data,
  };
}

function routerForTest(): { router: SessionEventRouter; actions: ChatAction[]; getState: () => ChatState } {
  const actions: ChatAction[] = [];
  let state = initialState;
  return {
    router: {
      openSessionIds: new Set(["s1"]),
      dispatch: (action) => {
        actions.push(action);
        state = chatReducer(state, action);
      },
      projectDispatch: vi.fn(),
      getState: () => state,
    },
    actions,
    getState: () => state,
  };
}

describe("turn_started last dispatched model", () => {
  it("routes the backend model to the target session", () => {
    const { router, actions, getState } = routerForTest();
    routeBusEnvelope(envelope({ model: "openai/gpt-4o" }), router);

    expect(actions).toContainEqual({
      type: "SET_LAST_DISPATCHED_MODEL",
      sessionId: "s1",
      model: "openai/gpt-4o",
    });
    expect(getState().sessions.s1.lastDispatchedModel).toBe("openai/gpt-4o");
  });

  it("ignores an empty model", () => {
    const { router, actions } = routerForTest();
    routeBusEnvelope(envelope({ model: "  " }), router);

    expect(actions.some((action) => action.type === "SET_LAST_DISPATCHED_MODEL")).toBe(false);
  });

  it("retains the dispatched model after a turn error", () => {
    const { router, getState } = routerForTest();
    routeBusEnvelope(envelope({ model: "openai/gpt-4o" }), router);
    routeBusEnvelope({ ...envelope({}), event: "turn_error", data: { error: "failed" } }, router);

    expect(getState().sessions.s1.lastDispatchedModel).toBe("openai/gpt-4o");
  });
});
