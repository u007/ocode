import { describe, expect, it } from "vitest";
import { computeThemeVars, contrastRatio } from "./useTheme";
import { builtinPalettes } from "./themePalettes.fixture";

// WCAG AA for normal-size text — the floor every foreground/surface pair in
// the web UI must clear after mapping a terminal palette onto CSS variables.
const AA = 4.5;

// Pairs (surface var, foreground var) that render text directly on top of
// each other in the web UI (user bubbles, buttons, badges, cards, inputs,
// inline code chips, destructive actions, selection).
const TEXT_PAIRS: Array<[string, string]> = [
  ["--primary", "--primary-foreground"],
  ["--accent", "--accent-foreground"],
  ["--destructive", "--destructive-foreground"],
  ["--secondary", "--secondary-foreground"],
  ["--muted", "--muted-foreground"],
  ["--background", "--muted-foreground"],
  // Prose links (file-path links, markdown anchors) render on the assistant
  // muted bubble and on plain page-colored surfaces (card/popover = background).
  ["--muted", "--link"],
  ["--background", "--link"],
];

describe("computeThemeVars contrast", () => {
  for (const [name, colors] of Object.entries(builtinPalettes)) {
    it(`keeps every text/surface pair readable for theme "${name}"`, () => {
      const vars = computeThemeVars(colors);
      for (const [bgVar, fgVar] of TEXT_PAIRS) {
        const ratio = contrastRatio(vars[bgVar], vars[fgVar]);
        expect(
          ratio,
          `${name}: ${fgVar} (${vars[fgVar]}) on ${bgVar} (${vars[bgVar]}) = ${ratio.toFixed(2)}:1`,
        ).toBeGreaterThanOrEqual(AA);
      }
      // Every derived value must be a valid hex — applyThemeColors silently
      // skips invalid ones, which would leave stale values from the previous
      // theme on screen.
      for (const [varName, value] of Object.entries(vars)) {
        expect(value, `${name}: ${varName} must be #rrggbb`).toMatch(/^#[0-9a-f]{6}$/i);
      }
    });
  }

  it("fixes the LCARS user-bubble regression (tan text on orange bg)", () => {
    const lcars = builtinPalettes.lcars;
    // The raw palette pair was unreadable — this is the bug being fixed.
    expect(contrastRatio(lcars.user, lcars.text)).toBeLessThan(AA);
    // The derived foreground must clear AA on the orange primary surface.
    const vars = computeThemeVars(lcars);
    expect(vars["--primary-foreground"]).not.toBe(lcars.text);
    expect(contrastRatio(vars["--primary"], vars["--primary-foreground"])).toBeGreaterThanOrEqual(AA);
    // Muted surface (sidebar cards, chat input) must not leave hint text stranded.
    expect(contrastRatio(vars["--muted"], vars["--muted-foreground"])).toBeGreaterThanOrEqual(AA);
  });

  it("derives a readable --link even when no theme candidate clears AA", () => {
    // Mid-gray user/header/accent fail AA on both surfaces; the derivation
    // must blend the best candidate toward the page polarity until readable.
    const colors = {
      ...builtinPalettes.tokyonight,
      user: "#808080",
      header: "#7a7a7a",
      accent: "#858585",
    };
    const vars = computeThemeVars(colors);
    expect(contrastRatio(vars["--muted"], vars["--link"])).toBeGreaterThanOrEqual(AA);
    expect(contrastRatio(vars["--background"], vars["--link"])).toBeGreaterThanOrEqual(AA);
    expect(vars["--link"]).toMatch(/^#[0-9a-f]{6}$/i);
  });

  it("prefers the theme's own link-ish color when it already reads (theme character)", () => {
    // github-dark ships #58a6ff as `user` — AA-safe on its muted/background,
    // so --link must be that exact color, not a blend or a black/white swap.
    const vars = computeThemeVars(builtinPalettes["github-dark"]);
    expect(vars["--link"]).toBe(builtinPalettes["github-dark"].user);
  });

  it("leaves already-readable mappings untouched (theme character)", () => {
    // A palette where text reads fine on the user surface and hint reads fine
    // on both the dim surface and the page background: the derivation must be
    // a no-op for those variables.
    const colors = {
      ...builtinPalettes.tokyonight,
      user: "#1a1b26", // dark surface → light text passes AA
      background: "#1a1b26",
      dim: "#1a1b26", // dark surface → hint passes AA
      hint: "#8899bb",
    };
    const vars = computeThemeVars(colors);
    expect(vars["--primary-foreground"]).toBe(colors.text);
    expect(vars["--muted"]).toBe(colors.dim);
    expect(vars["--muted-foreground"]).toBe(colors.hint);
  });
});

describe("computeThemeVars tolerates malformed palettes", () => {
  const base = builtinPalettes.tokyonight;

  it("does not throw when a link color field is missing", () => {
    // A custom/legacy palette (ThemeForm, cached JSON) can omit a field the
    // link derivation reads. hexToRgb must reject it rather than crash.
    const colors = { ...base, accent: undefined as unknown as string };
    expect(() => computeThemeVars(colors)).not.toThrow();
    const vars = computeThemeVars(colors);
    expect(vars["--link"]).toMatch(/^#[0-9a-f]{6}$/i);
  });

  it("falls back to a readable --link when no link candidate is valid", () => {
    // All three link-ish candidates invalid → linkCandidates is empty and the
    // reduce must not run; it should fall back to black/white by page polarity.
    const colors = {
      ...base,
      user: "not-a-color",
      header: "",
      accent: "#zzzzzz",
    };
    expect(() => computeThemeVars(colors)).not.toThrow();
    const vars = computeThemeVars(colors);
    expect(vars["--link"]).toMatch(/^#[0-9a-f]{6}$/i);
    expect(contrastRatio(vars["--muted"], vars["--link"])).toBeGreaterThanOrEqual(AA);
    expect(contrastRatio(vars["--background"], vars["--link"])).toBeGreaterThanOrEqual(AA);
  });
});
