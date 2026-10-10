import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { routeBusEnvelope, type SessionEventRouter } from "./sessionEvents";
import { chatReducer, initialState, type ChatAction, type ChatState } from "../stores/chatStore";
import {
  __resetSessionActivityForTests,
  getSessionActivity,
  setSkillActivity,
} from "./commandActivity";
import type { BusEnvelope } from "./eventBus";

const mockGetSessionState = vi.fn();
vi.mock("../api/client", () => ({
  api: {
    getSessionState: (...a: unknown[]) => mockGetSessionState(...a),
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
    getState: () => state,
    projectDispatch: () => {},
    onNewTab: () => {},
    hostFor: () => undefined,
  };
  return { router, actions, getState: () => state };
}

beforeEach(() => {
  __resetSessionActivityForTests();
});

afterEach(() => {
  __resetSessionActivityForTests();
  vi.clearAllMocks();
});

describe("skill activity from tool_start", () => {
  // The gap this closes: a `skill` call returns instantly (it only reads
  // SKILL.md), so a bar tied to the tool's pending state would vanish before
  // the skill did any work. It is recorded for the rest of the turn instead.
  it("records the skill name from a skill tool call", () => {
    const { router } = makeRouter();
    routeBusEnvelope(
      env("tool_start", { data: { tool: "skill", call_id: "c1", command: '{"name":"git-commit-push"}' } }),
      router,
    );

    const activity = getSessionActivity("s1");
    expect(activity?.kind).toBe("skill");
    if (activity?.kind === "skill") expect(activity.name).toBe("git-commit-push");
  });

  // load_skill is the registered alias some models emit (internal/tool/misc.go).
  it("records the skill from the load_skill alias too", () => {
    const { router } = makeRouter();
    routeBusEnvelope(
      env("tool_start", { data: { tool: "load_skill", call_id: "c1", command: '{"name":"brainstorming"}' } }),
      router,
    );

    const activity = getSessionActivity("s1");
    if (activity?.kind === "skill") expect(activity.name).toBe("brainstorming");
  });

  it("leaves other tools alone", () => {
    const { router } = makeRouter();
    routeBusEnvelope(env("tool_start", { data: { tool: "bash", call_id: "c1", command: "ls" } }), router);
    expect(getSessionActivity("s1")).toBeUndefined();
  });

  it("ignores a skill call whose args carry no usable name", () => {
    const { router } = makeRouter();
    routeBusEnvelope(
      env("tool_start", { data: { tool: "skill", call_id: "c1", command: '{"name":42}' } }),
      router,
    );
    expect(getSessionActivity("s1")).toBeUndefined();
  });

  it("keeps the skill bar while the skill's own tool calls run", () => {
    const { router } = makeRouter();
    routeBusEnvelope(
      env("tool_start", { data: { tool: "skill", call_id: "c1", command: '{"name":"git-commit-push"}' } }),
      router,
    );
    // The bash call that actually commits and pushes.
    routeBusEnvelope(
      env("tool_start", { data: { tool: "bash", call_id: "c2", command: "git push" } }),
      router,
    );

    const activity = getSessionActivity("s1");
    if (activity?.kind === "skill") expect(activity.name).toBe("git-commit-push");
  });

  it("does not touch another session's bar", () => {
    const { router } = makeRouter(["s1", "s2"]);
    setSkillActivity("s2", "other-skill");
    routeBusEnvelope(
      env("tool_start", { session_id: "s1", data: { tool: "skill", call_id: "c1", command: '{"name":"a"}' } }),
      router,
    );
    expect(getSessionActivity("s2")?.kind).toBe("skill");
  });
});

describe("skill activity lifecycle", () => {
  it("clears on turn_done", () => {
    const { router } = makeRouter();
    setSkillActivity("s1", "git-commit-push");
    routeBusEnvelope(env("turn_done"), router);
    expect(getSessionActivity("s1")).toBeUndefined();
  });

  it("clears on turn_error", () => {
    const { router } = makeRouter();
    setSkillActivity("s1", "git-commit-push");
    routeBusEnvelope(env("turn_error", { data: { error: "boom" } }), router);
    expect(getSessionActivity("s1")).toBeUndefined();
  });

  // A stale bar left by a finished turn must not resurface on the next one.
  it("clears when the next turn starts", () => {
    const { router } = makeRouter();
    setSkillActivity("s1", "git-commit-push");
    routeBusEnvelope(env("turn_started", { data: { model: "test/model" } }), router);
    expect(getSessionActivity("s1")).toBeUndefined();
  });

  it("leaves the other session's bar alone on turn_done", () => {
    const { router } = makeRouter(["s1", "s2"]);
    setSkillActivity("s2", "keep-me");
    routeBusEnvelope(env("turn_done", { session_id: "s1" }), router);
    expect(getSessionActivity("s2")).toBeDefined();
  });
});