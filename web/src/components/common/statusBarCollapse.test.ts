import { describe, it, expect, beforeEach } from "vitest";
import {
  loadStatusBarCollapsed,
  saveStatusBarCollapsed,
  subscribeStatusBarCollapsed,
  STATUS_BAR_COLLAPSED_STORAGE_KEY,
} from "./statusBarCollapse";

const KEY = STATUS_BAR_COLLAPSED_STORAGE_KEY;

describe("statusBarCollapse persistence", () => {
  beforeEach(() => {
    window.localStorage.removeItem(KEY);
  });

  it("defaults to false (expanded) when nothing stored", () => {
    expect(loadStatusBarCollapsed()).toBe(false);
  });

  it("persists true", () => {
    saveStatusBarCollapsed(true);
    expect(loadStatusBarCollapsed()).toBe(true);
    expect(window.localStorage.getItem(KEY)).toBe("true");
  });

  it("persists false", () => {
    saveStatusBarCollapsed(true);
    saveStatusBarCollapsed(false);
    expect(loadStatusBarCollapsed()).toBe(false);
    expect(window.localStorage.getItem(KEY)).toBe("false");
  });

  it("falls back to false on invalid value", () => {
    window.localStorage.setItem(KEY, "maybe");
    expect(loadStatusBarCollapsed()).toBe(false);
  });

  it("tolerates missing localStorage", () => {
    const orig = window.localStorage;
    delete (window as unknown as Record<string, unknown>).localStorage;
    expect(loadStatusBarCollapsed()).toBe(false);
    (window as unknown as { localStorage: Storage }).localStorage = orig;
  });

  it("notifies same-document subscribers on save", () => {
    const seen: boolean[] = [];
    const unsub = subscribeStatusBarCollapsed((v) => seen.push(v));
    saveStatusBarCollapsed(true);
    saveStatusBarCollapsed(false);
    unsub();
    saveStatusBarCollapsed(true);
    expect(seen).toEqual([true, false]);
  });

  it("notifies subscribers on cross-document storage events", () => {
    const seen: boolean[] = [];
    const unsub = subscribeStatusBarCollapsed((v) => seen.push(v));
    window.dispatchEvent(new StorageEvent("storage", { key: KEY, newValue: "true" }));
    unsub();
    expect(seen).toEqual([true]);
  });

  it("ignores storage events for other keys", () => {
    const seen: boolean[] = [];
    const unsub = subscribeStatusBarCollapsed((v) => seen.push(v));
    window.dispatchEvent(new StorageEvent("storage", { key: "other.key", newValue: "true" }));
    unsub();
    expect(seen).toEqual([]);
  });
});
