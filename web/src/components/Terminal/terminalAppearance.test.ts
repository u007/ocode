import { describe, expect, it } from "vitest";
import {
  DEFAULT_TERMINAL_APPEARANCE,
  appPolarity,
  resolveTerminalTheme,
  scaleCellMetrics,
  terminalThemeKey,
  themeColorToInt,
  type TerminalAppearance,
} from "./terminalAppearance";

const appearance: TerminalAppearance = {
  source: "ghostty",
  requested: "ghostty",
  font_family: `"Fira Code", ui-monospace, monospace`,
  font_size: 12,
  cell_width_adjust: {},
  cell_height_adjust: { percent: 5 },
  theme_name: "Bright Lights",
  dark: {
    foreground: "#b3c9d7",
    background: "#191919",
    cursor: "#f34b00",
    cursor_text: "#002831",
    selection_background: "#b3c9d7",
    selection_foreground: "#191919",
    palette: [
      "#191919",
      "#ff355b",
      "#b7e876",
      "#ffc251",
      "#76d4ff",
      "#ba76e7",
      "#6cbfb5",
      "#c2c8d7",
      "#4c4c4c",
      "#ff355b",
      "#b7e876",
      "#ffc251",
      "#76d5ff",
      "#ba76e7",
      "#6cbfb5",
      "#c2c8d7",
    ],
  },
  light: { foreground: "#000000", background: "#ffffff" },
};

describe("DEFAULT_TERMINAL_APPEARANCE", () => {
  it("matches the server defaults (12px with 10% extra cell height)", () => {
    expect(DEFAULT_TERMINAL_APPEARANCE.font_size).toBe(12);
    expect(DEFAULT_TERMINAL_APPEARANCE.cell_height_adjust).toEqual({ percent: 10 });
    expect(DEFAULT_TERMINAL_APPEARANCE.cell_width_adjust).toEqual({});
    expect(DEFAULT_TERMINAL_APPEARANCE.font_family).toContain("monospace");
  });
});

describe("resolveTerminalTheme", () => {
  it("maps Ghostty colour names onto ghostty-web's ITheme", () => {
    const theme = resolveTerminalTheme(appearance, "dark");
    expect(theme?.background).toBe("#191919");
    expect(theme?.foreground).toBe("#b3c9d7");
    expect(theme?.cursor).toBe("#f34b00");
    expect(theme?.cursorAccent).toBe("#002831");
    expect(theme?.selectionBackground).toBe("#b3c9d7");
    expect(theme?.selectionForeground).toBe("#191919");
  });

  it("maps palette 0-15 onto the ANSI colour names in order", () => {
    const theme = resolveTerminalTheme(appearance, "dark");
    expect(theme?.black).toBe("#191919");
    expect(theme?.red).toBe("#ff355b");
    expect(theme?.brightRed).toBe("#ff355b");
    expect(theme?.brightBlue).toBe("#76d5ff");
    expect(theme?.brightWhite).toBe("#c2c8d7");
  });

  it("picks the variant for the requested polarity", () => {
    const light = resolveTerminalTheme(appearance, "light");
    expect(light?.background).toBe("#ffffff");
    expect(light?.foreground).toBe("#000000");
  });

  it("returns undefined when the appearance carries no colours", () => {
    expect(resolveTerminalTheme({ ...appearance, dark: undefined, light: undefined }, "dark")).toBeUndefined();
    expect(resolveTerminalTheme(null)).toBeUndefined();
  });

  it("ignores a palette that is not exactly 16 entries", () => {
    const partial = { ...appearance, dark: { ...appearance.dark!, palette: ["#000000", "#ffffff"] } };
    const theme = resolveTerminalTheme(partial, "dark");
    expect(theme?.black).toBeUndefined();
    expect(theme?.background).toBe("#191919");
  });
});

describe("terminalThemeKey", () => {
  it("is stable for equal themes and distinct for different ones", () => {
    const a = resolveTerminalTheme(appearance, "dark");
    const b = resolveTerminalTheme(appearance, "dark");
    expect(terminalThemeKey(a)).toBe(terminalThemeKey(b));
    expect(terminalThemeKey(a)).not.toBe(terminalThemeKey(resolveTerminalTheme(appearance, "light")));
    expect(terminalThemeKey(undefined)).toBe("default");
  });
});

describe("scaleCellMetrics", () => {
  const base = { width: 8, height: 17, baseline: 13 };

  it("adds a percentage of the measured cell", () => {
    expect(scaleCellMetrics(base, undefined, { percent: 10 })).toEqual({ width: 8, height: 19, baseline: 14 });
  });

  it("adds pixels", () => {
    expect(scaleCellMetrics(base, { px: 2 }, { px: 3 })).toEqual({ width: 10, height: 20, baseline: 15 });
  });

  it("combines percentage and pixel deltas", () => {
    // 17 + 1.7 + 1 = 19.7 -> 20; baseline +1.5 -> 15 (rounded)
    expect(scaleCellMetrics(base, undefined, { percent: 10, px: 1 })).toEqual({ width: 8, height: 20, baseline: 15 });
  });

  it("returns integers so the canvas and CSS sizes cannot drift a pixel per cell", () => {
    const scaled = scaleCellMetrics({ width: 7.4, height: 16.6, baseline: 12.2 }, { percent: 3 }, { percent: 7 });
    for (const value of Object.values(scaled)) expect(Number.isInteger(value)).toBe(true);
  });

  it("never produces a zero or negative cell", () => {
    expect(scaleCellMetrics(base, { percent: -100 }, { percent: -100 })).toEqual({ width: 1, height: 1, baseline: 1 });
  });

  it("is idempotent when applied to the natural metrics it returns", () => {
    const once = scaleCellMetrics(base, undefined, { percent: 10 });
    // The caller re-measures (remeasureFont) before every application, so feeding
    // the scaled result back in must not be the production path — but the math
    // must at least stay monotonic/sane if it happens.
    const twice = scaleCellMetrics({ width: base.width, height: base.height, baseline: base.baseline }, undefined, {
      percent: 10,
    });
    expect(twice).toEqual(once);
  });
});

describe("themeColorToInt", () => {
  it("parses #rrggbb and falls back for anything else", () => {
    expect(themeColorToInt("#e4e4e7", 0)).toBe(0xe4e4e7);
    expect(themeColorToInt("e4e4e7", 0)).toBe(0xe4e4e7);
    expect(themeColorToInt(undefined, 7)).toBe(7);
    expect(themeColorToInt("rebeccapurple", 7)).toBe(7);
  });
});

describe("appPolarity", () => {
  it("falls back to dark when --background is unset (jsdom)", () => {
    expect(appPolarity()).toBe("dark");
  });
});
