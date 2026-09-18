import { describe, expect, it, vi } from "vitest";
import { dispatchCommand } from "./commands";

/**
 * /autocontinue regression suite.
 *
 * The command reads/writes GET|PUT /api/config/ocode/autocontinue through
 * ctx.api (injected). The no-arg `/autocontinue model` opens the interactive
 * judge-model picker (web counterpart of the TUI's no-arg `/autocontinue
 * model`, which opens kind="autocontinue-model") — previously the web UI
 * answered "The interactive picker is TUI-only", so `/autocontinue model` had
 * no picker at all.
 */
function ctx(opts: { getAutoContinue?: () => Promise<{ enabled: boolean; model: string }>; setAutoContinue?: (fields: { enabled?: boolean; model?: string; clear?: boolean }) => Promise<{ enabled: boolean; model: string }> } = {}) {
  const getAutoContinue =
    opts.getAutoContinue ?? vi.fn(async () => ({ enabled: false, model: "" }));
  const setAutoContinue =
    opts.setAutoContinue ?? vi.fn(async (_fields: { enabled?: boolean; model?: string; clear?: boolean }) => ({
      enabled: false,
      model: "",
    }));
  return {
    ctx: {
      commandName: "autocontinue",
      args: "",
      api: { getAutoContinue, setAutoContinue },
    } as never,
    getAutoContinue,
    setAutoContinue,
  };
}

describe("/autocontinue command", () => {
  it("no-arg /autocontinue model returns the picker-open result for the autocontinue purpose", async () => {
    const { ctx: c, setAutoContinue } = ctx();
    const result = await dispatchCommand("/autocontinue model", c);
    expect(result.handled).toBe(true);
    expect(result.openModelPicker).toBe(true);
    expect(result.modelPickerPurpose).toBe("autocontinue");
    // No write happened — the dialog owns persistence once a model is picked.
    expect(setAutoContinue).not.toHaveBeenCalled();
  });

  it("status reports enabled state and judge model", async () => {
    const { ctx: c, getAutoContinue } = ctx({
      getAutoContinue: vi.fn(async () => ({ enabled: true, model: "local/bonsai-8b" })),
    });
    const result = await dispatchCommand("/autocontinue", c);
    expect(result.handled).toBe(true);
    expect(result.messages?.[0]?.content).toContain("enabled");
    expect(result.messages?.[0]?.content).toContain("local/bonsai-8b");
    expect(getAutoContinue).toHaveBeenCalled();
  });

  it("on/off write only the gate; `model auto` clears the judge model only", async () => {
    const { ctx: onCtx, setAutoContinue: setOn } = ctx();
    await dispatchCommand("/autocontinue on", onCtx);
    expect(setOn).toHaveBeenCalledWith({ enabled: true });

    const { ctx: offCtx, setAutoContinue: setOff } = ctx();
    await dispatchCommand("/autocontinue off", offCtx);
    expect(setOff).toHaveBeenCalledWith({ enabled: false });

    const { ctx: clearCtx, setAutoContinue: setClear } = ctx();
    await dispatchCommand("/autocontinue model auto", clearCtx);
    expect(setClear).toHaveBeenCalledWith({ clear: true });
  });

  // Setting a judge model does not arm auto-continue. The reply must say the
  // gate is still off, or the user configures `typesafe/jev-latest`, sees
  // "Judge model: …", and assumes it is running — the "via jev does not work"
  // report (the gate was left disabled).
  it("model <name> warns that the enable gate is still off", async () => {
    const { ctx: c } = ctx({
      setAutoContinue: vi.fn(async () => ({ enabled: false, model: "typesafe/jev-latest" })),
    });
    const result = await dispatchCommand("/autocontinue model typesafe/jev-latest", c);
    expect(result.messages?.[0]?.content).toContain("typesafe/jev-latest");
    expect(result.messages?.[0]?.content).toContain("DISABLED");
  });

  it("model <name> does not warn when the gate is already enabled", async () => {
    const { ctx: c } = ctx({
      setAutoContinue: vi.fn(async () => ({ enabled: true, model: "typesafe/jev-latest" })),
    });
    const result = await dispatchCommand("/autocontinue model typesafe/jev-latest", c);
    expect(result.messages?.[0]?.content).toContain("typesafe/jev-latest");
    expect(result.messages?.[0]?.content).not.toContain("DISABLED");
  });
});