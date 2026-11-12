// Host side of the MCP Apps protocol (JSON-RPC 2.0 over postMessage) for a
// widget rendered in a sandboxed srcdoc iframe. Pure: no React, no DOM beyond
// the document string it builds, so every branch is unit-testable.

/** Protocol version the host answers with when the widget does not request one. */
export const MCP_APP_DEFAULT_PROTOCOL_VERSION = "2026-01-26";

/** Identity of one widget: cards that share a server and resource are one view. */
export function mcpAppKey(ref: { server: string; resource_uri: string }): string {
  return `${ref.server}|${ref.resource_uri}`;
}

const JSONRPC_VERSION = "2.0";
const ERR_INVALID_PARAMS = -32602;
const ERR_METHOD_NOT_FOUND = -32601;
const ERR_DEFAULT = -32000;

export type McpAppDisplayMode = "inline" | "fullscreen";

export interface McpAppCSPHints {
  connectDomains?: string[];
  resourceDomains?: string[];
}

// Domain entries are interpolated into a CSP string, so only plain origins
// pass: no quotes, semicolons, spaces or commas can get through.
const HTTP_DOMAIN_RE = /^https?:\/\/[A-Za-z0-9.*:-]+$/;
const WS_DOMAIN_RE = /^wss?:\/\/[A-Za-z0-9.*:-]+$/;

function safeDomains(list: unknown): string[] {
  if (!Array.isArray(list)) return [];
  return list.filter(
    (d): d is string => typeof d === "string" && (HTTP_DOMAIN_RE.test(d) || WS_DOMAIN_RE.test(d)),
  );
}

/** Builds the Content-Security-Policy for a widget. Exported for tests. */
export function mcpAppCSP(csp?: McpAppCSPHints): string {
  const resource = safeDomains(csp?.resourceDomains);
  const connect = safeDomains(csp?.connectDomains);
  const src = (base: string, domains: string[]) => [base, ...domains].join(" ");
  return [
    "default-src 'none'",
    src("script-src 'unsafe-inline'", resource),
    src("style-src 'unsafe-inline'", resource),
    src("img-src data: blob:", resource),
    src("font-src data:", resource),
    src("media-src data: blob:", resource),
    connect.length > 0 ? src("connect-src", connect) : "connect-src 'none'",
    "frame-src 'none'",
    "object-src 'none'",
    "base-uri 'none'",
    "form-action 'none'",
  ].join("; ");
}

/**
 * Returns the widget document with the CSP meta tag placed first in the parse.
 *
 * The meta goes immediately after a leading doctype, or at the very start of the
 * document when there is none. Nothing else is searched for: a `<head` or `<html`
 * tag can sit inside a comment or script string ahead of the real head, and a
 * meta placed there would be ignored by the parser. An HTML parser puts a
 * leading `<meta>` into the implicit head and merges the later `<html>`/`<head>`
 * tags, so the policy is always the first thing parsed. A leading doctype stays first,
 * so standards mode holds for documents that have one.
 */
export function buildMcpAppSrcDoc(html: string, csp?: McpAppCSPHints): string {
  const meta = `<meta http-equiv="Content-Security-Policy" content="${mcpAppCSP(csp)}">`;
  const doctype = /^\s*<!doctype[^>]*>/i.exec(html);
  if (doctype) {
    const at = doctype[0].length;
    return html.slice(0, at) + meta + html.slice(at);
  }
  return meta + html;
}

export interface McpAppHostOptions {
  /** Sends one JSON-RPC message to the iframe. */
  post(msg: object): void;
  /** Forwards a widget tools/call. Resolves with the raw CallToolResult; rejects
   *  with an Error that may carry a numeric `code` (the upstream JSON-RPC code). */
  callTool(name: string, args: Record<string, unknown>): Promise<unknown>;
  openLink(url: string): void;
  /** Returns the display mode actually applied. */
  requestDisplayMode(mode: McpAppDisplayMode): McpAppDisplayMode;
  onSize(width?: number, height?: number): void;
  getHostContext(): object;
  toolInput: object;
  toolResult: object;
}

export interface McpAppHost {
  handleMessage(data: unknown): void;
  notifyContext(partial: object): void;
  teardown(): void;
}

type JsonRpcId = string | number;

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function errorCode(err: unknown): number {
  if (isObject(err) && typeof err.code === "number" && Number.isFinite(err.code)) return err.code;
  return ERR_DEFAULT;
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

export function createMcpAppHost(opts: McpAppHostOptions): McpAppHost {
  let torn = false;
  let initialized = false;
  let nextId = 1;
  const pending = new Map<JsonRpcId, (result: unknown) => void>();

  function send(msg: object): void {
    if (torn) return;
    opts.post(msg);
  }

  function respond(id: JsonRpcId, body: { result: unknown } | { error: { code: number; message: string } }): void {
    send({ jsonrpc: JSONRPC_VERSION, id, ...body });
  }

  function respondError(id: JsonRpcId, code: number, message: string): void {
    respond(id, { error: { code, message } });
  }

  // Host-initiated requests. Responses resolve here; nothing awaits teardown.
  function sendRequest(method: string, params: object): Promise<unknown> {
    const id = nextId++;
    return new Promise<unknown>((resolve) => {
      pending.set(id, resolve);
      send({ jsonrpc: JSONRPC_VERSION, id, method, params });
    });
  }

  function handleRequest(id: JsonRpcId, method: string, params: unknown): void {
    switch (method) {
      case "ui/initialize": {
        const requested = isObject(params) ? params.protocolVersion : undefined;
        const protocolVersion =
          typeof requested === "string" && requested !== "" ? requested : MCP_APP_DEFAULT_PROTOCOL_VERSION;
        respond(id, {
          result: {
            protocolVersion,
            hostInfo: { name: "ocode", version: "1" },
            hostCapabilities: { serverTools: {}, openLinks: {}, logging: {} },
            hostContext: opts.getHostContext(),
          },
        });
        return;
      }
      case "tools/call": {
        const p = isObject(params) ? params : undefined;
        const name = p?.name;
        const args = p?.arguments;
        if (typeof name !== "string" || name === "") {
          respondError(id, ERR_INVALID_PARAMS, "tools/call requires a non-empty name");
          return;
        }
        if (args !== undefined && !isObject(args)) {
          respondError(id, ERR_INVALID_PARAMS, "tools/call arguments must be an object");
          return;
        }
        let call: Promise<unknown>;
        try {
          call = opts.callTool(name, (args as Record<string, unknown> | undefined) ?? {});
        } catch (err) {
          call = Promise.reject(err);
        }
        call.then(
          (result) => {
            if (torn) return;
            respond(id, { result });
          },
          (err: unknown) => {
            if (torn) return;
            respondError(id, errorCode(err), errorMessage(err));
          },
        );
        return;
      }
      case "ui/open-link": {
        const url = isObject(params) ? params.url : undefined;
        let protocol: string | undefined;
        if (typeof url === "string") {
          try {
            protocol = new URL(url).protocol;
          } catch {
            protocol = undefined;
          }
        }
        if (typeof url !== "string" || (protocol !== "http:" && protocol !== "https:")) {
          respondError(id, ERR_INVALID_PARAMS, "ui/open-link requires an http or https url");
          return;
        }
        opts.openLink(url);
        respond(id, { result: {} });
        return;
      }
      case "ui/request-display-mode": {
        const mode = isObject(params) ? params.mode : undefined;
        if (mode !== "inline" && mode !== "fullscreen") {
          respondError(id, ERR_INVALID_PARAMS, "ui/request-display-mode mode must be inline or fullscreen");
          return;
        }
        respond(id, { result: { mode: opts.requestDisplayMode(mode) } });
        return;
      }
      case "ping":
        respond(id, { result: {} });
        return;
      default:
        respondError(id, ERR_METHOD_NOT_FOUND, `Method not found: ${method}`);
    }
  }

  function handleNotification(method: string, params: unknown): void {
    switch (method) {
      case "ui/notifications/initialized": {
        if (initialized) return;
        initialized = true;
        send({
          jsonrpc: JSONRPC_VERSION,
          method: "ui/notifications/tool-input",
          params: { arguments: opts.toolInput },
        });
        send({ jsonrpc: JSONRPC_VERSION, method: "ui/notifications/tool-result", params: opts.toolResult });
        return;
      }
      case "ui/notifications/size-changed": {
        if (!isObject(params)) return;
        const width = typeof params.width === "number" && Number.isFinite(params.width) ? params.width : undefined;
        const height = typeof params.height === "number" && Number.isFinite(params.height) ? params.height : undefined;
        if (width === undefined && height === undefined) return;
        opts.onSize(width, height);
        return;
      }
      default:
        // notifications/message and unknown notifications are ignored.
        return;
    }
  }

  return {
    handleMessage(data: unknown): void {
      if (torn) return;
      if (!isObject(data) || data.jsonrpc !== JSONRPC_VERSION) return;
      const method = typeof data.method === "string" ? data.method : undefined;
      const hasId = data.id !== undefined && data.id !== null;
      const id = hasId && (typeof data.id === "string" || typeof data.id === "number") ? data.id : undefined;

      if (method === undefined) {
        // A response to a host-initiated request.
        if (id === undefined) return;
        const resolve = pending.get(id);
        if (!resolve) return;
        pending.delete(id);
        resolve(data.result ?? data.error);
        return;
      }
      if (id !== undefined) {
        handleRequest(id, method, data.params);
        return;
      }
      if (hasId) {
        // An id that is neither a string nor a number cannot be answered.
        return;
      }
      handleNotification(method, data.params);
    },

    notifyContext(partial: object): void {
      send({ jsonrpc: JSONRPC_VERSION, method: "ui/notifications/host-context-changed", params: partial });
    },

    teardown(): void {
      if (torn) return;
      // Best effort: the widget may already be gone, and nothing awaits this.
      void sendRequest("ui/resource-teardown", {});
      torn = true;
      pending.clear();
    },
  };
}
