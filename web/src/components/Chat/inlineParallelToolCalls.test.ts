import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  INLINE_PARALLEL_TOOL_CALLS_STORAGE_KEY as KEY,
  loadInlineParallelToolCalls,
  resetInlineParallelToolCallsFallbackForTests,
  saveInlineParallelToolCalls,
  subscribeInlineParallelToolCalls,
} from "./inlineParallelToolCalls";

const blocked = () => {
  throw new DOMException("storage blocked", "SecurityError");
};

describe("inline parallel tool calls preference", () => {
  beforeEach(() => {
    window.localStorage.clear();
    resetInlineParallelToolCallsFallbackForTests();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("round-trips through storage and fires the change event", () => {
    expect(loadInlineParallelToolCalls()).toBe(false);

    const onChange = vi.fn();
    const unsubscribe = subscribeInlineParallelToolCalls(onChange);

    saveInlineParallelToolCalls(true);
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(window.localStorage.getItem(KEY)).toBe("1");
    expect(loadInlineParallelToolCalls()).toBe(true);

    saveInlineParallelToolCalls(false);
    expect(onChange).toHaveBeenCalledTimes(2);
    expect(window.localStorage.getItem(KEY)).toBe("0");
    expect(loadInlineParallelToolCalls()).toBe(false);

    unsubscribe();
  });

  it("keeps the choice for the session when setItem throws", () => {
    const setSpy = vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked);

    saveInlineParallelToolCalls(true);
    expect(setSpy).toHaveBeenCalled();
    expect(window.localStorage.getItem(KEY)).toBeNull();
    expect(loadInlineParallelToolCalls()).toBe(true);

    saveInlineParallelToolCalls(false);
    expect(loadInlineParallelToolCalls()).toBe(false);
  });

  it("clears the fallback once a later write succeeds", () => {
    const setSpy = vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked);
    saveInlineParallelToolCalls(true);
    expect(loadInlineParallelToolCalls()).toBe(true);

    setSpy.mockRestore();
    saveInlineParallelToolCalls(false);
    expect(window.localStorage.getItem(KEY)).toBe("0");

    // Storage is the source of truth again: with the key gone, the read is off,
    // not the stale in-memory "true" from the failed write.
    window.localStorage.removeItem(KEY);
    expect(loadInlineParallelToolCalls()).toBe(false);
  });

  it("lets a value in storage win over the in-memory fallback", () => {
    const setSpy = vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked);
    saveInlineParallelToolCalls(true);
    expect(loadInlineParallelToolCalls()).toBe(true);

    // Another tab writes "0" to storage while this one is still holding "true".
    setSpy.mockRestore();
    window.localStorage.setItem(KEY, "0");
    expect(loadInlineParallelToolCalls()).toBe(false);
  });

  it("reads the in-memory choice when getItem throws", () => {
    const getSpy = vi.spyOn(Storage.prototype, "getItem").mockImplementation(blocked);

    expect(loadInlineParallelToolCalls()).toBe(false);
    expect(getSpy).toHaveBeenCalled();

    vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked);
    saveInlineParallelToolCalls(true);
    expect(loadInlineParallelToolCalls()).toBe(true);
  });

  describe("when storage already holds a value and the write fails", () => {
    it("keeps the unsaved choice while storage still holds the old value", () => {
      window.localStorage.setItem(KEY, "1");
      vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked);

      saveInlineParallelToolCalls(false);
      expect(window.localStorage.getItem(KEY)).toBe("1");
      // Stable across repeated reads: the snapshot must not drift.
      expect(loadInlineParallelToolCalls()).toBe(false);
      expect(loadInlineParallelToolCalls()).toBe(false);
    });

    it("lets storage win once another tab writes a different value", () => {
      window.localStorage.setItem(KEY, "1");
      const setSpy = vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked);
      // The failed write keeps "true" in memory, matching what storage holds.
      saveInlineParallelToolCalls(true);
      expect(loadInlineParallelToolCalls()).toBe(true);

      // Another tab turns it off. Storage now differs from the recorded "1".
      setSpy.mockRestore();
      window.localStorage.setItem(KEY, "0");
      expect(loadInlineParallelToolCalls()).toBe(false);
    });

    it("drops the fallback for good, even if storage returns to the old value", () => {
      window.localStorage.setItem(KEY, "1");
      const setSpy = vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked);
      saveInlineParallelToolCalls(false);
      expect(loadInlineParallelToolCalls()).toBe(false);

      // Another tab writes "0" (storage wins, fallback cleared), then "1" again.
      // The stale in-memory "false" must not come back.
      setSpy.mockRestore();
      window.localStorage.setItem(KEY, "0");
      expect(loadInlineParallelToolCalls()).toBe(false);
      window.localStorage.setItem(KEY, "1");
      expect(loadInlineParallelToolCalls()).toBe(true);
    });

    it("lets storage win when the key is removed from another tab", () => {
      window.localStorage.setItem(KEY, "1");
      const setSpy = vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked);
      saveInlineParallelToolCalls(false);
      expect(loadInlineParallelToolCalls()).toBe(false);

      // Storage no longer holds "1": the key is gone, so the read is off, and the
      // fallback is cleared rather than applied.
      setSpy.mockRestore();
      window.localStorage.removeItem(KEY);
      expect(loadInlineParallelToolCalls()).toBe(false);
      window.localStorage.setItem(KEY, "1");
      expect(loadInlineParallelToolCalls()).toBe(true);
    });
  });

  describe("when the baseline could not be read at save time", () => {
    // Reads and the write both fail at save time, so what storage held is unknown.
    // Returns the spies so the test can restore storage to working order.
    const blockReadsAndWrites = () => ({
      getSpy: vi.spyOn(Storage.prototype, "getItem").mockImplementation(blocked),
      setSpy: vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked),
    });

    it("keeps the user's choice once storage is readable, even if it holds the opposite value", () => {
      const { getSpy, setSpy } = blockReadsAndWrites();
      saveInlineParallelToolCalls(true);
      expect(loadInlineParallelToolCalls()).toBe(true);

      // Storage recovers holding "0". The first readable load adopts that as the
      // baseline, so the outage choice still applies, and it stays stable.
      getSpy.mockRestore();
      setSpy.mockRestore();
      window.localStorage.setItem(KEY, "0");
      expect(loadInlineParallelToolCalls()).toBe(true);
      expect(loadInlineParallelToolCalls()).toBe(true);
      expect(loadInlineParallelToolCalls()).toBe(true);
    });

    it("still detects another tab's write that arrives without an event", () => {
      const { getSpy, setSpy } = blockReadsAndWrites();
      saveInlineParallelToolCalls(true);
      getSpy.mockRestore();
      setSpy.mockRestore();
      window.localStorage.setItem(KEY, "0");
      expect(loadInlineParallelToolCalls()).toBe(true);

      // Adopted baseline "0" no longer matches storage: storage wins.
      window.localStorage.removeItem(KEY);
      expect(loadInlineParallelToolCalls()).toBe(false);
    });

    it("hands the choice to storage on a storage event for the key, and notifies subscribers", () => {
      const { getSpy, setSpy } = blockReadsAndWrites();
      saveInlineParallelToolCalls(true);
      getSpy.mockRestore();
      setSpy.mockRestore();
      window.localStorage.setItem(KEY, "0");
      expect(loadInlineParallelToolCalls()).toBe(true);

      // Another tab removes the key. The event is what tells this document.
      const onChange = vi.fn();
      const unsubscribe = subscribeInlineParallelToolCalls(onChange);
      window.localStorage.removeItem(KEY);
      window.dispatchEvent(new StorageEvent("storage", { key: KEY, newValue: null }));
      expect(onChange).toHaveBeenCalledTimes(1);
      expect(loadInlineParallelToolCalls()).toBe(false);

      window.localStorage.setItem(KEY, "1");
      expect(loadInlineParallelToolCalls()).toBe(true);
      unsubscribe();
    });

    it("treats a storage event for the key as a write even when the value is unchanged", () => {
      const { getSpy, setSpy } = blockReadsAndWrites();
      saveInlineParallelToolCalls(true);
      getSpy.mockRestore();
      setSpy.mockRestore();
      window.localStorage.setItem(KEY, "0");
      expect(loadInlineParallelToolCalls()).toBe(true);

      // Another tab wrote "0" again. Storage still matches the adopted baseline, so
      // only the event can tell that a write happened: storage wins. The listener
      // exists only while subscribed, as it is while a component reads the value.
      const unsubscribe = subscribeInlineParallelToolCalls(() => {});
      window.localStorage.setItem(KEY, "0");
      window.dispatchEvent(new StorageEvent("storage", { key: KEY, newValue: "0" }));
      expect(loadInlineParallelToolCalls()).toBe(false);
      unsubscribe();
    });

    it("ignores a storage event for a different key", () => {
      const { getSpy, setSpy } = blockReadsAndWrites();
      saveInlineParallelToolCalls(true);
      getSpy.mockRestore();
      setSpy.mockRestore();
      window.localStorage.setItem(KEY, "0");
      expect(loadInlineParallelToolCalls()).toBe(true);

      const onChange = vi.fn();
      const unsubscribe = subscribeInlineParallelToolCalls(onChange);
      window.dispatchEvent(new StorageEvent("storage", { key: "ocode:unrelated", newValue: "1" }));
      expect(onChange).not.toHaveBeenCalled();
      expect(loadInlineParallelToolCalls()).toBe(true);
      expect(loadInlineParallelToolCalls()).toBe(true);
      unsubscribe();
    });
  });
});
