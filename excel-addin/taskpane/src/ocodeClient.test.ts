import { describe, expect, it } from "vitest";
import { OcodeClient } from "./ocodeClient";

// A fetch stand-in that records each call and answers with a JSON body.
function recorder(reply: unknown = {}) {
  const calls: { url: string; method: string; body?: unknown; key?: string }[] = [];
  const fetchImpl = (async (url: string, init: RequestInit) => {
    calls.push({ url, method: init.method ?? "GET", body: init.body === undefined ? undefined : JSON.parse(String(init.body)), key: (init.headers as Record<string, string>)["X-Bridge-Key"] });
    return new Response(JSON.stringify(reply), { status: 200, headers: { "Content-Type": "application/json" } });
  }) as typeof fetch;
  return { calls, client: new OcodeClient({ base: "", key: "k1", fetchImpl }) };
}

describe("ocode client calls added for the pane controls", () => {
  it("reads a session's context window through the proxy", async () => {
    const { calls, client } = recorder({ current_tokens: 481, max_tokens: 1000000, model: "opencode-go/x", message_count: 2 });
    expect(await client.getContext("ses_1")).toMatchObject({ current_tokens: 481, max_tokens: 1000000, model: "opencode-go/x" });
    expect(calls[0]).toMatchObject({ url: "/api/sessions/ses_1/context", method: "GET", key: "k1" });
  });
  it("sets one session's permission mode with the session id in the body", async () => {
    const { calls, client } = recorder();
    await client.setPermissionMode("ses_1", "yolo");
    expect(calls[0]).toMatchObject({ url: "/api/permissions/mode", method: "PUT", body: { mode: "yolo", session_id: "ses_1" } });
  });
  it("reads and sets the server-wide auto-permission switch through bridge routes", async () => {
    const { calls, client } = recorder({ enabled: true });
    expect(await client.getAutoPermission()).toEqual({ enabled: true });
    await client.setAutoPermission(false);
    expect(calls.map((c) => [c.url, c.method, c.body])).toEqual([
      ["/bridge/auto-permission", "GET", undefined],
      ["/bridge/auto-permission", "PUT", { enabled: false }],
    ]);
  });
  it("reads the connected provider ids through the bridge route", async () => {
    const { calls, client } = recorder({ providers: ["opencode-go"] });
    expect(await client.connectedProviders()).toEqual({ providers: ["opencode-go"] });
    expect(calls[0]).toMatchObject({ url: "/bridge/connected-providers", method: "GET" });
  });
  it("starts a chat pinned to the excel MCP server", async () => {
    const { calls, client } = recorder({ sessionId: "ses_1", model: "m" });
    await client.startChat({ content: "hi", projectPath: "/ws/wb1", permissionMode: "normal" });
    expect(calls[0]).toMatchObject({
      url: "/api/chat",
      method: "POST",
      body: { content: "hi", project_path: "/ws/wb1", permission_mode: "normal", async: true, pinned_mcp_servers: ["excel"] },
    });
  });
  it("reads a session's saved permission mode through the bridge's narrow route, not the status proxy", async () => {
    const { calls, client } = recorder({ permission_mode: "locked" });
    expect(await client.getSavedMode("ses_1")).toEqual({ permission_mode: "locked" });
    expect(calls[0]).toMatchObject({ url: "/bridge/session-mode/ses_1", method: "GET", key: "k1" });
  });
});
