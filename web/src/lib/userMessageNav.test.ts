import { describe, it, expect } from "vitest";
import {
  NO_USER_JUMP,
  nextUserJumpCursor,
  userJumpLabel,
  isCountableUserMessage,
  loadedUserMessageIndices,
} from "./userMessageNav";

describe("nextUserJumpCursor", () => {
  it("seeds at the newest message on the first press in either direction", () => {
    // The user sits at the bottom, so the newest message is the nearest entry
    // point. Both keys must land there — a first alt+down that jumped to the
    // OLDEST would be a backwards teleport, and one that did nothing would be
    // a dead key.
    expect(nextUserJumpCursor(NO_USER_JUMP, 5, -1)).toBe(4);
    expect(nextUserJumpCursor(NO_USER_JUMP, 5, 1)).toBe(4);
  });

  it("seeds to 0 when there is exactly one message", () => {
    expect(nextUserJumpCursor(NO_USER_JUMP, 1, -1)).toBe(0);
    expect(nextUserJumpCursor(NO_USER_JUMP, 1, 1)).toBe(0);
  });

  it("walks backwards with dir -1", () => {
    expect(nextUserJumpCursor(4, 5, -1)).toBe(3);
    expect(nextUserJumpCursor(3, 5, -1)).toBe(2);
  });

  it("walks forwards with dir 1", () => {
    expect(nextUserJumpCursor(2, 5, 1)).toBe(3);
    expect(nextUserJumpCursor(3, 5, 1)).toBe(4);
  });

  it("clamps at the oldest instead of wrapping", () => {
    expect(nextUserJumpCursor(0, 5, -1)).toBe(0);
    // Wrapping would have produced 4 (the newest), which is the exact
    // disorientation the clamp exists to prevent.
    expect(nextUserJumpCursor(0, 5, -1)).not.toBe(4);
  });

  it("clamps at the newest instead of wrapping", () => {
    expect(nextUserJumpCursor(4, 5, 1)).toBe(4);
  });

  it("returns the sentinel when there are no messages", () => {
    expect(nextUserJumpCursor(NO_USER_JUMP, 0, -1)).toBe(NO_USER_JUMP);
    expect(nextUserJumpCursor(2, 0, 1)).toBe(NO_USER_JUMP);
  });

  it("walks a whole conversation without skipping or repeating", () => {
    const total = 4;
    let cursor = NO_USER_JUMP;
    const seen: number[] = [];
    // Walk up from the bottom, collecting every position visited.
    for (let i = 0; i < total; i++) {
      cursor = nextUserJumpCursor(cursor, total, -1);
      seen.push(cursor);
    }
    expect(seen).toEqual([3, 2, 1, 0]);
  });
});

describe("userJumpLabel", () => {
  it("renders a 1-based position over the total", () => {
    expect(userJumpLabel(0, 17)).toBe("msg 1/17");
    expect(userJumpLabel(16, 17)).toBe("msg 17/17");
    expect(userJumpLabel(2, 3)).toBe("msg 3/3");
  });

  it("renders nothing when inactive so the caller can omit the element", () => {
    expect(userJumpLabel(NO_USER_JUMP, 17)).toBe("");
    expect(userJumpLabel(0, 0)).toBe("");
    expect(userJumpLabel(-1, 0)).toBe("");
  });
});

describe("isCountableUserMessage", () => {
  it("accepts a plain user turn", () => {
    expect(isCountableUserMessage({ role: "user", content: "do the thing" })).toBe(true);
  });

  it("rejects assistant, tool, and thinking roles", () => {
    expect(isCountableUserMessage({ role: "assistant", content: "sure" })).toBe(false);
    expect(isCountableUserMessage({ role: "tool", content: "output" })).toBe(false);
  });

  it("rejects slash-command echoes, including after leading whitespace", () => {
    expect(isCountableUserMessage({ role: "user", content: "/theme dark" })).toBe(false);
    expect(isCountableUserMessage({ role: "user", content: "   \n /compact" })).toBe(false);
  });

  it("accepts a prompt that merely mentions a slash mid-string", () => {
    expect(isCountableUserMessage({ role: "user", content: "explain /theme to me" })).toBe(true);
  });

  it("tolerates missing fields rather than throwing", () => {
    expect(isCountableUserMessage({})).toBe(false);
    expect(isCountableUserMessage({ role: "user" })).toBe(true);
  });
});

describe("loadedUserMessageIndices", () => {
  it("returns ascending indices of countable user messages only", () => {
    const msgs = [
      { role: "user", content: "first" },
      { role: "assistant", content: "reply" },
      { role: "tool", content: "out" },
      { role: "user", content: "/theme" },
      { role: "user", content: "second" },
    ];
    expect(loadedUserMessageIndices(msgs)).toEqual([0, 4]);
  });

  it("returns an empty list for a transcript with no user turns", () => {
    expect(loadedUserMessageIndices([{ role: "assistant", content: "hi" }])).toEqual([]);
    expect(loadedUserMessageIndices([])).toEqual([]);
  });
});
