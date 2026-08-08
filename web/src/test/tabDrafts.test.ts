import { describe, expect, it, beforeEach } from "vitest";
import { getDraft, setDraft, clearDraft, isNewSessionTabEmpty } from "../lib/tabDrafts";

// tabDrafts is a module-level store, so tests share state — reset between tests.
describe("tabDrafts", () => {
  beforeEach(() => {
    clearDraft("tab-a");
    clearDraft("tab-b");
  });

  it("returns empty draft for unknown or null tab", () => {
    expect(getDraft("tab-a")).toBe("");
    expect(getDraft(null)).toBe("");
    expect(getDraft(undefined)).toBe("");
  });

  it("stores and retrieves drafts per tab id", () => {
    setDraft("tab-a", "hello");
    setDraft("tab-b", "world");
    expect(getDraft("tab-a")).toBe("hello");
    expect(getDraft("tab-b")).toBe("world");
  });

  it("overwrites a draft on repeated writes", () => {
    setDraft("tab-a", "first");
    setDraft("tab-a", "second");
    expect(getDraft("tab-a")).toBe("second");
  });

  it("clears a draft", () => {
    setDraft("tab-a", "hello");
    clearDraft("tab-a");
    expect(getDraft("tab-a")).toBe("");
  });

  it("ignores null/undefined writes", () => {
    setDraft(null, "nope");
    setDraft(undefined, "nope");
    expect(getDraft(null)).toBe("");
  });

  it("isNewSessionTabEmpty: empty new- tab", () => {
    clearDraft("new-123");
    expect(isNewSessionTabEmpty("new-123")).toBe(true);
  });

  it("isNewSessionTabEmpty: new- tab with draft is not empty", () => {
    setDraft("new-123", "hello");
    expect(isNewSessionTabEmpty("new-123")).toBe(false);
  });

  it("isNewSessionTabEmpty: real session tab is never empty", () => {
    expect(isNewSessionTabEmpty("session-abc")).toBe(false);
  });

  it("isNewSessionTabEmpty: null/undefined", () => {
    expect(isNewSessionTabEmpty(null)).toBe(false);
    expect(isNewSessionTabEmpty(undefined)).toBe(false);
  });
});
