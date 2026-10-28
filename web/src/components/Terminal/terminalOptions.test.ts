import { describe, expect, it } from "vitest";
import type { Ghostty } from "ghostty-web";
import { buildTerminalOptions, TERMINAL_THEME_DEFAULTS } from "./terminalOptions";

// The panel's constructor options are asserted directly: ghostty-web cannot open
// in jsdom (no canvas), and, unlike xterm, an option it does not know is a type
// error rather than a silent no-op, so the shape of this object is the contract.

const ghostty = { marker: "ghostty" } as unknown as Ghostty;
const PROD_OPTIONS = { scrollbackLines: 1000, fontFamily: "monospace", fontSize: 12, ghostty };

describe("buildTerminalOptions", () => {
  it("hands the shared libghostty instance to the terminal", () => {
    expect(buildTerminalOptions(PROD_OPTIONS).ghostty).toBe(ghostty);
  });

  it("carries the configured scrollback, font and instant scrolling", () => {
    const opts = buildTerminalOptions({ ...PROD_OPTIONS, scrollbackLines: 42 });
    expect(opts.scrollback).toBe(42);
    expect(opts.fontFamily).toBe("monospace");
    expect(opts.fontSize).toBe(12);
    expect(opts.cursorBlink).toBe(true);
    expect(opts.smoothScrollDuration).toBe(0);
  });

  it("uses the dark theme the serializer treats as 'default colour'", () => {
    const opts = buildTerminalOptions(PROD_OPTIONS);
    expect(opts.theme).toEqual({ background: "#18181b", foreground: "#e4e4e7" });
    // The serializer recognises default colours by their RGB value, so the two
    // representations of the theme must never drift apart.
    expect(TERMINAL_THEME_DEFAULTS).toEqual({
      foreground: parseInt("e4e4e7", 16),
      background: parseInt("18181b", 16),
    });
  });

  it("constructs at the saved buffer geometry and falls back to defaults otherwise", () => {
    const withGeom = buildTerminalOptions({ ...PROD_OPTIONS, savedBuffer: { cols: 120, rows: 40 } });
    expect(withGeom.cols).toBe(120);
    expect(withGeom.rows).toBe(40);
    const legacy = buildTerminalOptions({ ...PROD_OPTIONS, savedBuffer: { cols: 0, rows: 0 } });
    expect(legacy.cols).toBeUndefined();
    expect(legacy.rows).toBeUndefined();
  });

  it("carries no xterm-only options", () => {
    const keys = Object.keys(buildTerminalOptions(PROD_OPTIONS));
    for (const gone of [
      "allowProposedApi",
      "scrollSensitivity",
      "fastScrollSensitivity",
      "macOptionClickForcesSelection",
    ]) {
      expect(keys).not.toContain(gone);
    }
  });
});
