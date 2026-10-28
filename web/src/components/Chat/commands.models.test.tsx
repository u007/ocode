import { describe, expect, it, vi } from "vitest";
import { dispatchCommand } from "./commands";

/**
 * /models <name> regression suite.
 *
 * The web/desktop sidebar picker scopes a main-model pick to the chat (PUT
 * /api/sessions/:id/model). The slash command used to write the GLOBAL config
 * model instead ("Applies to this and new sessions"), which retroactively moved
 * every chat that had no override — the "switch chat and it runs the last
 * switched model" report. A real session must now be scoped; only a draft
 * (`new-*`) tab or no tab at all moves the default new chats start with.
 *
 * The rows mirror GET /api/models exactly: `name` is the canonical
 * provider-qualified id the setters accept, `model` is the bare model name, and
 * `display_name` is the human label.
 */
type ModelRow = {
  name: string;
  model: string;
  provider: string;
  active: boolean;
  display_name?: string;
};

function makeCtx(opts: {
  sessionId?: string | null;
  models?: ModelRow[];
  withSessionSetter?: boolean;
} = {}) {
  const setConfigModel = vi.fn(async (_model: string, _host?: string) => ({}));
  const setSessionModel = vi.fn(async (_id: string, _model: string, _host?: string) => ({}));
  const listModels = vi.fn(async (_host?: string) => opts.models ?? []);
  const api: Record<string, unknown> = { setConfigModel, listModels };
  if (opts.withSessionSetter !== false) api.setSessionModel = setSessionModel;
  const ctx = {
    commandName: "models",
    args: "",
    host: "devbox",
    api,
    getSessionId: () => opts.sessionId ?? null,
  } as never;
  return { ctx, setConfigModel, setSessionModel, listModels };
}

const MODELS: ModelRow[] = [
  {
    name: "opencode-go/deepseek-v4.1-flash",
    model: "deepseek-v4.1-flash",
    provider: "opencode-go",
    active: false,
    display_name: "DeepSeek V4.1 Flash",
  },
  {
    name: "anthropic/claude-sonnet-5-5",
    model: "claude-sonnet-5-5",
    provider: "anthropic",
    active: false,
    display_name: "Sonnet 5.5",
  },
];

describe("/models <name>", () => {
  it("scopes a pick in a real chat to that session, using the canonical id", async () => {
    const { ctx, setConfigModel, setSessionModel } = makeCtx({
      sessionId: "ses_123",
      models: MODELS,
    });
    // Picked by DISPLAY name: the canonical provider/id must reach the setter,
    // not the human label and not the bare model name (which the server 400s).
    const result = await dispatchCommand("/models DeepSeek V4.1 Flash", ctx);
    expect(result.handled).toBe(true);
    expect(setSessionModel).toHaveBeenCalledWith(
      "ses_123",
      "opencode-go/deepseek-v4.1-flash",
      "devbox",
    );
    expect(setConfigModel).not.toHaveBeenCalled();
    expect(result.messages?.[0]?.content).toContain("this chat");
  });

  it("moves the default from a draft tab (new-*), never the session", async () => {
    const { ctx, setConfigModel, setSessionModel } = makeCtx({
      sessionId: "new-1793000000000",
      models: MODELS,
    });
    const result = await dispatchCommand("/models Sonnet 5.5", ctx);
    expect(setSessionModel).not.toHaveBeenCalled();
    expect(setConfigModel).toHaveBeenCalledWith("anthropic/claude-sonnet-5-5", "devbox");
    expect(result.messages?.[0]?.content).toContain("new chats");
  });

  it("falls back to the default when no tab is in context or the setter is absent", async () => {
    const noTab = makeCtx({ sessionId: null, models: MODELS });
    await dispatchCommand("/models Sonnet 5.5", noTab.ctx);
    expect(noTab.setConfigModel).toHaveBeenCalledWith("anthropic/claude-sonnet-5-5", "devbox");

    const noSetter = makeCtx({ sessionId: "ses_123", models: MODELS, withSessionSetter: false });
    await dispatchCommand("/models Sonnet 5.5", noSetter.ctx);
    expect(noSetter.setConfigModel).toHaveBeenCalledWith("anthropic/claude-sonnet-5-5", "devbox");
  });

  it("suggests canonical ids without writing when the name matches nothing", async () => {
    const { ctx, setConfigModel, setSessionModel } = makeCtx({
      sessionId: "ses_123",
      models: MODELS,
    });
    const result = await dispatchCommand("/models son", ctx);
    expect(setSessionModel).not.toHaveBeenCalled();
    expect(setConfigModel).not.toHaveBeenCalled();
    expect(result.messages?.[0]?.content).toContain("Did you mean");
    expect(result.messages?.[0]?.content).toContain("anthropic/claude-sonnet-5-5");
  });
});
