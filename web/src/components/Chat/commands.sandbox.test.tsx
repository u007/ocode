import { describe, it, expect, vi, beforeEach } from "vitest";
import { dispatchCommand } from "./commands";
import { api } from "../../api/client";

// The /sandbox command's handler calls api.setPermissionMode / api.getPermissions
// through the module-level `api` (imported in commands.ts), so we mock the
// client and assert the right endpoint is invoked.
vi.mock("../../api/client", () => ({
  api: {
    setPermissionMode: vi.fn(),
    getPermissions: vi.fn(),
    // Unused by these handlers but referenced by commands.ts at import of the
    // api object shape; add the ones the dispatcher may reach. Provide sensible
    // defaults for the command handlers we don't exercise.
    getYolo: vi.fn(async () => ({ yolo: false })),
    getSession: vi.fn(async () => ({ messages: [], title: "" })),
    listSessions: vi.fn(async () => []),
  },
}));

const mockSetPermissionMode = vi.mocked(api.setPermissionMode);
const mockGetPermissions = vi.mocked(api.getPermissions);

function ctx() {
  // Minimal CommandContext accepted by the /sandbox and /yolo branches.
  return {
    commandName: "sandbox",
    args: "",
    api: {
      listSessions: async () => [],
      getSession: async () => ({ messages: [], title: "" }),
      getOcrConfig: async () => ({ enabled: false, engine: "tesseract", appName: "" } as never),
      setOcrConfig: async () => ({}) as never,
      getOcrModels: async () => [] as never,
      getOcrEnabled: async () => ({ enabled: false, model: "" }) as never,
      setOcrEnabled: async () => ({}) as never,
      setOcrModel: async () => ({}) as never,
      compactSession: async () => ({ original_len: 0, compacted_len: 0 }),
      recapSession: async () => ({ recap: "" }),
      shareSession: async () => ({ markdown: "" }),
      btwSession: async () => ({ status: "" }),
      getMaskConfig: async () => ({ enabled: false, mode: "", model: "" }),
      setMaskEnabled: async () => ({ enabled: false }),
      setMaskMode: async () => ({ mode: "" }),
      setMaskModel: async () => ({ model: "" }),
      getCommandContext: async () => ({ prompt: "" }),
      getSessionContext: async () => ({ session_id: "", message_count: 0, estimated_tokens: 0 }),
    } as never,
  };
}

describe("/sandbox command", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("on invokes setPermissionMode('sandbox')", async () => {
    const res = await dispatchCommand("/sandbox on", ctx());
    expect(mockSetPermissionMode).toHaveBeenCalledWith("sandbox");
    expect(res.handled).toBe(true);
    expect(res.messages?.[0]?.content).toContain("Sandbox mode");
  });

  it("off invokes setPermissionMode('normal')", async () => {
    await dispatchCommand("/sandbox off", ctx());
    expect(mockSetPermissionMode).toHaveBeenCalledWith("normal");
  });

  it("status reads getPermissions and reports confined behavior", async () => {
    mockGetPermissions.mockResolvedValueOnce({
      mode: "sandbox",
      auto_allow: false,
      sandbox_supported: true,
      effective_behavior: "confined",
      rules: [],
      bash_rules: [],
    } as never);
    const res = await dispatchCommand("/sandbox status", ctx());
    expect(mockGetPermissions).toHaveBeenCalled();
    expect(res.messages?.[0]?.content).toContain("**on**");
    expect(res.messages?.[0]?.content).toContain("confined");
  });
});
