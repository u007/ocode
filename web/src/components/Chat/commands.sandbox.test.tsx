import { describe, it, expect, vi, beforeEach } from "vitest";
import { dispatchCommand } from "./commands";
import { api } from "../../api/client";

// The /sandbox command's handler calls api.setPermissionMode / api.getPermissions
// through the module-level `api` (imported in commands.ts), so we mock the
// client and assert the right endpoint is invoked — with the active session id,
// since permission modes are per chat session.
vi.mock("../../api/client", () => ({
  api: {
    setPermissionMode: vi.fn(),
    getPermissions: vi.fn(),
    // Unused by these handlers but referenced by commands.ts at import of the
    // api object shape; add the ones the dispatcher may reach. Provide sensible
    // defaults for the command handlers we don't exercise.
    getYolo: vi.fn(async () => ({ yolo: false })),
    setYolo: vi.fn(async () => ({ yolo: true })),
    getSession: vi.fn(async () => ({ messages: [], title: "" })),
    listSessions: vi.fn(async () => []),
  },
}));

const mockSetPermissionMode = vi.mocked(api.setPermissionMode);
const mockGetPermissions = vi.mocked(api.getPermissions);

function ctx(sessionId: string | null = "ses_test_123") {
  // Minimal CommandContext accepted by the /sandbox and /yolo branches.
  return {
    commandName: "sandbox",
    args: "",
    getSessionId: () => sessionId,
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
      getSessionContext: async () => ({ session_id: "", message_count: 0, current_tokens: 0 }),
    } as never,
  };
}

describe("/sandbox command", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("on invokes setPermissionMode('sandbox', sessionId) — scoped, never global", async () => {
    const res = await dispatchCommand("/sandbox on", ctx("ses_abc"));
    expect(mockSetPermissionMode).toHaveBeenCalledWith("sandbox", "ses_abc", undefined);
    expect(res.handled).toBe(true);
    expect(res.messages?.[0]?.content).toContain("Sandbox mode");
  });

  it("off invokes setPermissionMode('normal', sessionId)", async () => {
    await dispatchCommand("/sandbox off", ctx("ses_xyz"));
    expect(mockSetPermissionMode).toHaveBeenCalledWith("normal", "ses_xyz", undefined);
  });

  it("status reads getPermissions scoped to the session and reports confined behavior", async () => {
    mockGetPermissions.mockResolvedValueOnce({
      mode: "sandbox",
      auto_allow: false,
      sandbox_supported: true,
      effective_behavior: "confined",
      rules: [],
      bash_rules: [],
    } as never);
    const res = await dispatchCommand("/sandbox status", ctx("ses_stat"));
    expect(mockGetPermissions).toHaveBeenCalledWith("ses_stat", undefined);
    expect(res.messages?.[0]?.content).toContain("**on**");
    expect(res.messages?.[0]?.content).toContain("confined");
  });
});

describe("/yolo command", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("on invokes setYolo(true, sessionId) — scoped, never global", async () => {
    await dispatchCommand("/yolo on", ctx("ses_yolo"));
    expect(api.setYolo).toHaveBeenCalledWith(true, "ses_yolo", undefined);
  });
});

describe("permission commands on a draft (new-*) tab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("holds the pick locally instead of PUTting a session-less mode", async () => {
    const setDraftPermissionMode = vi.fn();
    const c = ctx("new-123");
    (c as { setDraftPermissionMode?: (m: string) => void }).setDraftPermissionMode =
      setDraftPermissionMode;

    await dispatchCommand("/sandbox on", c);
    expect(setDraftPermissionMode).toHaveBeenCalledWith("sandbox");
    // No server session exists yet, so no scoped (and certainly no global) PUT.
    expect(mockSetPermissionMode).not.toHaveBeenCalled();
  });

  it("/yolo on holds the draft pick locally", async () => {
    const setDraftPermissionMode = vi.fn();
    const c = ctx("new-456");
    (c as { setDraftPermissionMode?: (m: string) => void }).setDraftPermissionMode =
      setDraftPermissionMode;

    await dispatchCommand("/yolo on", c);
    expect(setDraftPermissionMode).toHaveBeenCalledWith("yolo");
    expect(api.setYolo).not.toHaveBeenCalled();
  });
});
