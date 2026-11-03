import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  __resetAppearanceThemeForTests,
  getColorThemeId,
  loadAppearanceTheme,
} from "../../lib/appearanceTheme";
import { type CommandContext, dispatchCommand } from "./commands";

/**
 * Regression: `/themes vscode:<id>` used to apply the legacy 16-colour mapping
 * and persist the id into `tui.theme`, so it CLAIMED success but reloaded as
 * the TUI theme (the TUI cannot resolve a `vscode:` id) and painted a
 * different palette than Settings > Theme.
 *
 * This file drives the REAL appearance store against a fake server, so the
 * reload half is an actual cache reset + startup path, not a spy assertion.
 */

const THEME_ID = "vscode:acme.themes/dark";

const server = vi.hoisted(() => {
  const state = {
    stored: "tokyonight",
    icon: "",
    dirs: [] as string[],
    id: "vscode:acme.themes/dark",
  };
  const settings = () => ({
    color_theme: state.stored,
    icon_theme: state.icon,
    extra_extension_dirs: state.dirs,
    effective_color_theme: state.stored,
  });
  return {
    state,
    settings,
    getAppearanceThemes: vi.fn(async () => ({
      themes: [
        {
          id: "tokyonight",
          label: "Tokyo Night",
          source: "builtin",
          kind: "dark",
          has_icon_theme: false,
        },
        {
          id: state.id,
          label: "Acme Dark",
          source: "vscode",
          kind: "dark",
          has_icon_theme: false,
        },
      ],
      total: 2,
      offset: 0,
      limit: 2,
      truncated: false,
    })),
    getAppearanceSettings: vi.fn(async () => settings()),
    setAppearanceSettings: vi.fn(async (patch: Record<string, unknown>) => {
      if (typeof patch.color_theme === "string") state.stored = patch.color_theme;
      if (typeof patch.icon_theme === "string") state.icon = patch.icon_theme;
      if (Array.isArray(patch.extra_extension_dirs)) {
        state.dirs = patch.extra_extension_dirs as string[];
      }
      return settings();
    }),
    getAppearanceTheme: vi.fn(async (id: string) => ({
      id,
      label: "Acme Dark",
      kind: "dark" as const,
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
      ui: { "--background": "#1e1e2e" },
      monaco: { base: "vs-dark" as const, colors: {}, rules: [] },
      terminal: {},
    })),
    getTheme: vi.fn(async (name?: string) => ({
      name: name ?? "tokyonight",
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
    })),
  };
});

vi.mock("../../api/client", () => ({ api: server }));

function commandContext(): CommandContext {
  return {
    commandName: "/themes",
    args: "",
    api: {
      getAppearanceThemes: server.getAppearanceThemes,
      getAppearanceSettings: server.getAppearanceSettings,
    },
  } as unknown as CommandContext;
}

describe("/themes <vscode:id> survives a reload", () => {
  beforeEach(() => {
    __resetAppearanceThemeForTests();
    document.documentElement.removeAttribute("style");
    server.state.stored = "tokyonight";
    server.state.icon = "";
    server.state.dirs = [];
    vi.clearAllMocks();
  });

  it("persists to appearance.color_theme and re-applies it after a simulated reload", async () => {
    // A fresh page load: the store resolves the stored legacy theme first.
    await loadAppearanceTheme();
    expect(getColorThemeId()).toBe("tokyonight");
    expect(server.getAppearanceTheme).not.toHaveBeenCalled();

    const result = await dispatchCommand(`/themes ${THEME_ID}`, commandContext());
    expect(result.messages?.[0]?.content).toContain("Acme Dark");
    expect(server.setAppearanceSettings).toHaveBeenCalledWith(
      expect.objectContaining({ color_theme: THEME_ID }),
    );
    // Persisted BEFORE the apply, so a rejected write never claims the theme.
    expect(server.state.stored).toBe(THEME_ID);

    // "Reload": every cache is dropped, exactly as a fresh document would.
    __resetAppearanceThemeForTests();
    document.documentElement.removeAttribute("style");
    await loadAppearanceTheme();

    expect(server.getAppearanceTheme).toHaveBeenCalledWith(THEME_ID);
    expect(getColorThemeId()).toBe(THEME_ID);
    expect(document.documentElement.style.getPropertyValue("--background")).toBe(
      "240 21.1% 14.9%",
    );
  });
});
