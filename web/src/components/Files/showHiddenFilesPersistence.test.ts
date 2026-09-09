import { describe, it, expect, beforeEach } from "vitest";
import { loadShowHiddenFiles, saveShowHiddenFiles, subscribeShowHiddenFiles, SHOW_HIDDEN_FILES_STORAGE_KEY } from "./showHiddenFilesPersistence";

const KEY = SHOW_HIDDEN_FILES_STORAGE_KEY;

describe("showHiddenFilesPersistence", () => {
  beforeEach(() => {
    window.localStorage.removeItem(KEY);
  });

  it("defaults to false when nothing stored", () => {
    expect(loadShowHiddenFiles()).toBe(false);
  });

  it("persists true", () => {
    saveShowHiddenFiles(true);
    expect(loadShowHiddenFiles()).toBe(true);
    expect(window.localStorage.getItem(KEY)).toBe("true");
  });

  it("persists false", () => {
    saveShowHiddenFiles(false);
    expect(loadShowHiddenFiles()).toBe(false);
    expect(window.localStorage.getItem(KEY)).toBe("false");
  });

  it("falls back to false on invalid value", () => {
    window.localStorage.setItem(KEY, "maybe");
    expect(loadShowHiddenFiles()).toBe(false);
  });

  it("tolerates missing localStorage", () => {
    const orig = window.localStorage;
    delete (window as unknown as Record<string, unknown>).localStorage;
    expect(loadShowHiddenFiles()).toBe(false);
    (window as unknown as { localStorage: Storage }).localStorage = orig;
  });

  it("notifies same-document subscribers on save", () => {
    const seen: boolean[] = [];
    const unsub = subscribeShowHiddenFiles((v) => seen.push(v));
    saveShowHiddenFiles(true);
    saveShowHiddenFiles(false);
    unsub();
    saveShowHiddenFiles(true);
    expect(seen).toEqual([true, false]);
  });

  it("notifies subscribers on cross-document storage events", () => {
    const seen: boolean[] = [];
    const unsub = subscribeShowHiddenFiles((v) => seen.push(v));
    window.dispatchEvent(new StorageEvent("storage", { key: KEY, newValue: "true" }));
    window.dispatchEvent(new StorageEvent("storage", { key: "other", newValue: "true" }));
    unsub();
    expect(seen).toEqual([true]);
  });
});
