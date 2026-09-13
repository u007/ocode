import { describe, it, expect, vi } from "vitest";
import indexHtml from "../../index.html?raw";

// _basePath is resolved from window.location at module-eval time, so each
// case re-imports client.ts after replacing the location (same pattern as
// client.token.test.ts).
async function basePathFor(path: string): Promise<string> {
  vi.resetModules();
  window.history.replaceState(null, "", path);
  const { _basePath } = await import("./client");
  return _basePath;
}

// Runs the runtime <base> injection script from web/index.html against the
// jsdom location and returns the injected href path. The inline script and
// _basePath must derive the same prefix, so both are asserted per case.
function injectedBaseFor(path: string): string {
  window.history.replaceState(null, "", path);
  document.head.querySelectorAll("base").forEach((b) => b.remove());
  const m = indexHtml.match(/<script>([\s\S]*?)<\/script>/);
  if (!m) throw new Error("index.html: inline <base> script not found");
  new Function(m[1])();
  const base = document.head.querySelector("base");
  if (!base) throw new Error("inline script did not inject <base>");
  return new URL(base.href).pathname;
}

describe("_basePath / <base href> prefix derivation", () => {
  it("is empty at the server root", async () => {
    expect(await basePathFor("/")).toBe("");
    expect(injectedBaseFor("/")).toBe("/");
  });

  it("strips the trailing /session/<id> segment", async () => {
    expect(await basePathFor("/session/abc")).toBe("");
    expect(await basePathFor("/2026-07-05-173953/session/abc")).toBe("/2026-07-05-173953");
    expect(injectedBaseFor("/2026-07-05-173953/session/abc")).toBe("/2026-07-05-173953/");
  });

  it("treats a non-session path as the mount prefix (tailscale /desktop share)", async () => {
    expect(await basePathFor("/desktop/?token=t")).toBe("/desktop");
    expect(await basePathFor("/desktop")).toBe("/desktop");
    expect(await basePathFor("/desktop/session/abc")).toBe("/desktop");
    expect(injectedBaseFor("/desktop/?token=t")).toBe("/desktop/");
    expect(injectedBaseFor("/desktop")).toBe("/desktop/");
  });
});
