import { describe, it, expect, afterEach, vi } from "vitest";
import { fetchJSON, ApiError } from "./client";
// fetchJSON is now exported so tests (and components needing a custom
// RequestInit shape) can call it directly, same pattern as authedFetch.
// Each case stubs global fetch to a fixed Response.

function jsonResponse(body: string, status = 200, contentType = "application/json"): Response {
  return new Response(body, { status, headers: { "Content-Type": contentType } });
}

describe("fetchJSON success-body hardening", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("parses a valid JSON body", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse('[{"remote_port":3000}]')));
    await expect(fetchJSON("/api/desktop/portmaps")).resolves.toEqual([
      { remote_port: 3000 },
    ]);
  });

  it("returns undefined for an empty 200 body instead of throwing WebKit's SyntaxError", async () => {
    // WebKit (desktop WKWebView) response.json() on "" throws
    // "SyntaxError: The string did not match the expected pattern" —
    // the exact error seen in the port-maps panel before the guard.
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse("", 200)));
    await expect(fetchJSON("/api/empty")).resolves.toBeUndefined();
  });

  it("throws a readable ApiError (not a SyntaxError) for a 200 HTML body", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse("<!DOCTYPE html><html>spa</html>", 200, "text/html")),
    );
    try {
      await fetchJSON("/api/desktop/portmaps");
      expect.fail("expected ApiError");
    } catch (e) {
      expect(e).toBeInstanceOf(ApiError);
      const err = e as ApiError;
      expect(err.name).toBe("ApiError");
      expect(err.message).toContain("Non-JSON response");
      expect(err.message).toContain("/api/desktop/portmaps");
      expect(err.status).toBe(200);
      // The old behavior leaked a bare SyntaxError with WebKit's message.
      expect(err.constructor.name).not.toBe("SyntaxError");
    }
  });

  it("surfaces the server's error JSON on a non-2xx as before", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse('{"error":"port already in use"}', 409)),
    );
    try {
      await fetchJSON("/api/desktop/portmaps");
      expect.fail("expected ApiError");
    } catch (e) {
      expect(e).toBeInstanceOf(ApiError);
      expect((e as ApiError).message).toBe("port already in use");
      expect((e as ApiError).status).toBe(409);
    }
  });
});