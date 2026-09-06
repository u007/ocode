import { describe, it, expect, beforeEach } from "vitest";
import {
  browserStore,
  browserActions,
  CONSOLE_CAP,
  normalizeBrowseURL,
  isPrivateHost,
} from "./browserStore";

const KEY = "tab:abc" as const;

beforeEach(() => {
  // Reset store between cases (module-level singleton).
  browserStore.setState(() => ({ byKey: {} }));
  localStorage.clear();
});

describe("browserStore", () => {
  it("open creates default state, close discards it", () => {
    browserActions.open(KEY);
    expect(browserStore.state.byKey[KEY]).toBeTruthy();
    expect(browserStore.state.byKey[KEY].panelOpen).toBe(true);
    browserActions.close(KEY);
    expect(browserStore.state.byKey[KEY]).toBeUndefined();
  });

  it("navigate pushes history and truncates the forward stack", () => {
    browserActions.open(KEY);
    browserActions.navigate(KEY, "https://a.com");
    browserActions.navigate(KEY, "https://b.com");
    browserActions.back(KEY); // index now at a.com
    browserActions.navigate(KEY, "https://c.com"); // truncates b.com
    const s = browserStore.state.byKey[KEY];
    expect(s.history).toEqual(["https://a.com", "https://c.com"]);
    expect(s.historyIndex).toBe(1);
  });

  it("back/forward move the index without mutating history", () => {
    browserActions.open(KEY);
    browserActions.navigate(KEY, "https://a.com");
    browserActions.navigate(KEY, "https://b.com");
    browserActions.back(KEY);
    expect(browserStore.state.byKey[KEY].historyIndex).toBe(0);
    browserActions.forward(KEY);
    expect(browserStore.state.byKey[KEY].historyIndex).toBe(1);
    // back past start / forward past end are no-ops.
    browserActions.back(KEY);
    browserActions.back(KEY);
    expect(browserStore.state.byKey[KEY].historyIndex).toBe(0);
  });

  it("pushConsole ring-caps at CONSOLE_CAP, dropping the oldest", () => {
    browserActions.open(KEY);
    for (let i = 0; i < CONSOLE_CAP + 5; i++) {
      browserActions.pushConsole(KEY, { level: "log", text: `m${i}`, ts: i });
    }
    const ev = browserStore.state.byKey[KEY].consoleEvents;
    expect(ev.length).toBe(CONSOLE_CAP);
    expect(ev[0].text).toBe("m5"); // first 5 dropped
    expect(ev[ev.length - 1].text).toBe(`m${CONSOLE_CAP + 4}`);
  });

  it("applyNavEvent sets the authoritative url + status and clears loading", () => {
    browserActions.open(KEY);
    browserActions.applyNavEvent(KEY, {
      state_key: KEY, url: "https://final.com/x", status: 200, mode: "chrome",
    });
    const s = browserStore.state.byKey[KEY];
    expect(s.url).toBe("https://final.com/x");
    expect(s.status).toBe(200);
    expect(s.loading).toBe(false);
  });

  it("isPrivateHost matches localhost/*.localhost/loopback/RFC1918, rejects lookalikes", () => {
    expect(isPrivateHost("localhost")).toBe(true);
    expect(isPrivateHost("localhost:3000")).toBe(true);
    expect(isPrivateHost("app.localhost")).toBe(true);
    expect(isPrivateHost("a.b.localhost:5173")).toBe(true);
    expect(isPrivateHost("127.0.0.1")).toBe(true);
    expect(isPrivateHost("127.0.0.1:80")).toBe(true);
    expect(isPrivateHost("192.168.1.10")).toBe(true);
    expect(isPrivateHost("10.0.0.5")).toBe(true);
    expect(isPrivateHost("172.16.0.1")).toBe(true);
    expect(isPrivateHost("[::1]:5173")).toBe(true);
    expect(isPrivateHost("[::1]")).toBe(true);
    // lookalikes are public
    expect(isPrivateHost("localhost.evil.com")).toBe(false);
    expect(isPrivateHost("example.com")).toBe(false);
    expect(isPrivateHost("172.32.0.1")).toBe(false); // outside 172.16-31
  });

  it("applyNavEvent with status 0 marks loading", () => {
    browserActions.open(KEY);
    browserActions.applyNavEvent(KEY, { state_key: KEY, url: "https://x.com", status: 0, mode: "chrome" });
    expect(browserStore.state.byKey[KEY].loading).toBe(true);
    expect(browserStore.state.byKey[KEY].mode).toBe("chrome");
  });
});

describe("normalizeBrowseURL", () => {
  it("keeps an explicit scheme", () => {
    expect(normalizeBrowseURL("http://example.com/x")).toBe("http://example.com/x");
    expect(normalizeBrowseURL("https://localhost:3000/")).toBe("https://localhost:3000/");
  });
  it("assumes http for localhost / loopback / private hosts", () => {
    expect(normalizeBrowseURL("localhost:3000")).toBe("http://localhost:3000");
    expect(normalizeBrowseURL("localhost")).toBe("http://localhost");
    expect(normalizeBrowseURL("127.0.0.1:8080/api")).toBe("http://127.0.0.1:8080/api");
    expect(normalizeBrowseURL("[::1]:5173")).toBe("http://[::1]:5173");
    expect(normalizeBrowseURL("192.168.1.20:4096")).toBe("http://192.168.1.20:4096");
    expect(normalizeBrowseURL("10.0.0.5")).toBe("http://10.0.0.5");
    expect(normalizeBrowseURL("172.20.0.1")).toBe("http://172.20.0.1");
    expect(normalizeBrowseURL("app.localhost:3000")).toBe("http://app.localhost:3000");
  });
  it("assumes https for everything else", () => {
    expect(normalizeBrowseURL("example.com")).toBe("https://example.com");
    expect(normalizeBrowseURL("example.com:8443/p?q=1")).toBe("https://example.com:8443/p?q=1");
    expect(normalizeBrowseURL("172.32.0.1")).toBe("https://172.32.0.1");
  });
  it("trims whitespace and leaves empty alone", () => {
    expect(normalizeBrowseURL("  localhost:3000 ")).toBe("http://localhost:3000");
    expect(normalizeBrowseURL("")).toBe("");
  });
  it("navigate and open store the normalized URL", () => {
    browserActions.open(KEY, "localhost:3000");
    expect(browserStore.state.byKey[KEY].url).toBe("http://localhost:3000");
    browserActions.navigate(KEY, "example.com");
    expect(browserStore.state.byKey[KEY].url).toBe("https://example.com");
    expect(browserStore.state.byKey[KEY].history).toEqual(["http://localhost:3000", "https://example.com"]);
  });
});

describe("page titles (tab strip)", () => {
  it("setPageTitle applies matching titles and drops stale ones", () => {
    browserActions.open(KEY);
    browserActions.navigate(KEY, "https://a.com/");
    browserActions.setPageTitle(KEY, "A Site", "https://a.com/");
    expect(browserStore.state.byKey[KEY].pageTitle).toBe("A Site");
    // Stale: URL from before a navigation.
    browserActions.setPageTitle(KEY, "Stale", "https://old.com/");
    expect(browserStore.state.byKey[KEY].pageTitle).toBe("A Site");
  });

  it("an empty title clears back to fallback instead of sticking", () => {
    browserActions.open(KEY);
    browserActions.navigate(KEY, "https://a.com/");
    browserActions.setPageTitle(KEY, "A Site", "https://a.com/");
    expect(browserStore.state.byKey[KEY].pageTitle).toBe("A Site");
    // Page removed its <title>: clear (strip falls back to "New tab").
    browserActions.setPageTitle(KEY, "   ", "https://a.com/");
    expect(browserStore.state.byKey[KEY].pageTitle).toBeNull();
    // A stale clear for another URL must not wipe the live title.
    browserActions.setPageTitle(KEY, "A Site", "https://a.com/");
    browserActions.setPageTitle(KEY, "", "https://old.com/");
    expect(browserStore.state.byKey[KEY].pageTitle).toBe("A Site");
  });

  it("navigate clears a stale page title for the new document", () => {
    browserActions.open(KEY);
    browserActions.navigate(KEY, "https://a.com/");
    browserActions.setPageTitle(KEY, "A Site", "https://a.com/");
    browserActions.navigate(KEY, "https://b.com/");
    expect(browserStore.state.byKey[KEY].pageTitle).toBeNull();
  });

  it("applyNavEvent keeps the title on same-URL updates, drops it on new URLs", () => {
    browserActions.open(KEY, "https://a.com/");
    browserActions.setPageTitle(KEY, "A Site", "https://a.com/");
    browserActions.applyNavEvent(KEY, { state_key: KEY, url: "https://a.com/", status: 200, mode: "chrome" });
    expect(browserStore.state.byKey[KEY].pageTitle).toBe("A Site");
    browserActions.applyNavEvent(KEY, { state_key: KEY, url: "https://b.com/", status: 200, mode: "chrome" });
    expect(browserStore.state.byKey[KEY].pageTitle).toBeNull();
  });
});

describe("scroll offsets", () => {
  it("setScrollY records per-URL offsets and rejects garbage", () => {
    browserActions.open(KEY);
    browserActions.navigate(KEY, "https://a.com/");
    browserActions.setScrollY(KEY, -5);
    browserActions.setScrollY(KEY, NaN);
    expect(browserStore.state.byKey[KEY].scrollByUrl).toEqual({});
    browserActions.setScrollY(KEY, 321);
    expect(browserStore.state.byKey[KEY].scrollY).toBe(321);
    expect(browserStore.state.byKey[KEY].scrollByUrl["https://a.com/"]).toBe(321);
  });

  it("back/forward preload the stored offset for the target URL", () => {
    browserActions.open(KEY);
    browserActions.navigate(KEY, "https://a.com/");
    browserActions.setScrollY(KEY, 100);
    browserActions.navigate(KEY, "https://b.com/");
    expect(browserStore.state.byKey[KEY].scrollY).toBe(0);
    browserActions.back(KEY);
    expect(browserStore.state.byKey[KEY].scrollY).toBe(100);
  });

  it("restoreSurface rehydrates a persisted snapshot", () => {
    browserActions.restoreSurface(KEY, {
      url: "https://b.com/",
      history: ["https://a.com/", "https://b.com/"],
      historyIndex: 1,
      userMode: null,
      pageTitle: "B Site",
      scrollByUrl: { "https://b.com/": 55 },
    });
    const s = browserStore.state.byKey[KEY];
    expect(s.url).toBe("https://b.com/");
    expect(s.historyIndex).toBe(1);
    expect(s.pageTitle).toBe("B Site");
    expect(s.scrollY).toBe(55);
    expect(s.loading).toBe(false);
  });
});
