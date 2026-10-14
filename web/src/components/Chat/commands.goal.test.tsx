import { describe, expect, it, vi } from "vitest";
import { ApiError } from "../../api/client";
import { dispatchCommand } from "./commands";

/**
 * /goal suite.
 *
 * `/goal <condition>` PUTs the session goal and hands the server's kickoff
 * prompt to the normal send path; `/goal` reads the status line; the clear
 * verbs DELETE it. Every call is scoped to the session and its remote host.
 */
function ctx(api: Record<string, unknown> = {}, args = "") {
  const defaults = {
    getSessionGoal: vi.fn(async () => ({
      active: false,
      condition: "",
      status: "",
      turns: 0,
      last_reason: "",
      status_text: "No goal set.",
    })),
    setSessionGoal: vi.fn(async () => ({
      active: true,
      condition: "",
      status: "active",
      turns: 0,
      last_reason: "",
      status_text: "",
      kickoff_prompt: "",
    })),
    clearSessionGoal: vi.fn(async () => undefined),
  };
  return {
    commandName: "/goal",
    args,
    api: { ...defaults, ...api },
    host: "devbox",
    getSessionId: () => "ses_goal",
  } as never;
}

describe("/goal command", () => {
  it("sets goal then sends kickoff prompt", async () => {
    const setSessionGoal = vi.fn(async () => ({
      active: true,
      condition: "all tests pass",
      status: "active",
      turns: 0,
      last_reason: "",
      status_text: "",
      kickoff_prompt: "Work until all tests pass.",
    }));
    const result = await dispatchCommand("/goal all tests pass", ctx({ setSessionGoal }, "all tests pass"));
    expect(setSessionGoal).toHaveBeenCalledWith("ses_goal", "all tests pass", "devbox");
    expect(result.handled).toBe(true);
    expect(result.prompt).toBe("Work until all tests pass.");
  });

  it("status shows status_text", async () => {
    const getSessionGoal = vi.fn(async () => ({
      active: true,
      condition: "all tests pass",
      status: "active",
      turns: 2,
      last_reason: "2 failing",
      status_text: "Goal active: all tests pass (2 turns)",
    }));
    const result = await dispatchCommand("/goal", ctx({ getSessionGoal }));
    expect(getSessionGoal).toHaveBeenCalledWith("ses_goal", "devbox");
    expect(result.messages?.[0]?.content).toBe("Goal active: all tests pass (2 turns)");
  });

  it("clear verbs call DELETE", async () => {
    for (const verb of ["clear", "STOP", "off", "reset", "none", "cancel"]) {
      const clearSessionGoal = vi.fn(async () => undefined);
      const setSessionGoal = vi.fn();
      const result = await dispatchCommand(`/goal ${verb}`, ctx({ clearSessionGoal, setSessionGoal }, verb));
      expect(clearSessionGoal, verb).toHaveBeenCalledWith("ses_goal", "devbox");
      expect(setSessionGoal, verb).not.toHaveBeenCalled();
      expect(result.messages?.[0]?.content).toBe("Goal cleared.");
    }
  });

  it("empty condition shows usage without a request", async () => {
    const setSessionGoal = vi.fn();
    const getSessionGoal = vi.fn();
    const result = await dispatchCommand('/goal ""', ctx({ setSessionGoal, getSessionGoal }, '""'));
    expect(result.handled).toBe(true);
    expect(result.messages?.[0]?.content).toContain("Usage: `/goal <condition>`");
    expect(setSessionGoal).not.toHaveBeenCalled();
    expect(getSessionGoal).not.toHaveBeenCalled();
  });

  it("overlong condition surfaces the 400 message", async () => {
    const setSessionGoal = vi.fn(async () => {
      throw new ApiError("goal condition is too long (max 4000 characters)", 400);
    });
    const result = await dispatchCommand("/goal x", ctx({ setSessionGoal }, "x"));
    expect(result.prompt).toBeUndefined();
    expect(result.messages?.[0]?.content).toContain("goal condition is too long (max 4000 characters)");
  });
});
