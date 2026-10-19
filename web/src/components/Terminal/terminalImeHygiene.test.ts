import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { attachImeHygiene } from "./terminalImeHygiene";

describe("attachImeHygiene", () => {
  let ta: HTMLTextAreaElement;
  let dispose: () => void;
  beforeEach(() => {
    vi.useFakeTimers();
    ta = document.createElement("textarea");
    dispose = attachImeHygiene(ta);
  });
  afterEach(() => {
    dispose();
    vi.useRealTimers();
  });

  it("clears the textarea after a composition ends", () => {
    ta.dispatchEvent(new Event("compositionstart"));
    ta.value = "hello";
    ta.dispatchEvent(new Event("compositionend"));
    expect(ta.value).toBe("hello");
    vi.advanceTimersByTime(60);
    expect(ta.value).toBe("");
  });

  it("clears after a non-composing input", () => {
    ta.value = "2";
    ta.dispatchEvent(new Event("input"));
    vi.advanceTimersByTime(60);
    expect(ta.value).toBe("");
  });

  it("never clears while a composition is in flight", () => {
    ta.dispatchEvent(new Event("input"));
    ta.dispatchEvent(new Event("compositionstart"));
    ta.value = "wor";
    vi.advanceTimersByTime(200);
    expect(ta.value).toBe("wor");
  });

  it("stops clearing after dispose", () => {
    ta.value = "x";
    ta.dispatchEvent(new Event("input"));
    dispose();
    vi.advanceTimersByTime(200);
    expect(ta.value).toBe("x");
  });
});
