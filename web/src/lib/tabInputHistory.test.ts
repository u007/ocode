import { describe, it, expect, beforeEach } from "vitest";
import {
  getInputHistory,
  pushInputHistory,
  rekeyInputHistory,
  clearInputHistory,
  MAX_INPUT_HISTORY,
} from "./tabInputHistory";

const A = "hist-a";
const B = "hist-b";

describe("tabInputHistory", () => {
  beforeEach(() => {
    clearInputHistory(A);
    clearInputHistory(B);
    clearInputHistory("new-1");
    clearInputHistory("new-2");
  });

  it("records submitted text in order and reads it back", () => {
    pushInputHistory(A, "hello");
    pushInputHistory(A, "world");
    expect(getInputHistory(A)).toEqual(["hello", "world"]);
  });

  it("ignores empty text and `!` shell commands", () => {
    pushInputHistory(A, "");
    pushInputHistory(A, "   ");
    pushInputHistory(A, "!ls -la");
    expect(getInputHistory(A)).toEqual([]);
  });

  it("collapses an immediate repeat but keeps a later identical entry", () => {
    pushInputHistory(A, "same");
    pushInputHistory(A, "same");
    pushInputHistory(A, "other");
    pushInputHistory(A, "same");
    expect(getInputHistory(A)).toEqual(["same", "other", "same"]);
  });

  it("trims the submitted text", () => {
    pushInputHistory(A, "  spaced  ");
    expect(getInputHistory(A)).toEqual(["spaced"]);
  });

  it("returns an empty list for a null tab id", () => {
    expect(getInputHistory(null)).toEqual([]);
    pushInputHistory(null, "x");
    expect(getInputHistory(null)).toEqual([]);
  });

  it("caps the list, dropping the oldest entries", () => {
    for (let i = 0; i < MAX_INPUT_HISTORY + 5; i++) pushInputHistory(A, `m${i}`);
    const list = getInputHistory(A);
    expect(list).toHaveLength(MAX_INPUT_HISTORY);
    expect(list[0]).toBe("m5");
    expect(list[list.length - 1]).toBe(`m${MAX_INPUT_HISTORY + 4}`);
  });

  it("rekeys history onto a real session id, oldest entries first, no leak", () => {
    pushInputHistory("new-1", "first");
    pushInputHistory("new-1", "second");
    pushInputHistory(B, "later");
    rekeyInputHistory("new-1", B);
    expect(getInputHistory("new-1")).toEqual([]);
    expect(getInputHistory(B)).toEqual(["first", "second", "later"]);
  });

  it("is a no-op for a same-id or missing rekey", () => {
    pushInputHistory(A, "keep");
    rekeyInputHistory(A, A);
    rekeyInputHistory("missing", A);
    expect(getInputHistory(A)).toEqual(["keep"]);
  });

  it("clearInputHistory drops the tab's list", () => {
    pushInputHistory(A, "x");
    clearInputHistory(A);
    expect(getInputHistory(A)).toEqual([]);
  });
});
