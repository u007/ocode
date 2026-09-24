import { describe, expect, it } from "vitest";
import { isChildSessionId } from "./sessionId";

describe("isChildSessionId", () => {
  it("detects subagent / child-context session ids", () => {
    expect(
      isChildSessionId("ses_2026-09-24-133707-d5eda0e8_child_context_2026-09-24-133958"),
    ).toBe(true);
    expect(isChildSessionId("ses_parent_child_general_2026-09-24-120000")).toBe(true);
  });

  it("treats normal session ids as main sessions", () => {
    expect(isChildSessionId("ses_2026-09-24-133707-d5eda0e8")).toBe(false);
    // A bare `_child` without the surrounding infix is not a child id.
    expect(isChildSessionId("ses_2026-09-24-133707-childless")).toBe(false);
    expect(isChildSessionId("")).toBe(false);
  });
});
