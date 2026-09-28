import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { routeBusEnvelope, type SessionEventRouter } from "./sessionEvents";
import { pulseEventSink } from "../stores/pulseStore";
import type { BusEnvelope } from "./eventBus";
import type { ChatAction, ChatState } from "../stores/chatStore";
import { chatReducer, initialState } from "../stores/chatStore";

vi.mock("../api/client", () => ({
  api: {
    getSessionState: vi.fn(),
    getSession: vi.fn(),
    closeSession: vi.fn(),
  },
}));

const mockSink = vi.fn();
// The router calls the module-level sink by name, so a spy on the module
// namespace is what proves the forward exists and fires in the right order.
vi.mock("../stores/pulseStore", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../stores/pulseStore")>();
  return { ...actual, pulseEventSink: (...a: unknown[]) => mockSink(...a) };
});

function env(event: string, over: Partial<BusEnvelope> = {}): BusEnvelope {
  return { event, project: "/proj", session_id: "s1", seq: 1, data: {}, ...over };
}

function makeRouter(openIds: string[]) {
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
  return { router, actions };
}

beforeEach(() => {
  mockSink.mockReset();
  vi.spyOn(console, "warn").mockImplementation(() => {});
});
afterEach(() => vi.restoreAllMocks());

describe("routeBusEnvelope → pulse forward", () => {
  it("forwards an event for a session with NO open tab to the pulse sink", () => {
    // The whole point: the dashboard watches sessions the user has not opened.
    const { router, actions } = makeRouter(["other-session"]);
    routeBusEnvelope(
      env("turn_started", { session_id: "ses_unopened", data: { model: "m" } }),
      router,
    );
    expect(mockSink).toHaveBeenCalledWith("turn_started", "ses_unopened", { model: "m" });
    // …and the chat store is still untouched for that session.
    expect(actions.filter((a) => JSON.stringify(a).includes("ses_unopened"))).toHaveLength(0);
  });

  it("forwards todo_updated for an unopened session", () => {
    const { router } = makeRouter([]);
    routeBusEnvelope(
      env("todo_updated", {
        session_id: "ses_unopened",
        data: { session_id: "ses_unopened", done: 1, total: 2, current: "x", items: [] },
      }),
      router,
    );
    expect(mockSink).toHaveBeenCalledWith(
      "todo_updated",
      "ses_unopened",
      expect.objectContaining({ current: "x" }),
    );
  });

  it("still forwards for a tracked session, and the normal routing is unchanged", () => {
    const { router, actions } = makeRouter(["s1"]);
    routeBusEnvelope(env("turn_started", { data: { model: "m" } }), router);
    expect(mockSink).toHaveBeenCalledWith("turn_started", "s1", { model: "m" });
    expect(actions.some((a) => a.type === "SET_TURN_STATE" && a.turnActive)).toBe(true);
  });

  it("forwards permission and question asks for unopened sessions", () => {
    const { router } = makeRouter([]);
    routeBusEnvelope(
      env("permission", { session_id: "ses_x", data: { tool: "bash", command: "ls" } }),
      router,
    );
    routeBusEnvelope(
      env("question", { session_id: "ses_x", data: { questions: [{ question: "?" }] } }),
      router,
    );
    expect(mockSink.mock.calls.map((c) => c[0])).toEqual(["permission", "question"]);
  });

  it("does not forward a non-session-scoped event", () => {
    // git_status/spending/logs carry no session id; forwarding them would make
    // the dashboard treat every unrelated frame as an unknown-session refetch.
    const { router } = makeRouter(["s1"]);
    routeBusEnvelope({ event: "git_status", project: "/proj", seq: 2, data: {} }, router);
    expect(mockSink).not.toHaveBeenCalled();
  });

  it("does not forward an event with no session id", () => {
    const { router } = makeRouter(["s1"]);
    routeBusEnvelope({ event: "turn_started", project: "/proj", seq: 2, data: {} }, router);
    expect(mockSink).not.toHaveBeenCalled();
  });
});

describe("pulseEventSink without a mounted provider", () => {
  it("is a no-op rather than throwing", () => {
    // The real module-level sink is used here (the mock above replaces the
    // import for the router test only). Nothing is mounted, so this must not
    // throw — the SSE router calls it on every session-scoped frame.
    expect(() => pulseEventSink("turn_started", "s1", {})).not.toThrow();
  });
});
