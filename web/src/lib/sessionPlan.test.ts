import { describe, expect, it, vi } from "vitest";
import { forgetSessionPlan, getSessionPlan, setSessionPlan } from "./sessionPlan";
import type { TodoUpdatedEvent } from "../api/types";

const plan = (revision: number, current: string): TodoUpdatedEvent => ({
  session_id: "s",
  revision,
  done: 0,
  total: 1,
  current,
  items: [],
});

describe("setSessionPlan ordering", () => {
  it("ignores a stale frame and accepts an equal or newer one", () => {
    setSessionPlan("s", plan(5, "five"));
    setSessionPlan("s", plan(4, "stale"));
    expect(getSessionPlan("s")?.current).toBe("five");
    setSessionPlan("s", plan(5, "same"));
    expect(getSessionPlan("s")?.current).toBe("same");
    setSessionPlan("s", plan(6, "six"));
    expect(getSessionPlan("s")?.current).toBe("six");
  });
});

function richPlan(sessionId: string, revision: number, overrides: Partial<TodoUpdatedEvent> = {}): TodoUpdatedEvent {
  return {
    session_id: sessionId,
    revision,
    done: 0,
    total: 1,
    current: "",
    items: [{ id: "t1", text: "first", state: revision > 0 ? "in_progress" : "pending" }],
    ...overrides,
  };
}

describe("sessionPlan validation and clearing", () => {
  it("rejects a malformed body instead of storing it", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    setSessionPlan("ses-store-bad", {} as TodoUpdatedEvent);
    expect(getSessionPlan("ses-store-bad")).toBeUndefined();
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
  });

  it("rejects a plan addressed to a different session", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    setSessionPlan("ses-store-a", richPlan("ses-store-b", 1));
    expect(getSessionPlan("ses-store-a")).toBeUndefined();
    warn.mockRestore();
  });

  it("lets revision 0 clear a plan the server no longer has", () => {
    setSessionPlan("ses-store-rev", richPlan("ses-store-rev", 3));
    setSessionPlan("ses-store-rev", richPlan("ses-store-rev", 0, { total: 0, items: [] }));
    expect(getSessionPlan("ses-store-rev")).toEqual(expect.objectContaining({ revision: 0, items: [] }));
    forgetSessionPlan("ses-store-rev");
  });

  it("force lets a reconnect adopt a lower revision (a recreated plan file)", () => {
    setSessionPlan("ses-store-force", richPlan("ses-store-force", 5));
    setSessionPlan("ses-store-force", richPlan("ses-store-force", 1), { force: true });
    expect(getSessionPlan("ses-store-force")?.revision).toBe(1);
    forgetSessionPlan("ses-store-force");
  });

  it("forget removes the entry", () => {
    setSessionPlan("ses-store-forget", richPlan("ses-store-forget", 1));
    forgetSessionPlan("ses-store-forget");
    expect(getSessionPlan("ses-store-forget")).toBeUndefined();
  });
});
