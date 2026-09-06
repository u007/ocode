import { describe, it, expect, beforeEach, vi } from "vitest";

// client.ts resolves its auth token from window.location at module-eval
// time, so each test needs a fresh module instance with a fresh
// location/sessionStorage. vi.resetModules() + a dynamic import per test
// forces Vitest to re-evaluate the module (and its top-level
// resolveInitialToken() call) from scratch for each case.
describe("remote token bootstrap", () => {
  beforeEach(() => {
    vi.resetModules();
    sessionStorage.clear();
    window.history.replaceState(null, "", "/");
  });

  it("reads a fragment token, strips the fragment, and caches it in sessionStorage", async () => {
    window.history.replaceState(null, "", "/#token=abc123");
    const { authToken, isRemoteSession } = await import("./client");
    expect(authToken()).toBe("abc123");
    expect(isRemoteSession()).toBe(true);
    expect(window.location.hash).toBe("");
    expect(sessionStorage.getItem("ocode.remoteToken")).toBe("abc123");
  });

  it("falls back to a cached sessionStorage token on a plain reload", async () => {
    sessionStorage.setItem("ocode.remoteToken", "cached123");
    const { authToken, isRemoteSession } = await import("./client");
    expect(authToken()).toBe("cached123");
    expect(isRemoteSession()).toBe(true);
  });

  it("falls back to the existing ?token= query string when neither is present", async () => {
    window.history.replaceState(null, "", "/?token=rctoken");
    const { authToken, isRemoteSession } = await import("./client");
    expect(authToken()).toBe("rctoken");
    expect(isRemoteSession()).toBe(false);
  });

  it("fragment token takes precedence over an existing sessionStorage cache", async () => {
    sessionStorage.setItem("ocode.remoteToken", "stale");
    window.history.replaceState(null, "", "/#token=fresh456");
    const { authToken, isRemoteSession } = await import("./client");
    expect(authToken()).toBe("fresh456");
    expect(isRemoteSession()).toBe(true);
    expect(sessionStorage.getItem("ocode.remoteToken")).toBe("fresh456");
  });

  it("sessionStorage cache takes precedence over the ?token= query string", async () => {
    sessionStorage.setItem("ocode.remoteToken", "cached789");
    window.history.replaceState(null, "", "/?token=rctoken");
    const { authToken, isRemoteSession } = await import("./client");
    expect(authToken()).toBe("cached789");
    expect(isRemoteSession()).toBe(true);
  });

  it("authHeaders() reflects the resolved token", async () => {
    window.history.replaceState(null, "", "/#token=abc123");
    const { authHeaders } = await import("./client");
    expect(authHeaders()).toEqual({ Authorization: "Bearer abc123" });
  });

  it("returns an empty token and non-remote session when nothing is present", async () => {
    const { authToken, isRemoteSession, authHeaders } = await import("./client");
    expect(authToken()).toBe("");
    expect(isRemoteSession()).toBe(false);
    expect(authHeaders()).toEqual({});
  });
});
