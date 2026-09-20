import { beforeEach, describe, expect, it, vi } from "vitest";

import { getWindowId, WINDOW_ID_STORAGE_KEY } from "./windowId";

describe("getWindowId", () => {
  beforeEach(() => {
    sessionStorage.clear();
    window.history.replaceState(null, "", "/");
    vi.restoreAllMocks();
  });

  it("uses the ?windowId= URL param and persists it to sessionStorage", () => {
    window.history.replaceState(null, "", "/?windowId=main");
    expect(getWindowId()).toBe("main");
    expect(sessionStorage.getItem(WINDOW_ID_STORAGE_KEY)).toBe("main");
  });

  it("keeps returning the persisted id after the query string is dropped", () => {
    // Reproduces the divergence: the desktop deep-link redirect
    // (`SessionPage` -> navigate("/", {replace:true})) strips ?windowId=, and
    // the old URL-param branch never persisted it, so the chat request minted a
    // fresh random id while the profile pill still targeted "main".
    window.history.replaceState(null, "", "/session/ses_1?windowId=main");
    const pillWindowId = getWindowId();
    expect(pillWindowId).toBe("main");

    window.history.replaceState(null, "", "/");
    expect(getWindowId()).toBe(pillWindowId);
    expect(sessionStorage.getItem(WINDOW_ID_STORAGE_KEY)).toBe("main");
  });

  it("reuses a stored id across calls without a URL param", () => {
    const first = getWindowId();
    expect(first).toMatch(/^win-[0-9a-f]{8}$/);
    expect(getWindowId()).toBe(first);
  });

  it("mints distinct ids for distinct tabs (fresh sessionStorage, no param)", () => {
    const a = getWindowId();
    sessionStorage.clear();
    const b = getWindowId();
    expect(b).not.toBe(a);
  });

  it("prefers the URL param over a stale stored id", () => {
    sessionStorage.setItem(WINDOW_ID_STORAGE_KEY, "stale");
    window.history.replaceState(null, "", "/?windowId=main");
    expect(getWindowId()).toBe("main");
  });
});
