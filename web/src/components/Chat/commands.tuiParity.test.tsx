import { describe, expect, it, vi } from "vitest";
import { COMMANDS, dispatchCommand } from "./commands";

/**
 * TUI-parity slash-command suite.
 *
 * The user-reported bug: `/fake-agent status` typed in the web/desktop UI
 * silently fell through to the LLM as a chat message (the command existed in
 * internal/tui/commands.go but neither the COMAMNDS picker nor dispatchCommand
 * knew about it). This suite locks both halves:
 *   1. every picker entry is actually dispatched (no fall-through), and
 *   2. the newly-added handlers persist through the same endpoints the
 *      Settings forms use.
 */
function ctx(api: Record<string, unknown> = {}) {
  return { commandName: "test", args: "", api } as never;
}

describe("COMMANDS registry ⇄ dispatch alignment", () => {
  // /new and /clear are intercepted in App.tsx before delegating to
  // dispatchCommand (they open a fresh session tab), so they legitimately
  // report handled:false here.
  const APP_HANDLED = new Set(["/new", "/clear"]);

  it("dispatches every picker entry instead of falling through to the LLM", async () => {
    const fallThrough: string[] = [];
    for (const c of COMMANDS) {
      if (APP_HANDLED.has(c.name)) continue;
      const result = await dispatchCommand(c.name, ctx());
      if (!result.handled) fallThrough.push(c.name);
    }
    expect(fallThrough).toEqual([]);
  });
});

describe("/fake-agent command", () => {
  it("reports the active harness and the available options", async () => {
    const result = await dispatchCommand(
      "/fake-agent status",
      ctx({
        getFakeAgent: vi.fn(async () => ({
          fake_agent: "ocode",
          active: "ocode",
          options: ["ocode", "opencode", "claude-code", "cline", "kilo-code", "codex"],
        })),
      }),
    );
    expect(result.handled).toBe(true);
    const content = result.messages?.[0]?.content ?? "";
    expect(content).toContain("Harness identity");
    expect(content).toContain("ocode");
    expect(content).toContain("claude-code");
  });

  it("switches the harness and reports the new active identity", async () => {
    const setFakeAgent = vi.fn(async (name: string) => ({
      fake_agent: name,
      active: "claude-code",
      options: ["ocode", "claude-code"],
    }));
    const result = await dispatchCommand(
      "/fake-agent claude-code",
      ctx({
        getFakeAgent: vi.fn(async () => ({ fake_agent: "ocode", active: "ocode", options: [] })),
        setFakeAgent,
      }),
    );
    expect(setFakeAgent).toHaveBeenCalledWith("claude-code");
    expect(result.messages?.[0]?.content).toContain("claude-code");
  });

  it("bare /fake-agent behaves like status", async () => {
    const getFakeAgent = vi.fn(async () => ({ fake_agent: "cline", active: "cline", options: [] }));
    const result = await dispatchCommand("/fake-agent", ctx({ getFakeAgent }));
    expect(getFakeAgent).toHaveBeenCalled();
    expect(result.messages?.[0]?.content).toContain("cline");
  });
});

describe("/explorer-model and /context-model", () => {
  it.each([
    ["explorer", "getExplorerModel", "setExplorerModel", "setExplorerModelEnabled"],
    ["context", "getContextModel", "setContextModel", "setContextModelEnabled"],
  ])("%s status reports model + enabled + fallback", async (kind, getKey, _setKey, _enKey) => {
    const result = await dispatchCommand(
      `/${kind}-model status`,
      ctx({ [getKey]: vi.fn(async () => ({ model: "", enabled: false })) }),
    );
    expect(result.handled).toBe(true);
    const content = result.messages?.[0]?.content ?? "";
    expect(content).toContain("Model");
    expect(content).toContain("disabled");
    expect(content).toContain("small model");
  });

  it.each([
    ["explorer", "getExplorerModel", "setExplorerModel", "setExplorerModelEnabled"],
    ["context", "getContextModel", "setContextModel", "setContextModelEnabled"],
  ])("%s enable/disable writes only the gate", async (kind, getKey, _setKey, enKey) => {
    const setEnabled = vi.fn(async (enabled: boolean) => ({ model: "x/y", enabled }));
    const c = ctx({
      [getKey]: vi.fn(async () => ({ model: "x/y", enabled: false })),
      [enKey]: setEnabled,
    });
    const on = await dispatchCommand(`/${kind}-model enable`, c);
    expect(setEnabled).toHaveBeenCalledWith(true);
    expect(on.messages?.[0]?.content).toContain("enabled");

    const off = await dispatchCommand(`/${kind}-model disable`, c);
    expect(setEnabled).toHaveBeenCalledWith(false);
    expect(off.messages?.[0]?.content).toContain("disabled");
  });

  it("no-arg `model` opens the picker; explicit `model <id>` writes directly", async () => {
    const setExplorerModel = vi.fn(async (model: string) => ({ model, enabled: true }));
    const c = ctx({
      getExplorerModel: vi.fn(async () => ({ model: "", enabled: true })),
      setExplorerModel,
      setExplorerModelEnabled: vi.fn(),
    });

    const picker = await dispatchCommand("/explorer-model model", c);
    expect(picker.openModelPicker).toBe(true);
    expect(picker.modelPickerPurpose).toBe("explorer");
    expect(setExplorerModel).not.toHaveBeenCalled();

    await dispatchCommand("/explorer-model model openai/gpt-5", c);
    expect(setExplorerModel).toHaveBeenCalledWith("openai/gpt-5");
  });

  it("`model auto` clears the override", async () => {
    const setContextModel = vi.fn(async (_model: string) => ({ model: "", enabled: true }));
    const result = await dispatchCommand(
      "/context-model model auto",
      ctx({
        getContextModel: vi.fn(async () => ({ model: "x/y", enabled: true })),
        setContextModel,
        setContextModelEnabled: vi.fn(),
      }),
    );
    expect(setContextModel).toHaveBeenCalledWith("auto");
    expect(result.messages?.[0]?.content).toContain("small model");
  });
});

describe("/editor and /editor-mode", () => {
  it("reports the current editor", async () => {
    const result = await dispatchCommand(
      "/editor",
      ctx({ getEditorConfig: vi.fn(async () => ({ editor: "nvim", editor_mode: "external", ide_mode: "off" })) }),
    );
    expect(result.messages?.[0]?.content).toContain("nvim");
  });

  it("sets the editor while preserving the other fields", async () => {
    const setEditorConfig = vi.fn(async (editor: string, editorMode: string, ideMode: string) => ({
      editor,
      editor_mode: editorMode,
      ide_mode: ideMode,
    }));
    await dispatchCommand(
      "/editor code",
      ctx({
        getEditorConfig: vi.fn(async () => ({ editor: "vim", editor_mode: "tmux-split", ide_mode: "claude" })),
        setEditorConfig,
      }),
    );
    expect(setEditorConfig).toHaveBeenCalledWith("code", "tmux-split", "claude");
  });

  it("rejects an unknown editor mode and accepts a valid one", async () => {
    const setEditorConfig = vi.fn(async (editor: string, editorMode: string, ideMode: string) => ({
      editor,
      editor_mode: editorMode,
      ide_mode: ideMode,
    }));
    const c = ctx({
      getEditorConfig: vi.fn(async () => ({ editor: "code", editor_mode: "external", ide_mode: "off" })),
      setEditorConfig,
    });

    const bad = await dispatchCommand("/editor-mode bogus", c);
    expect(setEditorConfig).not.toHaveBeenCalled();
    expect(bad.messages?.[0]?.content).toContain("Unknown editor mode");

    await dispatchCommand("/editor-mode tmux-window", c);
    expect(setEditorConfig).toHaveBeenCalledWith("code", "tmux-window", "off");
  });
});

describe("/themes and /search", () => {
  it("lists themes and marks the current one", async () => {
    const result = await dispatchCommand(
      "/themes",
      ctx({
        getThemes: vi.fn(async () => ({
          current: "tokyonight",
          themes: [
            { name: "tokyonight", label: "Tokyo Night" },
            { name: "github-dark", label: "GitHub Dark" },
          ],
        })),
        getTheme: vi.fn(),
        getTUISettings: vi.fn(),
        setTUISettings: vi.fn(),
      }),
    );
    const content = result.messages?.[0]?.content ?? "";
    expect(content).toContain("tokyonight");
    expect(content).toContain("GitHub Dark");
    expect(content).toContain("(current)");
  });

  it("applies and persists a named theme", async () => {
    const setTUISettings = vi.fn(async (cfg: unknown) => cfg);
    const result = await dispatchCommand(
      "/themes github-dark",
      ctx({
        getThemes: vi.fn(async () => ({
          current: "tokyonight",
          themes: [{ name: "github-dark", label: "GitHub Dark" }],
        })),
        getTheme: vi.fn(async () => ({
          name: "github-dark",
          colors: {
            user: "#111111", assistant: "#222222", header: "#333333", border: "#444444",
            hint: "#555555", text: "#eeeeee", background: "#000000", status_bg: "#111111",
            status_fg: "#ffffff", selected_fg: "#ffffff", selected_bg: "#222222",
            success: "#22c55e", error: "#ef4444", accent: "#3b82f6", dim: "#666666",
            thinking: "#888888",
          },
        })),
        getTUISettings: vi.fn(async () => ({
          theme: "tokyonight", mouse: null, scroll_speed: 0, keybinds: {}, leader_timeout: 0, branchless: false,
        })),
        setTUISettings,
      }),
    );
    expect(setTUISettings).toHaveBeenCalledWith(
      expect.objectContaining({ theme: "github-dark" }),
    );
    expect(result.messages?.[0]?.content).toContain("GitHub Dark");
  });

  it("rejects an unknown theme without persisting", async () => {
    const setTUISettings = vi.fn();
    const result = await dispatchCommand(
      "/themes nope",
      ctx({
        getThemes: vi.fn(async () => ({ current: "tokyonight", themes: [{ name: "tokyonight", label: "Tokyo Night" }] })),
        getTheme: vi.fn(),
        getTUISettings: vi.fn(),
        setTUISettings,
      }),
    );
    expect(setTUISettings).not.toHaveBeenCalled();
    expect(result.messages?.[0]?.content).toContain("Unknown theme");
  });

  it("/search and /find open the in-chat find bar with the query", async () => {
    const seen: string[] = [];
    const handler = (e: Event) => seen.push((e as CustomEvent<{ query: string }>).detail.query);
    window.addEventListener("ocode:open-chat-search", handler);
    try {
      const a = await dispatchCommand("/search retry logic", ctx());
      expect(a.handled).toBe(true);
      const b = await dispatchCommand("/find retry logic", ctx());
      expect(b.handled).toBe(true);
      expect(seen).toEqual(["retry logic", "retry logic"]);
    } finally {
      window.removeEventListener("ocode:open-chat-search", handler);
    }
  });
});

describe("TUI-only commands answer instead of falling through", () => {
  it.each(["/ide", "/sidebar", "/details", "/sound", "/rc", "/exit"])(
    "%s is handled with an explanatory message",
    async (name) => {
      const result = await dispatchCommand(name, ctx());
      expect(result.handled).toBe(true);
      const content = result.messages?.[0]?.content ?? "";
      expect(content).toContain(name);
      expect(content).toContain("not available in the web/desktop UI");
    },
  );

  it("/secret points at the Files context menu", async () => {
    const result = await dispatchCommand("/secret status", ctx());
    expect(result.handled).toBe(true);
    expect(result.messages?.[0]?.content).toContain("Files");
  });

  it("/mcp-auth points at the TUI and never claims the desktop shell can run it", async () => {
    const result = await dispatchCommand("/mcp-auth some-server", ctx());
    expect(result.handled).toBe(true);
    const content = result.messages?.[0]?.content ?? "";
    expect(content).toContain("TUI");
    // The desktop app renders this same SPA, so "run it in the desktop shell"
    // sent users in a circle.
    expect(content).not.toContain("desktop shell");
  });
});

describe("/tools command", () => {
  const status = {
    platform: "darwin",
    package_manager: "brew",
    tools: [
      { name: "rg", description: "ripgrep", found: true, command: "rg" },
      { name: "fzf", description: "fuzzy finder", found: false },
    ],
  };

  it("bare /tools lists every tool with its install state", async () => {
    const getCliTools = vi.fn(async () => status);
    const result = await dispatchCommand("/tools", ctx({ getCliTools }));
    expect(getCliTools).toHaveBeenCalled();
    const content = result.messages?.[0]?.content ?? "";
    expect(content).toContain("rg");
    expect(content).toContain("fzf");
    expect(content).toContain("/tools fzf");
  });

  it("reports a tool found under an alias", async () => {
    const result = await dispatchCommand(
      "/tools",
      ctx({
        getCliTools: vi.fn(async () => ({
          platform: "linux",
          package_manager: "apt",
          tools: [{ name: "fd", description: "fd-find", found: true, command: "fdfind" }],
        })),
      }),
    );
    expect(result.messages?.[0]?.content).toContain("found as `fdfind`");
  });

  it("shows the package-manager hint when none is available", async () => {
    const result = await dispatchCommand(
      "/tools",
      ctx({
        getCliTools: vi.fn(async () => ({
          platform: "linux",
          package_manager: "",
          manager_hint: "No supported package manager found on PATH",
          tools: [],
        })),
      }),
    );
    expect(result.messages?.[0]?.content).toContain("No supported package manager");
  });

  it("routes the status probe to the session's remote host", async () => {
    const getCliTools = vi.fn(async (_host?: string) => status);
    const c = ctx({ getCliTools }) as { host?: string };
    c.host = "james@10.0.0.5";
    await dispatchCommand("/tools", c as never);
    expect(getCliTools).toHaveBeenCalledWith("james@10.0.0.5");
  });

  it("/tools <name> starts a background install and reports the outcome via notify", async () => {
    vi.useFakeTimers();
    try {
      const startCliToolsInstall = vi.fn(async (tool: string) => ({
        job_id: "job-1",
        tool,
        status: "running" as const,
      }));
      const getCliToolsInstallStatus = vi
        .fn()
        .mockResolvedValueOnce({ job_id: "job-1", tool: "rg", status: "running" })
        .mockResolvedValue({ job_id: "job-1", tool: "rg", status: "done", manager: "brew", output: "$ brew install ripgrep" });
      const notify = vi.fn();
      const result = await dispatchCommand("/tools rg", {
        commandName: "test",
        args: "",
        api: { startCliToolsInstall, getCliToolsInstallStatus },
        notify,
      } as never);
      // The handler must return immediately — a package-manager run can take
      // minutes and the composer is blocked for the whole await.
      expect(result.messages?.[0]?.content).toContain("background");
      expect(notify).not.toHaveBeenCalled();
      expect(startCliToolsInstall).toHaveBeenCalledWith("rg", undefined);

      // Let the detached poller run to completion.
      await vi.advanceTimersByTimeAsync(10_000);
      expect(notify).toHaveBeenCalledTimes(1);
      const msg = notify.mock.calls[0][0] as string;
      expect(msg).toContain("rg");
      expect(msg).toContain("brew");
    } finally {
      vi.useRealTimers();
    }
  });

  it("/tools <name> reports an install failure with the manager hint", async () => {
    vi.useFakeTimers();
    try {
      const notify = vi.fn();
      await dispatchCommand("/tools rg", {
        commandName: "test",
        args: "",
        api: {
          startCliToolsInstall: vi.fn(async (tool: string) => ({ job_id: "j", tool, status: "running" })),
          getCliToolsInstallStatus: vi.fn(async () => ({
            job_id: "j",
            tool: "rg",
            status: "error",
            error: "no available manager could install rg",
            no_manager: true,
            hint: "Install one, then retry",
          })),
        },
        notify,
      } as never);
      await vi.advanceTimersByTimeAsync(10_000);
      expect(notify).toHaveBeenCalledTimes(1);
      const msg = notify.mock.calls[0][0] as string;
      expect(msg).toContain("failed");
      expect(msg).toContain("Install one, then retry");
    } finally {
      vi.useRealTimers();
    }
  });

  it("surfaces a rejected install request as a command failure", async () => {
    const result = await dispatchCommand(
      "/tools not-a-tool",
      ctx({
        startCliToolsInstall: vi.fn(async () => {
          throw new Error("unknown tool");
        }),
      }),
    );
    expect(result.handled).toBe(true);
    expect(result.messages?.[0]?.content).toContain("unknown tool");
  });
});
