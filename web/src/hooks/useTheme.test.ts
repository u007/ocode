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
