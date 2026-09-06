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

// reportAuthFailure + the persistent remote-session marker (whole-branch
// review fix Important 7): a 401 in a remote session must clear the stale
// cached token (so it's never resent) while keeping isRemoteSession() true
// across a reload — the marker is what makes that survive the token clear.
describe("reportAuthFailure and the persistent remote-session marker", () => {
  beforeEach(() => {
    vi.resetModules();
    sessionStorage.clear();
    window.history.replaceState(null, "", "/");
  });

  it("clears the cached token and fires the registered handler on a 401 in a remote session", async () => {
    window.history.replaceState(null, "", "/#token=abc123");
    const { setAuthFailureHandler, reportAuthFailure } = await import("./client");
    const handler = vi.fn();
    setAuthFailureHandler(handler);

    reportAuthFailure(401);

    expect(sessionStorage.getItem("ocode.remoteToken")).toBeNull();
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("does nothing for a non-401 status", async () => {
    window.history.replaceState(null, "", "/#token=abc123");
    const { setAuthFailureHandler, reportAuthFailure } = await import("./client");
    const handler = vi.fn();
    setAuthFailureHandler(handler);

    reportAuthFailure(500);

    expect(sessionStorage.getItem("ocode.remoteToken")).toBe("abc123");
    expect(handler).not.toHaveBeenCalled();
  });

  it("does nothing for a 401 outside a remote session (e.g. wrong password on a plain authenticated server)", async () => {
    // No fragment/cached token/marker — a plain non-remote session.
    const { setAuthFailureHandler, reportAuthFailure } = await import("./client");
    const handler = vi.fn();
    setAuthFailureHandler(handler);

    reportAuthFailure(401);

    expect(handler).not.toHaveBeenCalled();
  });

  it("keeps isRemoteSession() true across a reload after the cached token is cleared, via the persistent marker", async () => {
    window.history.replaceState(null, "", "/#token=abc123");
    const first = await import("./client");
    expect(first.isRemoteSession()).toBe(true);
    first.reportAuthFailure(401); // simulate a 401 clearing the cached token

    // Simulate a reload: fresh module instance, same sessionStorage (the
    // fragment is long gone, and the token cache was just cleared).
    vi.resetModules();
    window.history.replaceState(null, "", "/");
    const second = await import("./client");
    expect(second.isRemoteSession()).toBe(true);
    expect(second.authToken()).toBe("");
  });
});
