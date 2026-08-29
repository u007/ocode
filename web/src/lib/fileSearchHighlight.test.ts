import { describe, it, expect, beforeEach, vi } from "vitest";
import { setPendingHighlight, consumePendingHighlight, peekPendingHighlight } from "./fileSearchHighlight";

describe("fileSearchHighlight pending store", () => {
  beforeEach(() => {
    // Clear by consuming any leftover
    consumePendingHighlight("a.txt");
    consumePendingHighlight("a.txt", "/proj1");
    consumePendingHighlight("a.txt", "/proj2");
  });

  it("stores and consumes highlight with projectRoot key", () => {
    setPendingHighlight("a.txt", "hello", 10, "/proj1");
    setPendingHighlight("a.txt", "world", 20, "/proj2");
    // Same relative path but different root should not collide
    const h1 = peekPendingHighlight("a.txt", "/proj1");
    const h2 = peekPendingHighlight("a.txt", "/proj2");
    expect(h1?.query).toBe("hello");
    expect(h1?.line).toBe(10);
    expect(h2?.query).toBe("world");
    expect(h2?.line).toBe(20);
    // Consuming one should not affect the other
    const c1 = consumePendingHighlight("a.txt", "/proj1");
    expect(c1?.query).toBe("hello");
    expect(peekPendingHighlight("a.txt", "/proj1")).toBeNull();
    expect(peekPendingHighlight("a.txt", "/proj2")?.query).toBe("world");
  });

  it("timer does not delete newer highlight (captured ts bug)", async () => {
    vi.useFakeTimers();
    setPendingHighlight("b.txt", "first", 1);
    vi.advanceTimersByTime(15000);
    // Overwrite after 15s
    setPendingHighlight("b.txt", "second", 2);
    // After 15s more (total 30s from first), first timer fires but should not delete second
    vi.advanceTimersByTime(15000);
    expect(peekPendingHighlight("b.txt")?.query).toBe("second");
    // After another 15s (30s from second), second's timer fires and clears
    vi.advanceTimersByTime(15000);
    expect(peekPendingHighlight("b.txt")).toBeNull();
    vi.useRealTimers();
  });

  it("consume removes pending and returns highlight", () => {
    setPendingHighlight("c.txt", "query", 5);
    const h = consumePendingHighlight("c.txt");
    expect(h?.query).toBe("query");
    expect(h?.line).toBe(5);
    expect(peekPendingHighlight("c.txt")).toBeNull();
    expect(consumePendingHighlight("c.txt")).toBeNull();
  });

  it("handles rapid query changes with generation guard simulation", () => {
    // Simulate FileTree generation guard: last write wins
    setPendingHighlight("d.txt", "old", 1);
    setPendingHighlight("d.txt", "new", 2);
    const h = consumePendingHighlight("d.txt");
    expect(h?.query).toBe("new");
  });
});
