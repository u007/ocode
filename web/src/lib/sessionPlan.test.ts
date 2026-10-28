import { describe, expect, it } from "vitest";
import { getSessionPlan, setSessionPlan } from "./sessionPlan";
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
