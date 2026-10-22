import { beforeEach, describe, expect, it, vi } from "vitest";
import { routeBusEnvelope, type SessionEventRouter } from "./sessionEvents";
import type { BusEnvelope } from "./eventBus";
import type { ChatAction, ChatState } from "../stores/chatStore";
import { chatReducer, getSessionSlice, initialState } from "../stores/chatStore";

const mockGetSessionState = vi.fn();
const mockGetSession = vi.fn();
const mockCloseSession = vi.fn();
vi.mock("../api/client", () => ({
  api: {
    getSessionState: (...a: unknown[]) => mockGetSessionState(...a),
    getSession: (...a: unknown[]) => mockGetSession(...a),
    closeSession: (...a: unknown[]) => mockCloseSession(...a),
  },
  apiPath: (p: string) => p,
}));

function env(event: string, over: Partial<BusEnvelope> = {}): BusEnvelope {
  return { event, project: "/proj", session_id: "s1", seq: 1, data: {}, ...over };
}

function makeRouter(openIds: string[] = ["s1"]) {
  const actions: ChatAction[] = [];
  let state: ChatState = initialState;
  const router: SessionEventRouter = {
    openSessionIds: new Set(openIds),
    dispatch: (a) => {
      actions.push(a);
      state = chatReducer(state, a);
    },
    projectDispatch: () => {},
    getState: () => state,
  };
  return { router, actions, getState: () => state };
}

describe("suggestions bus event", () => {
  beforeEach(() => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
  });

  it("stores the proposed options on the session slice", () => {
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(
      env("suggestions", { data: { suggestions: ["Run the tests", "Open a PR"] } }),
      router,
    );
    expect(getSessionSlice(getState(), "s1").suggestions).toEqual([
      "Run the tests",
      "Open a PR",
    ]);
  });

  it("ignores a malformed payload instead of storing junk", () => {
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(env("suggestions", { data: { suggestions: "not an array" } }), router);
    expect(getSessionSlice(getState(), "s1").suggestions).toEqual([]);

    routeBusEnvelope(env("suggestions", { data: { suggestions: [1, 2, 3] } }), router);
    expect(getSessionSlice(getState(), "s1").suggestions).toEqual([]);
  });

  it("ignores an empty list so the shown row is not cleared by a stray frame", () => {
    const { router, getState } = makeRouter(["s1"]);
    routeBusEnvelope(env("suggestions", { data: { suggestions: ["keep me"] } }), router);
    routeBusEnvelope(env("suggestions", { data: { suggestions: [] } }), router);
    expect(getSessionSlice(getState(), "s1").suggestions).toEqual(["keep me"]);
  });

  it("does not route the event to a session this client does not track", () => {
    const { router, getState } = makeRouter(["other"]);
    routeBusEnvelope(
      env("suggestions", { data: { suggestions: ["nope"] } }),
      router,
    );
    expect(getSessionSlice(getState(), "s1").suggestions).toEqual([]);
  });
});
