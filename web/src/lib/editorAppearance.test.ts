import { beforeEach, describe, expect, it } from "vitest";
import { renderHook, act } from "@testing-library/react";
import {
  EDITOR_APPEARANCE_STORAGE_KEY,
  getEditorAppearance,
  setEditorAppearance,
  toggleEditorAppearance,
  useEditorAppearance,
  __resetEditorAppearanceForTests,
} from "./editorAppearance";

describe("editorAppearance store", () => {
  beforeEach(() => {
    window.localStorage.clear();
    __resetEditorAppearanceForTests();
  });

  it("defaults to dark when nothing is stored and no app polarity is readable", () => {
    // jsdom does not load index.css, so getComputedStyle("--background") is
    // empty and the default must fall back to dark.
    expect(getEditorAppearance()).toBe("dark");
  });

  it("reads a valid persisted value", () => {
    window.localStorage.setItem(EDITOR_APPEARANCE_STORAGE_KEY, "light");
    __resetEditorAppearanceForTests();
    expect(getEditorAppearance()).toBe("light");
  });

  it("treats an invalid persisted value as unset instead of selecting it", () => {
    window.localStorage.setItem(EDITOR_APPEARANCE_STORAGE_KEY, "blue");
    __resetEditorAppearanceForTests();
    expect(getEditorAppearance()).toBe("dark");
  });

  it("setEditorAppearance persists and re-renders subscribers", () => {
    const { result, unmount } = renderHook(() => useEditorAppearance());
    expect(result.current).toBe("dark");

    act(() => {
      setEditorAppearance("light");
    });

    expect(result.current).toBe("light");
    expect(window.localStorage.getItem(EDITOR_APPEARANCE_STORAGE_KEY)).toBe("light");
    unmount();
  });

  it("toggleEditorAppearance flips and persists", () => {
    expect(toggleEditorAppearance()).toBe("light");
    expect(window.localStorage.getItem(EDITOR_APPEARANCE_STORAGE_KEY)).toBe("light");
    expect(toggleEditorAppearance()).toBe("dark");
    expect(window.localStorage.getItem(EDITOR_APPEARANCE_STORAGE_KEY)).toBe("dark");
  });

  it("applies a cross-tab storage event", () => {
    expect(getEditorAppearance()).toBe("dark");
    act(() => {
      window.dispatchEvent(
        new StorageEvent("storage", {
          key: EDITOR_APPEARANCE_STORAGE_KEY,
          newValue: "light",
        }),
      );
    });
    expect(getEditorAppearance()).toBe("light");
  });

  it("ignores a storage event for a different key or an invalid value", () => {
    window.dispatchEvent(new StorageEvent("storage", { key: "other", newValue: "light" }));
    expect(getEditorAppearance()).toBe("dark");
    window.dispatchEvent(
      new StorageEvent("storage", { key: EDITOR_APPEARANCE_STORAGE_KEY, newValue: "nope" }),
    );
    expect(getEditorAppearance()).toBe("dark");
  });
});
