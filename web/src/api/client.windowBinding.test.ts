import { beforeEach, describe, expect, it, vi } from "vitest";

// The window id sent on chat requests must match the id the ProfileSwitcher
// wrote its active profile to. Both must come from one resolver, or a profile
// picked for window "main" is invisible to a session that re-derived a random
// id after the desktop deep-link redirect stripped ?windowId=.
describe("chat request window binding", () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules();
    sessionStorage.clear();
    window.history.replaceState(null, "", "/");
    fetchSpy = vi.fn(async () => new Response(JSON.stringify({ sessionId: "s1", model: "m" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }));
    vi.stubGlobal("fetch", fetchSpy);
  });

  it("sendMessage binds to the same window id the profile pill resolved", async () => {
    // Desktop deep link: URL carries windowId=main, and the profile pill is the
    // first thing to resolve it.
    window.history.replaceState(null, "", "/session/ses_1?windowId=main");
    const { getActiveWindowId } = await import("../components/ProfileSwitcher");
    const pillWindowId = getActiveWindowId();
    expect(pillWindowId).toBe("main");

    // SessionPage then redirects to "/" with no query string.
    window.history.replaceState(null, "", "/");

    const { api } = await import("./client");
    await api.sendMessage("ses_1", "hello");
    const init = fetchSpy.mock.calls[0][1] as RequestInit;
    const body = JSON.parse(String(init.body));
    expect(body.windowId).toBe(pillWindowId);
    expect((init.headers as Headers).get("X-Window-Id")).toBe(pillWindowId);
  });

  it("chat binds to the same window id the profile pill resolved", async () => {
    window.history.replaceState(null, "", "/session/ses_1?windowId=main");
    const { getActiveWindowId } = await import("../components/ProfileSwitcher");
    const pillWindowId = getActiveWindowId();
    expect(pillWindowId).toBe("main");
    window.history.replaceState(null, "", "/");

    const { api } = await import("./client");
    await api.chat("hello", undefined, undefined, "new-1", "/proj");
    const init = fetchSpy.mock.calls[0][1] as RequestInit;
    const body = JSON.parse(String(init.body));
    expect(body.windowId).toBe(pillWindowId);
    expect((init.headers as Headers).get("X-Window-Id")).toBe(pillWindowId);
  });
});
