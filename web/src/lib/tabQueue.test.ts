import { describe, expect, it } from "vitest";
import {
  getQueue,
  pushQueued,
  shiftQueued,
  popLastQueued,
  rekeyQueue,
  clearQueue,
} from "./tabQueue";

const msg = (text: string) => ({ kind: "message" as const, text });
const cmd = (text: string) => ({ kind: "command" as const, text });

describe("tabQueue", () => {
  it("pushes and shifts FIFO", () => {
    pushQueued("t1", msg("a"));
    pushQueued("t1", msg("b"));
    expect(shiftQueued("t1")).toEqual(msg("a"));
    expect(shiftQueued("t1")).toEqual(msg("b"));
    expect(shiftQueued("t1")).toBeUndefined();
    clearQueue("t1");
  });

  it("popLastQueued restores only message items, leaving commands queued", () => {
    pushQueued("t2", cmd("/btw aside"));
    pushQueued("t2", msg("follow-up"));
    // Newest message is restored; the command stays for execution.
    expect(popLastQueued("t2")).toEqual(msg("follow-up"));
    expect(getQueue("t2")).toEqual([cmd("/btw aside")]);
    // Only a command remains — nothing is restorable.
    expect(popLastQueued("t2")).toBeUndefined();
    expect(getQueue("t2")).toEqual([cmd("/btw aside")]);
    clearQueue("t2");
  });

  it("popLastQueued walks backwards past trailing commands to the last message", () => {
    pushQueued("t3", msg("first"));
    pushQueued("t3", cmd("/btw c1"));
    pushQueued("t3", cmd("/btw c2"));
    expect(popLastQueued("t3")).toEqual(msg("first"));
    expect(getQueue("t3")).toEqual([cmd("/btw c1"), cmd("/btw c2")]);
    clearQueue("t3");
  });

  it("rekeyQueue moves a queue to a new tab id preserving order", () => {
    pushQueued("new-abc", msg("queued while first turn streams"));
    rekeyQueue("new-abc", "ses_123");
    expect(getQueue("new-abc")).toEqual([]);
    expect(shiftQueued("ses_123")).toEqual(msg("queued while first turn streams"));
    clearQueue("ses_123");
  });

  it("rekeyQueue appends to an existing queue and tolerates no-ops", () => {
    pushQueued("a", msg("old"));
    pushQueued("b", msg("new"));
    rekeyQueue("a", "b");
    // The destination's pre-existing items stay first; moved items append after.
    expect(shiftQueued("b")).toEqual(msg("new"));
    expect(shiftQueued("b")).toEqual(msg("old"));
    // Same-id rekey is a no-op; unknown source is a no-op.
    rekeyQueue("b", "b");
    rekeyQueue("missing", "b");
    expect(getQueue("b")).toEqual([]);
    clearQueue("a");
    clearQueue("b");
  });

  it("clearQueue drops a tab's queue", () => {
    pushQueued("t4", msg("x"));
    clearQueue("t4");
    expect(getQueue("t4")).toEqual([]);
  });
});
