import { describe, it, expect, beforeEach } from "vitest";
import {
  loadShowHiddenFiles,
  saveShowHiddenFiles,
  subscribeShowHiddenFiles,
  showHiddenFilesProjectKey,
  SHOW_HIDDEN_FILES_STORAGE_KEY,
} from "./showHiddenFilesPersistence";

const GLOBAL_KEY = SHOW_HIDDEN_FILES_STORAGE_KEY;
const A = showHiddenFilesProjectKey("/proj/a", undefined);
const B = showHiddenFilesProjectKey("/proj/b", undefined);

function projectStorageKey(projectKey: string): string {
  return `${GLOBAL_KEY}:${projectKey}`;
}

describe("showHiddenFilesPersistence (per project)", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("derives distinct keys per path and remote host", () => {
    expect(showHiddenFilesProjectKey("/proj", undefined)).toBe("/proj");
    expect(showHiddenFilesProjectKey("/proj/", undefined)).toBe("/proj");
    expect(showHiddenFilesProjectKey("/proj", "me@host")).toBe("me@host\u0000/proj");
    expect(showHiddenFilesProjectKey("~/www/app", "me@host")).toBe("me@host\u0000~/www/app");
    expect(showHiddenFilesProjectKey(undefined, undefined)).toBe("");
  });

  it("defaults to false when nothing stored", () => {
    expect(loadShowHiddenFiles(A)).toBe(false);
  });

  it("persists per project and keeps projects independent", () => {
    saveShowHiddenFiles(A, true);
    expect(loadShowHiddenFiles(A)).toBe(true);
    expect(loadShowHiddenFiles(B)).toBe(false);
    expect(window.localStorage.getItem(projectStorageKey(A))).toBe("true");

    saveShowHiddenFiles(B, true);
    saveShowHiddenFiles(A, false);
    expect(loadShowHiddenFiles(A)).toBe(false);
    expect(loadShowHiddenFiles(B)).toBe(true);
  });

  it("distinguishes the same path on different remote hosts", () => {
    const hostA = showHiddenFilesProjectKey("~/www/app", "a@host");
    const hostB = showHiddenFilesProjectKey("~/www/app", "b@host");
    saveShowHiddenFiles(hostA, true);
    expect(loadShowHiddenFiles(hostA)).toBe(true);
    expect(loadShowHiddenFiles(hostB)).toBe(false);
  });

  it("falls back to the legacy global value for projects without an override", () => {
    window.localStorage.setItem(GLOBAL_KEY, "true");
    expect(loadShowHiddenFiles(A)).toBe(true);
    // An explicit per-project choice wins over the legacy default.
    saveShowHiddenFiles(A, false);
    expect(loadShowHiddenFiles(A)).toBe(false);
    expect(loadShowHiddenFiles(B)).toBe(true);
  });

  it("falls back to false on an invalid stored value", () => {
    window.localStorage.setItem(projectStorageKey(A), "maybe");
    expect(loadShowHiddenFiles(A)).toBe(false);
  });

  it("tolerates missing localStorage", () => {
    const orig = window.localStorage;
    delete (window as unknown as Record<string, unknown>).localStorage;
    expect(loadShowHiddenFiles(A)).toBe(false);
    (window as unknown as { localStorage: Storage }).localStorage = orig;
  });

  it("notifies same-document subscribers for the matching project only", () => {
    const seen: boolean[] = [];
    const unsub = subscribeShowHiddenFiles(A, (v) => seen.push(v));
    saveShowHiddenFiles(A, true);
    saveShowHiddenFiles(B, true); // different project — ignored
    saveShowHiddenFiles(A, false);
    unsub();
    saveShowHiddenFiles(A, true); // after unsubscribe — ignored
    expect(seen).toEqual([true, false]);
  });

  it("notifies subscribers on cross-document storage events for its project", () => {
    const seen: boolean[] = [];
    const unsub = subscribeShowHiddenFiles(A, (v) => seen.push(v));
    window.dispatchEvent(new StorageEvent("storage", { key: projectStorageKey(A), newValue: "true" }));
    window.dispatchEvent(new StorageEvent("storage", { key: projectStorageKey(B), newValue: "true" }));
    window.dispatchEvent(new StorageEvent("storage", { key: "other", newValue: "true" }));
    window.dispatchEvent(new StorageEvent("storage", { key: projectStorageKey(A), newValue: null }));
    unsub();
    expect(seen).toEqual([true]);
  });
});
