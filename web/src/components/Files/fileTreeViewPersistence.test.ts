import { describe, it, expect, beforeEach } from "vitest";
import { loadFileTreeView, saveFileTreeView } from "./fileTreeViewPersistence";

const KEY = "ocode.ui.filetree_view.v1";

describe("fileTreeViewPersistence", () => {
  beforeEach(() => {
    window.localStorage.removeItem(KEY);
  });

  it("defaults to tree when nothing stored", () => {
    expect(loadFileTreeView()).toBe("tree");
  });

  it("persists and restores columns", () => {
    saveFileTreeView("columns");
    expect(loadFileTreeView()).toBe("columns");
    expect(window.localStorage.getItem(KEY)).toBe("columns");
  });

  it("persists and restores tree", () => {
    saveFileTreeView("tree");
    expect(loadFileTreeView()).toBe("tree");
  });

  it("falls back to tree on invalid value", () => {
    window.localStorage.setItem(KEY, "grid");
    expect(loadFileTreeView()).toBe("tree");
  });

  it("falls back to tree on empty string", () => {
    window.localStorage.setItem(KEY, "");
    expect(loadFileTreeView()).toBe("tree");
  });

  it("tolerates missing localStorage (SSR) and returns tree", () => {
    const orig = window.localStorage;
    // delete is intentional for SSR fallback — suppress unused @ts-expect-error
    delete (window as unknown as Record<string, unknown>).localStorage;
    expect(loadFileTreeView()).toBe("tree");
    // restore
    (window as unknown as { localStorage: Storage }).localStorage = orig;
  });
});
