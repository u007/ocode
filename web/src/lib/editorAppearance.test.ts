import { beforeEach, describe, expect, it } from "vitest";
import { renderHook, act } from "@testing-library/react";
import {
  DEFAULT_EDITOR_APPEARANCE,
  EDITOR_APPEARANCE_STORAGE_KEY,
  getEditorAppearance,
  resolveEffectiveEditorAppearance,
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

  it("reads an absent stored value as auto", () => {
    expect(getEditorAppearance()).toBe("auto");
    expect(DEFAULT_EDITOR_APPEARANCE).toBe("auto");
  });

  it("reads a valid persisted value", () => {
    window.localStorage.setItem(EDITOR_APPEARANCE_STORAGE_KEY, "light");
    __resetEditorAppearanceForTests();
    expect(getEditorAppearance()).toBe("light");
  });

  it("keeps an explicitly stored preference", () => {
    window.localStorage.setItem(EDITOR_APPEARANCE_STORAGE_KEY, "dark");
    __resetEditorAppearanceForTests();
    expect(getEditorAppearance()).toBe("dark");
  });

  it("reads an explicitly stored auto as auto", () => {
    window.localStorage.setItem(EDITOR_APPEARANCE_STORAGE_KEY, "auto");
    __resetEditorAppearanceForTests();
    expect(getEditorAppearance()).toBe("auto");
  });

  it("treats an unparsable persisted value as auto", () => {
    window.localStorage.setItem(EDITOR_APPEARANCE_STORAGE_KEY, "blue");
    __resetEditorAppearanceForTests();
    expect(getEditorAppearance()).toBe("auto");
  });

  it("setEditorAppearance persists and re-renders subscribers", () => {
    const { result, unmount } = renderHook(() => useEditorAppearance());
    expect(result.current).toBe("auto");

    act(() => {
      setEditorAppearance("light");
    });

    expect(result.current).toBe("light");
    expect(window.localStorage.getItem(EDITOR_APPEARANCE_STORAGE_KEY)).toBe("light");

    // "Use theme colours" writes auto back.
    act(() => {
      setEditorAppearance("auto");
    });
    expect(result.current).toBe("auto");
    expect(window.localStorage.getItem(EDITOR_APPEARANCE_STORAGE_KEY)).toBe("auto");
    unmount();
  });

  it("toggleEditorAppearance flips and persists", () => {
    expect(toggleEditorAppearance("dark")).toBe("light");
    expect(window.localStorage.getItem(EDITOR_APPEARANCE_STORAGE_KEY)).toBe("light");
    expect(toggleEditorAppearance("light")).toBe("dark");
    expect(window.localStorage.getItem(EDITOR_APPEARANCE_STORAGE_KEY)).toBe("dark");
  });

  it("toggles away from the effective polarity when storage says auto", () => {
    // Auto on a LIGHT theme shows the light palette, so the first click must
    // switch to dark — not assume the old dark default.
    expect(toggleEditorAppearance("light")).toBe("dark");
    expect(window.localStorage.getItem(EDITOR_APPEARANCE_STORAGE_KEY)).toBe("dark");
  });

  it("applies a cross-tab storage event", () => {
    expect(getEditorAppearance()).toBe("auto");
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

  it("reverts to auto when another tab removes the key", () => {
    setEditorAppearance("dark");
    act(() => {
      window.dispatchEvent(
        new StorageEvent("storage", { key: EDITOR_APPEARANCE_STORAGE_KEY, newValue: null }),
      );
    });
    expect(getEditorAppearance()).toBe("auto");
  });

  it("ignores a storage event for a different key or an invalid value", () => {
    // Seed a NON-default value: with the store at "auto" an ignored invalid
    // value and an applied one are indistinguishable, so this used to pass
    // vacuously.
    setEditorAppearance("dark");
    expect(getEditorAppearance()).toBe("dark");
    window.dispatchEvent(new StorageEvent("storage", { key: "other", newValue: "light" }));
    expect(getEditorAppearance()).toBe("dark");
    window.dispatchEvent(
      new StorageEvent("storage", { key: EDITOR_APPEARANCE_STORAGE_KEY, newValue: "nope" }),
    );
    expect(getEditorAppearance()).toBe("dark");
  });
});

describe("resolveEffectiveEditorAppearance", () => {
  it.each([
    ["auto", "dark", "dark"],
    ["auto", "light", "light"],
    ["light", "dark", "light"],
    ["dark", "light", "dark"],
  ] as const)("resolves %s against a %s theme to %s", (value, polarity, expected) => {
    expect(resolveEffectiveEditorAppearance(value, polarity)).toBe(expected);
  });
});
