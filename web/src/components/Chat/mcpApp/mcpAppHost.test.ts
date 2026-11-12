import { describe, expect, it, vi } from "vitest";
import { buildMcpAppSrcDoc, createMcpAppHost, mcpAppCSP, type McpAppHostOptions } from "./mcpAppHost";

function metaOf(doc: string): string {
  const m = /<meta http-equiv="Content-Security-Policy" content="([^"]*)">/.exec(doc);
  if (!m) throw new Error("no CSP meta in document");
  return m[1];
}

function makeOpts(overrides: Partial<McpAppHostOptions> = {}) {
  const posted: Array<Record<string, unknown>> = [];
  const opts: McpAppHostOptions = {
    post: vi.fn((msg: object) => {
      posted.push(msg as Record<string, unknown>);
    }),
    callTool: vi.fn(async () => ({ content: [{ type: "text", text: "ok" }] })),
    openLink: vi.fn(),
    requestDisplayMode: vi.fn((mode) => mode),
    onSize: vi.fn(),
    getHostContext: () => ({ theme: "dark" }),
    toolInput: { screen: 1 },
    toolResult: { content: [{ type: "text", text: "result" }] },
    ...overrides,
  };
  return { opts, posted };
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("buildMcpAppSrcDoc", () => {
  const META_START = '<meta http-equiv="Content-Security-Policy" content="';

  it("places the meta directly after a leading doctype", () => {
    const doc = buildMcpAppSrcDoc("<!doctype html><html><head><title>x</title></head><body>b</body></html>", {
      resourceDomains: ["https://cdn.example.com"],
    });
    expect(doc.startsWith("<!doctype html>" + META_START)).toBe(true);
    expect(doc.indexOf("<meta")).toBeLessThan(doc.indexOf("<html>"));
    expect(metaOf(doc)).toContain("script-src 'unsafe-inline' https://cdn.example.com");
  });

  it("starts the document with the meta when there is no leading doctype", () => {
    const doc = buildMcpAppSrcDoc('<html lang="en"><head><title>x</title></head><body>hi</body></html>');
    expect(doc.startsWith(META_START)).toBe(true);
    expect(doc.endsWith('<html lang="en"><head><title>x</title></head><body>hi</body></html>')).toBe(true);
  });

  it("puts the meta before a head tag that sits inside a leading comment", () => {
    const withDoctype = buildMcpAppSrcDoc("<!doctype html><!-- <head> --><html><head></head></html>");
    expect(withDoctype.startsWith("<!doctype html>" + META_START)).toBe(true);
    expect(withDoctype.indexOf("<meta")).toBeLessThan(withDoctype.indexOf("<!-- <head>"));

    const noDoctype = buildMcpAppSrcDoc("<!-- <head> --><html><head></head></html>");
    expect(noDoctype.startsWith(META_START)).toBe(true);
  });

  it("matches a doctype case-insensitively and keeps it first", () => {
    const doc = buildMcpAppSrcDoc("<!DOCTYPE html><p>hi</p>");
    expect(doc.startsWith("<!DOCTYPE html>" + META_START)).toBe(true);
  });

  it("tolerates whitespace before the doctype", () => {
    const doc = buildMcpAppSrcDoc("\n  <!doctype html><p>hi</p>");
    expect(doc.startsWith("\n  <!doctype html>" + META_START)).toBe(true);
  });

  it("emits exactly one CSP meta and leaves the body unchanged", () => {
    const body = "<HTML><HEAD><TITLE>x</TITLE></HEAD><BODY>b</BODY></HTML>";
    const doc = buildMcpAppSrcDoc(body);
    expect(doc.match(/Content-Security-Policy/g)).toHaveLength(1);
    expect(doc.endsWith(body)).toBe(true);
  });

  it("parses with the CSP meta as the first element of document.head", () => {
    const out = buildMcpAppSrcDoc("<!doctype html><html><head><title>x</title></head><body>b</body></html>", {
      connectDomains: ["https://api.example.com"],
    });
    const parsed = new DOMParser().parseFromString(out, "text/html");
    const first = parsed.head.firstElementChild;
    expect(first?.tagName).toBe("META");
    expect(first?.getAttribute("http-equiv")).toBe("Content-Security-Policy");
    expect(first?.getAttribute("content")).toContain("connect-src https://api.example.com;");
    expect(parsed.head.querySelector("title")?.textContent).toBe("x");
    expect(parsed.compatMode).toBe("CSS1Compat");
  });

  it("drops hostile domain entries that could break out of the CSP string", () => {
    const policy = mcpAppCSP({
      resourceDomains: ["https://ok.example.com", "https://evil.example\"; script-src *"],
      connectDomains: ["javascript:alert(1)", "https://a.com;b", "wss://ws.example.com:8443", "https://*.api.example"],
    });
    expect(policy).toContain("script-src 'unsafe-inline' https://ok.example.com;");
    expect(policy).not.toContain("evil");
    expect(policy).not.toContain("javascript");
    expect(policy).toContain("connect-src wss://ws.example.com:8443 https://*.api.example;");
  });

  it("uses 'none' for connect-src when no connect domains survive", () => {
    expect(mcpAppCSP({ connectDomains: [] })).toContain("connect-src 'none';");
    expect(mcpAppCSP({ connectDomains: ["not a domain"] })).toContain("connect-src 'none';");
    expect(mcpAppCSP(undefined)).toContain("connect-src 'none';");
  });

  it("emits the full fixed policy", () => {
    const policy = mcpAppCSP({});
    expect(policy).toBe(
      "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; " +
        "font-src data:; media-src data: blob:; connect-src 'none'; frame-src 'none'; object-src 'none'; " +
        "base-uri 'none'; form-action 'none'",
    );
  });
});

describe("createMcpAppHost", () => {
  it("answers ui/initialize echoing the requested protocol version", () => {
    const { opts, posted } = makeOpts();
    createMcpAppHost(opts).handleMessage({
      jsonrpc: "2.0",
      id: 1,
      method: "ui/initialize",
      params: { protocolVersion: "2025-11-25" },
    });
    expect(posted).toEqual([
      {
        jsonrpc: "2.0",
        id: 1,
        result: {
          protocolVersion: "2025-11-25",
          hostInfo: { name: "ocode", version: "1" },
          hostCapabilities: { serverTools: {}, openLinks: {}, logging: {} },
          hostContext: { theme: "dark" },
        },
      },
    ]);
  });

  it("falls back to the default protocol version when none is requested", () => {
    const { opts, posted } = makeOpts();
    createMcpAppHost(opts).handleMessage({ jsonrpc: "2.0", id: "a", method: "ui/initialize" });
    expect((posted[0].result as { protocolVersion: string }).protocolVersion).toBe("2026-01-26");
  });

  it("sends tool-input then tool-result exactly once after initialized", () => {
    const { opts, posted } = makeOpts();
    const host = createMcpAppHost(opts);
    host.handleMessage({ jsonrpc: "2.0", method: "ui/notifications/initialized" });
    host.handleMessage({ jsonrpc: "2.0", method: "ui/notifications/initialized" });
    expect(posted).toEqual([
      { jsonrpc: "2.0", method: "ui/notifications/tool-input", params: { arguments: { screen: 1 } } },
      {
        jsonrpc: "2.0",
        method: "ui/notifications/tool-result",
        params: { content: [{ type: "text", text: "result" }] },
      },
    ]);
  });

  it("answers tools/call with the raw result", async () => {
    const { opts, posted } = makeOpts();
    createMcpAppHost(opts).handleMessage({
      jsonrpc: "2.0",
      id: 7,
      method: "tools/call",
      params: { name: "show_screen", arguments: { x: 1 } },
    });
    await flush();
    expect(opts.callTool).toHaveBeenCalledWith("show_screen", { x: 1 });
    expect(posted).toEqual([
      { jsonrpc: "2.0", id: 7, result: { content: [{ type: "text", text: "ok" }] } },
    ]);
  });

  it("passes empty arguments when tools/call omits them", async () => {
    const { opts } = makeOpts();
    createMcpAppHost(opts).handleMessage({ jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "t" } });
    await flush();
    expect(opts.callTool).toHaveBeenCalledWith("t", {});
  });

  it("answers a rejected tools/call with the upstream code", async () => {
    const err = Object.assign(new Error("upstream broke"), { code: -32603 });
    const { opts, posted } = makeOpts({ callTool: vi.fn(() => Promise.reject(err)) });
    createMcpAppHost(opts).handleMessage({ jsonrpc: "2.0", id: 3, method: "tools/call", params: { name: "t" } });
    await flush();
    expect(posted).toEqual([{ jsonrpc: "2.0", id: 3, error: { code: -32603, message: "upstream broke" } }]);
  });

  it("uses -32000 when a rejection carries no numeric code", async () => {
    const { opts, posted } = makeOpts({ callTool: vi.fn(() => Promise.reject(new Error("down"))) });
    createMcpAppHost(opts).handleMessage({ jsonrpc: "2.0", id: 4, method: "tools/call", params: { name: "t" } });
    await flush();
    expect(posted[0]).toEqual({ jsonrpc: "2.0", id: 4, error: { code: -32000, message: "down" } });
  });

  it("rejects tools/call with invalid params without calling the tool", () => {
    const { opts, posted } = makeOpts();
    const host = createMcpAppHost(opts);
    host.handleMessage({ jsonrpc: "2.0", id: 5, method: "tools/call", params: { name: "" } });
    host.handleMessage({ jsonrpc: "2.0", id: 6, method: "tools/call", params: { name: "t", arguments: "x" } });
    expect(opts.callTool).not.toHaveBeenCalled();
    expect(posted.map((m) => (m.error as { code: number }).code)).toEqual([-32602, -32602]);
  });

  it("opens https links and rejects javascript: links", () => {
    const { opts, posted } = makeOpts();
    const host = createMcpAppHost(opts);
    host.handleMessage({ jsonrpc: "2.0", id: 1, method: "ui/open-link", params: { url: "https://example.com/x" } });
    host.handleMessage({ jsonrpc: "2.0", id: 2, method: "ui/open-link", params: { url: "javascript:alert(1)" } });
    expect(opts.openLink).toHaveBeenCalledTimes(1);
    expect(opts.openLink).toHaveBeenCalledWith("https://example.com/x");
    expect(posted[0]).toEqual({ jsonrpc: "2.0", id: 1, result: {} });
    expect(posted[1]).toMatchObject({ id: 2, error: { code: -32602 } });
  });

  it("handles request-display-mode and rejects unknown modes", () => {
    const requestDisplayMode = vi.fn(() => "inline" as const);
    const { opts, posted } = makeOpts({ requestDisplayMode });
    const host = createMcpAppHost(opts);
    host.handleMessage({ jsonrpc: "2.0", id: 1, method: "ui/request-display-mode", params: { mode: "fullscreen" } });
    host.handleMessage({ jsonrpc: "2.0", id: 2, method: "ui/request-display-mode", params: { mode: "pip" } });
    expect(requestDisplayMode).toHaveBeenCalledWith("fullscreen");
    expect(posted[0]).toEqual({ jsonrpc: "2.0", id: 1, result: { mode: "inline" } });
    expect(posted[1]).toMatchObject({ id: 2, error: { code: -32602 } });
  });

  it("forwards finite size-changed values to onSize", () => {
    const { opts } = makeOpts();
    const host = createMcpAppHost(opts);
    host.handleMessage({ jsonrpc: "2.0", method: "ui/notifications/size-changed", params: { width: 320, height: 240 } });
    host.handleMessage({ jsonrpc: "2.0", method: "ui/notifications/size-changed", params: { height: NaN } });
    expect(opts.onSize).toHaveBeenCalledTimes(1);
    expect(opts.onSize).toHaveBeenCalledWith(320, 240);
  });

  it("answers ping and rejects unknown requests with -32601", () => {
    const { opts, posted } = makeOpts();
    const host = createMcpAppHost(opts);
    host.handleMessage({ jsonrpc: "2.0", id: 1, method: "ping" });
    host.handleMessage({ jsonrpc: "2.0", id: 2, method: "resources/list" });
    expect(posted[0]).toEqual({ jsonrpc: "2.0", id: 1, result: {} });
    expect(posted[1]).toEqual({
      jsonrpc: "2.0",
      id: 2,
      error: { code: -32601, message: "Method not found: resources/list" },
    });
  });

  it("ignores notifications it does not handle", () => {
    const { opts, posted } = makeOpts();
    const host = createMcpAppHost(opts);
    host.handleMessage({ jsonrpc: "2.0", method: "notifications/message", params: { level: "info" } });
    host.handleMessage({ jsonrpc: "2.0", method: "something/else" });
    expect(posted).toEqual([]);
  });

  it("ignores data that is not JSON-RPC 2.0", () => {
    const { opts, posted } = makeOpts();
    const host = createMcpAppHost(opts);
    host.handleMessage("hello");
    host.handleMessage(null);
    host.handleMessage({ id: 1, method: "ping" });
    host.handleMessage({ jsonrpc: "1.0", id: 1, method: "ping" });
    expect(posted).toEqual([]);
  });

  it("posts host-context-changed for notifyContext", () => {
    const { opts, posted } = makeOpts();
    createMcpAppHost(opts).notifyContext({ theme: "light" });
    expect(posted).toEqual([
      { jsonrpc: "2.0", method: "ui/notifications/host-context-changed", params: { theme: "light" } },
    ]);
  });

  it("after teardown a tools/call is never dispatched and gets no reply", () => {
    const { opts, posted } = makeOpts();
    const host = createMcpAppHost(opts);
    host.teardown();
    const afterTeardown = posted.length;

    host.handleMessage({ jsonrpc: "2.0", id: 11, method: "tools/call", params: { name: "stop", arguments: {} } });
    host.handleMessage({ jsonrpc: "2.0", id: 12, method: "ui/open-link", params: { url: "https://example.com" } });

    expect(opts.callTool).not.toHaveBeenCalled();
    expect(opts.openLink).not.toHaveBeenCalled();
    expect(posted).toHaveLength(afterTeardown);
  });

  it("after teardown posts nothing and drops late tool results", async () => {
    let resolveCall: (v: unknown) => void = () => {};
    const callTool = vi.fn(() => new Promise<unknown>((resolve) => { resolveCall = resolve; }));
    const { opts, posted } = makeOpts({ callTool });
    const host = createMcpAppHost(opts);

    host.handleMessage({ jsonrpc: "2.0", id: 9, method: "tools/call", params: { name: "t" } });
    const beforeTeardown = posted.length;
    host.teardown();

    // Only the best-effort teardown request went out.
    expect(posted.slice(beforeTeardown)).toEqual([
      expect.objectContaining({ method: "ui/resource-teardown" }),
    ]);
    const afterTeardown = posted.length;

    resolveCall({ content: [] });
    await flush();
    host.handleMessage({ jsonrpc: "2.0", id: 10, method: "ping" });
    host.notifyContext({ theme: "light" });
    host.teardown();

    expect(posted).toHaveLength(afterTeardown);
  });
});
