import { describe, expect, it } from "vitest";
import {
  softKeyboardBackspace,
  softKeyboardNavigationSequence,
  type SoftKeyboardEventShape,
} from "./terminalSoftKeyboard";

function ev(init: Partial<SoftKeyboardEventShape> & { key: string }): SoftKeyboardEventShape {
  return {
    code: "",
    keyCode: 0,
    ctrlKey: false,
    altKey: false,
    metaKey: false,
    shiftKey: false,
    ...init,
  };
}

describe("softKeyboardBackspace", () => {
  it("sends DEL for a soft-keyboard Backspace (Android IME keyCode 229)", () => {
    expect(softKeyboardBackspace(ev({ key: "Backspace", keyCode: 229 }))).toBe("\x7f");
  });

  it("sends DEL for a Backspace with no physical code (keyCode 8)", () => {
    expect(softKeyboardBackspace(ev({ key: "Backspace", keyCode: 8 }))).toBe("\x7f");
  });

  it("sends DEL for a Backspace reporting code Unidentified", () => {
    expect(softKeyboardBackspace(ev({ key: "Backspace", code: "Unidentified", keyCode: 8 }))).toBe("\x7f");
  });

  it("leaves a hardware Backspace to ghostty-web (real code, keyCode 8)", () => {
    expect(softKeyboardBackspace(ev({ key: "Backspace", code: "Backspace", keyCode: 8 }))).toBeNull();
  });

  it("never claims a modified Backspace", () => {
    for (const mods of [{ ctrlKey: true }, { altKey: true }, { metaKey: true }, { shiftKey: true }]) {
      expect(softKeyboardBackspace(ev({ key: "Backspace", keyCode: 229, ...mods }))).toBeNull();
    }
  });

  it("ignores IME-marked keys that are not Backspace", () => {
    expect(softKeyboardBackspace(ev({ key: "Unidentified", keyCode: 229 }))).toBeNull();
    expect(softKeyboardBackspace(ev({ key: "a", keyCode: 229 }))).toBeNull();
  });

  it("ignores other keys", () => {
    expect(softKeyboardBackspace(ev({ key: "Enter", keyCode: 13 }))).toBeNull();
  });
});

describe("softKeyboardNavigationSequence (shared soft-keyboard check)", () => {
  it("still maps a soft-keyboard arrow and leaves a hardware arrow alone", () => {
    expect(softKeyboardNavigationSequence(ev({ key: "ArrowUp", keyCode: 229 }), false)).toBe("\x1b[A");
    expect(softKeyboardNavigationSequence(ev({ key: "ArrowUp", code: "ArrowUp", keyCode: 38 }), false)).toBeNull();
  });
});
