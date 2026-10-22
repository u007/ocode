import { describe, expect, it } from "vitest";
import {
  getQueue,
  pushQueued,
  shiftQueued,
  shiftUndispatched,
  removeQueuedItem,
  removeDispatchedQueuedByText,
  popLastQueued,
  unshiftQueued,
  rekeyQueue,
  restoreQueuedFrom,
  clearQueue,
  QUEUE_CHANGED_EVENT,
  dispatchQueueChanged,
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

  it("removeDispatchedQueuedByText drops only the dispatched entry matching text", () => {
    pushQueued("t9", { kind: "message", text: "live", dispatched: true });
    pushQueued("t9", msg("pending"));
    pushQueued("t9", { kind: "message", text: "live", dispatched: true });
    // First match wins (FIFO), leaving the fresh message and the duplicate.
    expect(removeDispatchedQueuedByText("t9", "live")).toBe(true);
    expect(getQueue("t9")).toEqual([msg("pending"), { kind: "message", text: "live", dispatched: true }]);
    // Undispatched entries are never matches — a queued-but-not-yet-injected
    // message must keep draining normally.
    expect(removeDispatchedQueuedByText("t9", "pending")).toBe(false);
    expect(getQueue("t9")).toEqual([msg("pending"), { kind: "message", text: "live", dispatched: true }]);
    // Second call removes the remaining duplicate.
    expect(removeDispatchedQueuedByText("t9", "live")).toBe(true);
    expect(getQueue("t9")).toEqual([msg("pending")]);
    clearQueue("t9");
  });

  it("removeDispatchedQueuedByText ignores commands and unknown tabs/text", () => {
    pushQueued("t10", cmd("/status"));
    // A command's text can never collide: only dispatched message entries match.
    expect(removeDispatchedQueuedByText("t10", "/status")).toBe(false);
    expect(getQueue("t10")).toEqual([cmd("/status")]);
    expect(removeDispatchedQueuedByText("missing", "x")).toBe(false);
    expect(removeDispatchedQueuedByText(null, "x")).toBe(false);
    clearQueue("t10");
  });

  it("dispatchQueueChanged fires a tab-scoped window event", () => {
    const seen: unknown[] = [];
    const handler = (e: Event) => seen.push((e as CustomEvent).detail);
    window.addEventListener(QUEUE_CHANGED_EVENT, handler);
    try {
      dispatchQueueChanged("t11");
      dispatchQueueChanged(null);
      expect(seen).toEqual([{ tabId: "t11" }]);
    } finally {
      window.removeEventListener(QUEUE_CHANGED_EVENT, handler);
    }
  });

  // --- restoreQueuedFrom (click-to-restore in the composer queue strip) ---

  it("restores the first queued item and everything after it", () => {
    const first = msg("first");
    pushQueued("r1", first);
    pushQueued("r1", msg("second"));
    pushQueued("r1", msg("third"));
    expect(restoreQueuedFrom("r1", first, "")).toEqual({
      value: "first\nsecond\nthird",
      draftStart: "first\nsecond\nthird".length,
    });
    expect(getQueue("r1")).toEqual([]);
    clearQueue("r1");
  });

  it("restores from a middle item and leaves the earlier items queued", () => {
    const first = msg("first");
    const second = msg("second");
    pushQueued("r2", first);
    pushQueued("r2", second);
    pushQueued("r2", msg("third"));
    // A non-empty draft stays a suffix so nothing the user typed is clobbered.
    const merged = restoreQueuedFrom("r2", second, "my draft");
    expect(merged?.value).toBe("second\nthird\nmy draft");
    expect(merged?.draftStart).toBe("second\nthird".length + 1);
    expect(getQueue("r2")).toEqual([first]);
    clearQueue("r2");
  });

  it("restores only the last item when it is clicked", () => {
    const first = msg("first");
    const last = msg("last");
    pushQueued("r3", first);
    pushQueued("r3", last);
    expect(restoreQueuedFrom("r3", last, "")?.value).toBe("last");
    expect(getQueue("r3")).toEqual([first]);
    clearQueue("r3");
  });

  it("leaves commands queued instead of inlining them into prose", () => {
    const first = msg("before");
    const compact = cmd("/compact");
    const last = msg("after");
    pushQueued("r4", first);
    pushQueued("r4", compact);
    pushQueued("r4", last);
    // Restoring from the first message pulls back the messages but keeps the
    // command queued so it still auto-drains as a command.
    expect(restoreQueuedFrom("r4", first, "")?.value).toBe("before\nafter");
    expect(getQueue("r4")).toEqual([compact]);
    clearQueue("r4");
  });

  it("does nothing when the clicked suffix has no messages", () => {
    const compact = cmd("/compact");
    pushQueued("r5", compact);
    expect(restoreQueuedFrom("r5", compact, "draft")).toBeNull();
    expect(getQueue("r5")).toEqual([compact]);
    clearQueue("r5");
  });

  it("is a no-op for an item that is no longer queued (stale click)", () => {
    const stale = msg("already drained");
    const live = msg("still queued");
    pushQueued("r6", live);
    expect(restoreQueuedFrom("r6", stale, "")).toBeNull();
    expect(getQueue("r6")).toEqual([live]);
    clearQueue("r6");
  });

  it("restores dispatched messages, matching the up-arrow recall", () => {
    const live = { kind: "message" as const, text: "in flight", dispatched: true };
    pushQueued("r7", live);
    expect(restoreQueuedFrom("r7", live, "")?.value).toBe("in flight");
    expect(getQueue("r7")).toEqual([]);
    clearQueue("r7");
  });
});
