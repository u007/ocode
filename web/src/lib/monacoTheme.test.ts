import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ConvertedTheme } from "../api/types";
import {
  VSCODE_MONACO_THEME_PREFIX,
  applyMonacoTheme,
  monacoThemeChoiceBlocker,
  resolveEditorTheme,
  resolvedThemePolarity,
  __resetMonacoThemeForTests,
} from "./monacoTheme";

function converted(overrides: Partial<ConvertedTheme> = {}): ConvertedTheme {
  return {
    id: "tokyonight",
    label: "Tokyonight",
    kind: "dark",
    colors: {
      user: "#7aa2f7",
      assistant: "#a9b1d6",
      header: "#7aa2f7",
      border: "#3b4261",
      hint: "#565f89",
      text: "#c0caf5",
      background: "#1a1b26",
      status_bg: "#16161e",
      status_fg: "#c0caf5",
      selected_fg: "#c0caf5",
      selected_bg: "#283457",
      success: "#9ece6a",
      error: "#f7768e",
      accent: "#bb9af7",
      dim: "#16161e",
      thinking: "#565f89",
    },
    ui: {},
    monaco: {
      base: "vs-dark",
      colors: { "editor.background": "#1a1b26" },
      rules: [{ token: "comment", foreground: "#565f89", fontStyle: "italic" }],
    },
    terminal: {},
    ...overrides,
  };
}

const BUILTIN = converted();
const VSCODE_DARK = converted({
  id: "vscode:acme.themes/dark",
  label: "Acme Dark",
  monaco: {
    base: "vs-dark",
    colors: { "editor.background": "#101010" },
    rules: [{ token: "keyword", foreground: "#ff0000" }],
  },
});
const VSCODE_LIGHT = converted({
  id: "vscode:acme.themes/light",
  label: "Acme Light",
  kind: "light",
  monaco: {
    base: "vs",
    colors: { "editor.background": "#fefefe" },
    rules: [{ token: "string", foreground: "#00aa00" }],
  },
});

describe("resolveEditorTheme — §8.6 precedence", () => {
  it.each([
    ["light", VSCODE_DARK, "ocode-dark", "ocode-light"],
    ["dark", VSCODE_LIGHT, "vs", "ocode-dark"],
  ] as const)(
    "case 1: an explicit %s override wins over a VS Code theme",
    (appearance, theme, monacoTheme, expected) => {
      expect(resolveEditorTheme({ appearance, theme, monacoTheme })).toEqual({
        id: expected,
        define: null,
      });
    },
  );

  it("case 2: a VS Code theme contributes its converted Monaco palette", () => {
    const resolved = resolveEditorTheme({
      appearance: "auto",
      theme: VSCODE_DARK,
      monacoTheme: "vs",
    });
    expect(resolved.id).toBe(`${VSCODE_MONACO_THEME_PREFIX}acme.themes/dark`);
    expect(resolved.define).toEqual(VSCODE_DARK.monaco);
  });

  it("case 2: a light VS Code theme keeps its base and rules", () => {
    const resolved = resolveEditorTheme({
      appearance: "auto",
      theme: VSCODE_LIGHT,
      monacoTheme: "ocode-dark",
    });
    expect(resolved.id).toBe(`${VSCODE_MONACO_THEME_PREFIX}acme.themes/light`);
    expect(resolved.define).toEqual(VSCODE_LIGHT.monaco);
  });

  it.each([
    ["vs-dark", "vs-dark"],
    ["vs", "vs"],
    ["hc-black", "hc-black"],
    ["ocode-light", "ocode-light"],
  ])("case 3: a builtin theme uses a defined monaco.theme (%s)", (monacoTheme, expected) => {
    expect(resolveEditorTheme({ appearance: "auto", theme: BUILTIN, monacoTheme })).toEqual({
      id: expected,
      define: null,
    });
  });

  it("case 3: an unknown monaco.theme falls back to the theme's polarity", () => {
    expect(
      resolveEditorTheme({ appearance: "auto", theme: BUILTIN, monacoTheme: "not-a-theme" }),
    ).toEqual({ id: "ocode-dark", define: null });
    const light = converted({ kind: "light" });
    expect(
      resolveEditorTheme({ appearance: "auto", theme: light, monacoTheme: "" }),
    ).toEqual({ id: "ocode-light", define: null });
  });

  it("case 3: a defined monaco.theme is honoured with NO loaded theme", () => {
    // The appearance fetch is pending, offline or 401'd. Before this rule a
    // defined `monaco.theme` was dropped in that state, silently discarding the
    // user's editor palette; it was honoured unconditionally before §8.6.
    expect(resolveEditorTheme({ appearance: "auto", theme: null, monacoTheme: "vs" })).toEqual({
      id: "vs",
      define: null,
    });
    expect(
      resolveEditorTheme({ appearance: "auto", theme: null, monacoTheme: "ocode-dark" }),
    ).toEqual({ id: "ocode-dark", define: null });
  });

  it("case 2: a VS Code theme outranks a defined monaco.theme", () => {
    expect(
      resolveEditorTheme({ appearance: "auto", theme: VSCODE_LIGHT, monacoTheme: "vs-dark" }).id,
    ).toBe(`${VSCODE_MONACO_THEME_PREFIX}acme.themes/light`);
  });

  it("case 5: no loaded theme and no defined monaco.theme falls back to ocode-dark", () => {
    expect(resolveEditorTheme({ appearance: "auto", theme: null, monacoTheme: "" })).toEqual({
      id: "ocode-dark",
      define: null,
    });
    expect(
      resolveEditorTheme({ appearance: "auto", theme: null, monacoTheme: "not-a-theme" }),
    ).toEqual({ id: "ocode-dark", define: null });
  });

  it("does not coerce a light monaco.theme when the override is auto", () => {
    // The old FileEditor coerced `vs` to `ocode-dark`; that silent coercion is
    // the defect §8.6 removes.
    expect(
      resolveEditorTheme({ appearance: "auto", theme: BUILTIN, monacoTheme: "vs" }).id,
    ).toBe("vs");
  });
});

describe("applyMonacoTheme", () => {
  const defineTheme = vi.fn();
  const setTheme = vi.fn();
  const monaco = { editor: { defineTheme, setTheme } };

  beforeEach(() => {
    __resetMonacoThemeForTests();
    defineTheme.mockClear();
    setTheme.mockClear();
  });

  it("defines a VS Code theme once per id and always selects it", () => {
    const resolved = resolveEditorTheme({
      appearance: "auto",
      theme: VSCODE_DARK,
      monacoTheme: "ocode-dark",
    });

    applyMonacoTheme(resolved, monaco);
    applyMonacoTheme(resolved, monaco);
    applyMonacoTheme(resolved, monaco);

    expect(defineTheme).toHaveBeenCalledTimes(1);
    expect(defineTheme).toHaveBeenCalledWith(`${VSCODE_MONACO_THEME_PREFIX}acme.themes/dark`, {
      base: "vs-dark",
      inherit: true,
      rules: VSCODE_DARK.monaco.rules,
      colors: VSCODE_DARK.monaco.colors,
    });
    expect(setTheme).toHaveBeenCalledTimes(3);
    expect(setTheme).toHaveBeenLastCalledWith(`${VSCODE_MONACO_THEME_PREFIX}acme.themes/dark`);
  });

  it("defines each VS Code theme id once and re-defines a different one", () => {
    applyMonacoTheme(
      resolveEditorTheme({ appearance: "auto", theme: VSCODE_DARK, monacoTheme: "" }),
      monaco,
    );
    applyMonacoTheme(
      resolveEditorTheme({ appearance: "auto", theme: VSCODE_LIGHT, monacoTheme: "" }),
      monaco,
    );
    applyMonacoTheme(
      resolveEditorTheme({ appearance: "auto", theme: VSCODE_DARK, monacoTheme: "" }),
      monaco,
    );

    expect(defineTheme).toHaveBeenCalledTimes(2);
  });

  it("never re-defines the custom pair or a Monaco built-in", () => {
    for (const appearance of ["light", "dark"] as const) {
      applyMonacoTheme(
        resolveEditorTheme({ appearance, theme: VSCODE_DARK, monacoTheme: "" }),
        monaco,
      );
    }
    applyMonacoTheme(
      resolveEditorTheme({ appearance: "auto", theme: BUILTIN, monacoTheme: "vs-dark" }),
      monaco,
    );
    expect(defineTheme).not.toHaveBeenCalled();
    expect(setTheme.mock.calls.map(([id]) => id)).toEqual([
      "ocode-light",
      "ocode-dark",
      "vs-dark",
    ]);
  });

  it("normalizes a missing or foreign base to the theme polarity", () => {
    const missingBase = converted({ id: "vscode:acme.themes/odd", monaco: { base: "", colors: {}, rules: [] } });
    applyMonacoTheme(
      resolveEditorTheme({ appearance: "auto", theme: missingBase, monacoTheme: "" }),
      monaco,
    );
    expect(defineTheme.mock.calls[0][1]).toMatchObject({ base: "vs-dark", inherit: true });
  });

  it("normalizes a LIGHT theme's missing or foreign base to vs", () => {
    // The dark half above would still pass if the kind check were deleted and
    // every fallback hardcoded "vs-dark" — a light theme would then define a
    // dark base over light colours.
    for (const base of ["", "monokai"]) {
      __resetMonacoThemeForTests();
      defineTheme.mockClear();
      const light = converted({
        id: `vscode:acme.themes/light-${base || "missing"}`,
        kind: "light",
        monaco: { base, colors: {}, rules: [] },
      });
      applyMonacoTheme(
        resolveEditorTheme({ appearance: "auto", theme: light, monacoTheme: "" }),
        monaco,
      );
      expect(defineTheme.mock.calls[0][1]).toMatchObject({ base: "vs", inherit: true });
    }
  });
});

describe("resolvedThemePolarity", () => {
  it.each([
    ["ocode-light", "light"],
    ["vs", "light"],
    ["hc-light", "light"],
    ["ocode-dark", "dark"],
    ["vs-dark", "dark"],
    ["hc-black", "dark"],
  ])("reports %s as %s", (id, expected) => {
    expect(resolvedThemePolarity({ id, define: null })).toBe(expected);
  });

  it("follows a converted theme's normalized base", () => {
    expect(
      resolvedThemePolarity(
        resolveEditorTheme({ appearance: "auto", theme: VSCODE_LIGHT, monacoTheme: "" }),
      ),
    ).toBe("light");
    expect(
      resolvedThemePolarity(
        resolveEditorTheme({ appearance: "auto", theme: VSCODE_DARK, monacoTheme: "" }),
      ),
    ).toBe("dark");
  });

  it("reports a light monaco.theme under a dark colour theme as light", () => {
    // The toggle must describe the EDITOR, not the colour theme: `vs` paints a
    // light editor even though the theme is dark.
    expect(
      resolvedThemePolarity(
        resolveEditorTheme({ appearance: "auto", theme: BUILTIN, monacoTheme: "vs" }),
      ),
    ).toBe("light");
    expect(
      resolvedThemePolarity(
        resolveEditorTheme({ appearance: "light", theme: BUILTIN, monacoTheme: "vs-dark" }),
      ),
    ).toBe("light");
  });
});

describe("monacoThemeChoiceBlocker", () => {
  it("is null when monaco.theme actually decides the palette", () => {
    expect(monacoThemeChoiceBlocker({ appearance: "auto", theme: BUILTIN })).toBeNull();
    expect(monacoThemeChoiceBlocker({ appearance: "auto", theme: null })).toBeNull();
  });

  it("reports the editor override and the VS Code theme, override first", () => {
    expect(monacoThemeChoiceBlocker({ appearance: "light", theme: BUILTIN })).toBe(
      "editor-appearance",
    );
    expect(monacoThemeChoiceBlocker({ appearance: "dark", theme: null })).toBe("editor-appearance");
    expect(monacoThemeChoiceBlocker({ appearance: "auto", theme: VSCODE_DARK })).toBe(
      "vscode-theme",
    );
    expect(monacoThemeChoiceBlocker({ appearance: "dark", theme: VSCODE_LIGHT })).toBe(
      "editor-appearance",
    );
  });
});
