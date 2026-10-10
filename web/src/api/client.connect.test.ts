import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./client";

// Pins the client half of the Connectors contract: the exact URL + method for
// each route, the request body, and — the part that actually bites — that
// `host` is threaded through EVERY call. Credentials are per machine, so a
// connect call answered from the local store while the user is looking at a
// remote project would save the key on the wrong box. Threading is asserted
// with an explicit `undefined` rather than by omission: vitest distinguishes
// `("a","b")` from `("a","b",undefined)`, and an accidentally-dropped trailing
// arg is exactly the regression this file exists to catch.
function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

let fetchMock: ReturnType<typeof vi.fn>;

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubFetch(body: unknown = { ok: true }): void {
  fetchMock = vi.fn(async () => jsonResponse(body));
  vi.stubGlobal("fetch", fetchMock);
}

function lastCall(): [string, RequestInit | undefined] {
  const call = fetchMock.mock.calls[fetchMock.mock.calls.length - 1];
  return [call[0] as string, call[1] as RequestInit | undefined];
}

describe("api connector methods — URL and method", () => {
  it("lists providers with GET and no body", async () => {
    stubFetch({ providers: [] });
    await api.listConnectProviders();

    const [url, init] = lastCall();
    expect(url).toBe("/api/auth/connect");
    expect(init?.method ?? "GET").toBe("GET");
    expect(init?.body).toBeUndefined();
  });

  it("sets a credential with PUT and the apiKey body field", async () => {
    stubFetch({ ok: true, provider: {} });
    await api.setConnectCredential("anthropic", { apiKey: "sk-ant-123" });

    const [url, init] = lastCall();
    expect(url).toBe("/api/auth/connect/anthropic");
    expect(init?.method).toBe("PUT");
    expect(JSON.parse(init?.body as string)).toEqual({ apiKey: "sk-ant-123" });
  });

  it("removes a credential with DELETE", async () => {
    stubFetch({ ok: true, provider: {} });
    await api.removeConnectCredential("anthropic");

    const [url, init] = lastCall();
    expect(url).toBe("/api/auth/connect/anthropic");
    expect(init?.method).toBe("DELETE");
  });

  it("tests a credential via POST to the /test sub-route", async () => {
    stubFetch({ ok: true });
    await api.testConnectCredential("openai");

    const [url, init] = lastCall();
    expect(url).toBe("/api/auth/connect/openai/test");
    expect(init?.method).toBe("POST");
  });

  it("starts an OAuth flow with the method id in the body", async () => {
    stubFetch({ flowId: "f1", kind: "paste-code" });
    await api.startConnectFlow("anthropic", "oauth_console");

    const [url, init] = lastCall();
    expect(url).toBe("/api/auth/connect/anthropic/oauth/start");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({ method: "oauth_console" });
  });

  it("polls flow status via GET", async () => {
    stubFetch({ flowId: "f1", state: "running" });
    await api.getConnectFlow("f1");

    const [url, init] = lastCall();
    expect(url).toBe("/api/auth/connect/flows/f1");
    expect(init?.method ?? "GET").toBe("GET");
  });

  it("submits flow input with code, authToken and ct0", async () => {
    stubFetch({ flowId: "f1", state: "running" });
    await api.submitConnectFlowInput("f1", { code: "http://localhost/cb?code=x", authToken: "", ct0: "" });

    const [url, init] = lastCall();
    expect(url).toBe("/api/auth/connect/flows/f1/input");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({
      code: "http://localhost/cb?code=x",
      authToken: "",
      ct0: "",
    });
  });

  it("cancels a flow with DELETE", async () => {
    stubFetch({ flowId: "f1", state: "cancelled" });
    await api.cancelConnectFlow("f1");

    const [url, init] = lastCall();
    expect(url).toBe("/api/auth/connect/flows/f1");
    expect(init?.method).toBe("DELETE");
  });
});

describe("api connector methods — host threading", () => {
  // Credentials live in a per-machine auth store, so every connector call must
  // reach the host the user is actually looking at. The observable proof is the
  // remote-proxy prefix on the URL — NOT the number of arguments handed to
  // fetch, which is an implementation detail of fetchJSON. A dropped trailing
  // `host` shows up here as a missing `/api/remote/<host>` prefix.
  const HOST = "host-a";
  const cases: Array<[string, (host: string | undefined) => Promise<unknown>, string]> = [
    ["listConnectProviders", (h) => api.listConnectProviders(h), "/api/auth/connect"],
    ["setConnectCredential", (h) => api.setConnectCredential("anthropic", { apiKey: "k" }, h), "/api/auth/connect/anthropic"],
    ["removeConnectCredential", (h) => api.removeConnectCredential("anthropic", h), "/api/auth/connect/anthropic"],
    ["testConnectCredential", (h) => api.testConnectCredential("openai", h), "/api/auth/connect/openai/test"],
    ["startConnectFlow", (h) => api.startConnectFlow("anthropic", "oauth", h), "/api/auth/connect/anthropic/oauth/start"],
    ["getConnectFlow", (h) => api.getConnectFlow("f1", h), "/api/auth/connect/flows/f1"],
    ["submitConnectFlowInput", (h) => api.submitConnectFlowInput("f1", { code: "c" }, h), "/api/auth/connect/flows/f1/input"],
    ["cancelConnectFlow", (h) => api.cancelConnectFlow("f1", h), "/api/auth/connect/flows/f1"],
  ];

  for (const [name, call, path] of cases) {
    it(`${name} prefixes the URL with the remote proxy when a host is given`, async () => {
      stubFetch();
      await call(HOST);

      const [url] = lastCall();
      expect(url).toBe(`/api/remote/${encodeURIComponent(HOST)}${path}`);
    });
  }

  for (const [name, call] of cases) {
    it(`${name} targets the local server when no host is given`, async () => {
      stubFetch();
      await call(undefined);

      const [url] = lastCall();
      // Guards the opposite regression: a method that prefixed the proxy path
      // unconditionally, or that left a stale host in a ref, would silently
      // redirect local credential writes.
      expect(url.startsWith("/api/remote/")).toBe(false);
    });
  }
});

describe("api connector methods — the legacy connectProvider route is untouched", () => {
  it("still posts to POST /api/auth/connect, which is a DIFFERENT server route", async () => {
    stubFetch({ provider: "openai", key: "sk-1" });
    await api.connectProvider("openai", "sk-1");

    const [url, init] = lastCall();
    expect(url).toBe("/api/auth/connect");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({ provider: "openai", api_key: "sk-1" });
  });
});