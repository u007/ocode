import { beforeEach, describe, expect, it, vi } from "vitest";
import { dispatchCommand } from "./commands";

// /ban remove must DELETE the rule, not write an "ask" rule. An inert ask entry
// outlives the ban and still lists under "Other bash rules" in /permissions.
const getPermissions = vi.fn();
const setBashRules = vi.fn();
const setBashRule = vi.fn();

function ctx() {
  return {
    commandName: "ban",
    args: "",
    getSessionId: () => "ses_ban",
    api: { getPermissions, setBashRules, setBashRule },
  } as never;
}

describe("/ban command", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getPermissions.mockResolvedValue({
      mode: "normal",
      auto_allow: false,
      sandbox_supported: true,
      effective_behavior: "normal",
      rules: [],
      bash_rules: [],
    });
  });

  it("remove deletes the prefix through the batch delta (no 'ask' downgrade)", async () => {
    const res = await dispatchCommand("/ban remove Sed", ctx());
    expect(setBashRules).toHaveBeenCalledWith({ remove: ["sed"] });
    expect(setBashRule).not.toHaveBeenCalled();
    expect(res.handled).toBe(true);
    expect(res.messages?.[0]?.content).toContain("Removed bash rule(s)");
    expect(res.messages?.[0]?.content).not.toContain("set to ask");
  });

  it("remove takes several prefixes in one delta", async () => {
    await dispatchCommand("/ban remove rm sed", ctx());
    expect(setBashRules).toHaveBeenCalledWith({ remove: ["rm", "sed"] });
  });

  it("add still writes a deny rule", async () => {
    await dispatchCommand("/ban add sed", ctx());
    expect(setBashRule).toHaveBeenCalledWith("sed", "deny");
    expect(setBashRules).not.toHaveBeenCalled();
  });
});
