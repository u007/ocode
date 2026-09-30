import { beforeEach, describe, expect, it } from "vitest";
import {
  clearQueue,
  drainQueuedMessagesIntoDraft,
  getQueue,
  mergeQueuedIntoDraft,
  pushQueued,
} from "./tabQueue";

const TAB = "restore-tab";

describe("mergeQueuedIntoDraft", () => {
  it("leaves an empty draft as just the queued block", () => {
    const merged = mergeQueuedIntoDraft(["one", "two"], "");
    expect(merged.value).toBe("one\ntwo");
    expect(merged.draftStart).toBe(merged.value.length);
  });

  it("keeps the user's draft as the suffix", () => {
    const merged = mergeQueuedIntoDraft(["queued"], "typing away");
    expect(merged.value).toBe("queued\ntyping away");
    // The draft begins one past the queued block plus its separator, so a
    // composer can shift the caret by exactly this much and stay anchored.
    expect(merged.value.slice(merged.draftStart)).toBe("typing away");
  });

  it("returns the draft untouched when there is nothing queued", () => {
    expect(mergeQueuedIntoDraft([], "typing away")).toEqual({ value: "typing away", draftStart: 0 });
    expect(mergeQueuedIntoDraft(["   "], "typing away")).toEqual({ value: "typing away", draftStart: 0 });
  });

  it("preserves submission order and drops blank entries", () => {
    const merged = mergeQueuedIntoDraft(["a", "  ", "b", "c"], "z");
    expect(merged.value).toBe("a\nb\nc\nz");
    expect(merged.value.slice(merged.draftStart)).toBe("z");
  });

  it("trims queued text so restored prose has no stray padding", () => {
    expect(mergeQueuedIntoDraft(["  padded  "], "").value).toBe("padded");
  });

  it("reports the offset in UTF-16 units so it maps onto textarea offsets", () => {
    // Textarea selectionStart/End are UTF-16 code-unit offsets, so the shift
    // must be measured the same way — a rune count would drift on astral chars.
    const merged = mergeQueuedIntoDraft(["🎉"], "ok");
    expect(merged.value).toBe("🎉\nok");
    expect(merged.value.slice(merged.draftStart)).toBe("ok");
  });
});

describe("drainQueuedMessagesIntoDraft", () => {
  beforeEach(() => clearQueue(TAB));

  it("returns null when the queue holds nothing", () => {
    expect(drainQueuedMessagesIntoDraft(TAB, "draft")).toBeNull();
  });

  it("moves messages out of the queue and merges them into the draft", () => {
    pushQueued(TAB, { kind: "message", text: "first queued" });
    pushQueued(TAB, { kind: "message", text: "second queued" });

    const merged = drainQueuedMessagesIntoDraft(TAB, "my draft");
    expect(merged?.value).toBe("first queued\nsecond queued\nmy draft");
    expect(getQueue(TAB)).toEqual([]);
  });

  it("leaves commands queued and dispatchable", () => {
    pushQueued(TAB, { kind: "command", text: "/compact" });
    pushQueued(TAB, { kind: "message", text: "queued prose" });

    const merged = drainQueuedMessagesIntoDraft(TAB, "");
    expect(merged?.value).toBe("queued prose");
    expect(getQueue(TAB)).toEqual([{ kind: "command", text: "/compact" }]);
  });

  it("returns null and changes nothing when only commands are queued", () => {
    pushQueued(TAB, { kind: "command", text: "/compact" });
    expect(drainQueuedMessagesIntoDraft(TAB, "draft")).toBeNull();
    expect(getQueue(TAB)).toHaveLength(1);
  });

  it("clears a dispatched flag with the message it belonged to", () => {
    pushQueued(TAB, { kind: "message", text: "sent while streaming", dispatched: true });
    drainQueuedMessagesIntoDraft(TAB, "");
    // A dispatched entry left behind would be skipped by the drain backstop and
    // shown as a phantom queue row.
    expect(getQueue(TAB)).toEqual([]);
  });
});
