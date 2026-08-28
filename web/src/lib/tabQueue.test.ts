import { describe, expect, it } from "vitest";
import {
  getQueue,
  pushQueued,
  shiftQueued,
  shiftUndispatched,
  removeQueuedItem,
  popLastQueued,
  unshiftQueued,
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

  it("popLastQueued restores the last item regardless of kind (LIFO)", () => {
    pushQueued("t2", cmd("/btw aside"));
    pushQueued("t2", msg("follow-up"));
    // Newest item (the message) is restored and removed.
    expect(popLastQueued("t2")).toEqual(msg("follow-up"));
    expect(getQueue("t2")).toEqual([cmd("/btw aside")]);
    // The remaining command is also restorable — mirrors the TUI's up-arrow
    // restore, which recalls the last queued item even when it is a command.
    expect(popLastQueued("t2")).toEqual(cmd("/btw aside"));
    expect(getQueue("t2")).toEqual([]);
    clearQueue("t2");
  });

  it("popLastQueued restores a queued command when it is the last item", () => {
    pushQueued("t2b", msg("first"));
    pushQueued("t2b", cmd("/sidebar"));
    // The trailing command (last queued) is restored, not the earlier message.
    expect(popLastQueued("t2b")).toEqual(cmd("/sidebar"));
    expect(getQueue("t2b")).toEqual([msg("first")]);
    clearQueue("t2b");
  });

  it("popLastQueued restores a command-only queue", () => {
    pushQueued("t3", cmd("/btw c1"));
    pushQueued("t3", cmd("/btw c2"));
    expect(popLastQueued("t3")).toEqual(cmd("/btw c2"));
    expect(popLastQueued("t3")).toEqual(cmd("/btw c1"));
    expect(getQueue("t3")).toEqual([]);
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

  it("unshiftQueued restores a failed item ahead of newer work", () => {
    pushQueued("t4b", msg("newer"));
    unshiftQueued("t4b", msg("failed"));
    expect(shiftQueued("t4b")).toEqual(msg("failed"));
    expect(shiftQueued("t4b")).toEqual(msg("newer"));
    clearQueue("t4b");
  });

  it("shiftUndispatched skips dispatched items and returns the next fresh one (FIFO)", () => {
    pushQueued("t5", { kind: "message", text: "injected", dispatched: true });
    pushQueued("t5", msg("fresh"));
    pushQueued("t5", { kind: "message", text: "injected2", dispatched: true });
    // The dispatched head is dropped; the fresh message is returned.
    expect(shiftUndispatched("t5")).toEqual(msg("fresh"));
    // Only the trailing dispatched item remains — it is discarded, not returned.
    expect(shiftUndispatched("t5")).toBeUndefined();
    expect(getQueue("t5")).toEqual([]);
    clearQueue("t5");
  });

  it("shiftUndispatched discards a queue that is entirely dispatched", () => {
    pushQueued("t6", { kind: "message", text: "a", dispatched: true });
    pushQueued("t6", { kind: "message", text: "b", dispatched: true });
    expect(shiftUndispatched("t6")).toBeUndefined();
    expect(getQueue("t6")).toEqual([]);
    clearQueue("t6");
  });

  it("removeQueuedItem removes by reference and leaves the rest intact", () => {
    const a = msg("a");
    const b = msg("b");
    const c = { kind: "message" as const, text: "c", dispatched: true };
    pushQueued("t7", a);
    pushQueued("t7", b);
    pushQueued("t7", c);
    // A rejected submit should drop only the phantom dispatched entry.
    removeQueuedItem("t7", c);
    expect(getQueue("t7")).toEqual([a, b]);
    // Removing a non-existent reference is a no-op.
    removeQueuedItem("t7", { kind: "message", text: "nope" });
    expect(getQueue("t7")).toEqual([a, b]);
    clearQueue("t7");
  });

  it("popLastQueued returns the last item even when dispatched", () => {
    pushQueued("t8", msg("first"));
    pushQueued("t8", { kind: "message", text: "live", dispatched: true });
    // Recall walks to the newest item regardless of its dispatched flag, so a
    // message typed during streaming is still restorable via up-arrow.
    expect(popLastQueued("t8")).toEqual({ kind: "message", text: "live", dispatched: true });
    expect(getQueue("t8")).toEqual([msg("first")]);
    clearQueue("t8");
  });
});
