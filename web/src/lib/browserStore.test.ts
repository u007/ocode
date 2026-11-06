import { describe, it, expect, beforeEach } from "vitest";
import {
  browserStore,
  browserActions,
  CONSOLE_CAP,
  HISTORY_CAP,
  NETWORK_BODY_CAP,
  NETWORK_CAP,
  isLocalRequestId,
  normalizeBrowseURL,
  isPrivateHost,
  type NetworkEvent,
  type ResponseBody,
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
    browserActions.close(KEY, "");
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

  it("rekey moves a surface's state to the new key and drops the old", () => {
    browserActions.open(KEY);
    browserActions.navigate(KEY, "https://a.com");
    const next = "side:chat:ses_new" as const;
    browserActions.rekey(KEY, next);
    expect(browserStore.state.byKey[KEY]).toBeUndefined();
    expect(browserStore.state.byKey[next]?.url).toBe("https://a.com");
  });

  it("rekey is a no-op when the source is absent or the target already exists", () => {
    const next = "side:chat:ses_new" as const;
    // absent source
    browserActions.rekey(KEY, next);
    expect(browserStore.state.byKey[next]).toBeUndefined();
    // target already live: the live target wins, the source is left alone.
    browserActions.open(KEY);
    browserActions.navigate(KEY, "https://old.com");
    browserActions.open(next);
    browserActions.navigate(next, "https://live.com");
    browserActions.rekey(KEY, next);
    expect(browserStore.state.byKey[next]?.url).toBe("https://live.com");
    expect(browserStore.state.byKey[KEY]?.url).toBe("https://old.com");
  });

  it("retains response bodies for only the newest rows and drops them on clear", () => {
    browserActions.open(KEY);
    // Local mode pushes every body EAGERLY (a consumed response cannot be
    // re-read), so the store must bound what it keeps.
    const total = NETWORK_BODY_CAP + 4;
    for (let i = 0; i < total; i++) {
      const requestId = `local-${i}`;
      browserActions.pushNetwork(KEY, {
        requestId,
        method: "GET",
        url: `https://a.dev/${i}`,
        status: 200,
        durationMs: 1,
        ts: i,
      });
      browserActions.setResponseBody(KEY, requestId, { body: `body-${i}`, base64Encoded: false, truncated: false });
    }

    const bodies = browserStore.state.byKey[KEY].responseBodies;
    expect(Object.keys(bodies)).toHaveLength(NETWORK_BODY_CAP);
    expect(bodies[`local-${total - 1}`].body).toBe(`body-${total - 1}`);
    expect(bodies["local-0"]).toBeUndefined();

    browserActions.clearNetwork(KEY);
    expect(browserStore.state.byKey[KEY].responseBodies).toEqual({});
  });

  it("keeps an on-demand CDP body while its row is in the ring buffer, even past the local body cap", () => {
    browserActions.open(KEY);
    const cdpId = "1234.56";
    browserActions.pushNetwork(KEY, { requestId: cdpId, method: "GET", url: "https://a.dev/api", status: 200, durationMs: 1, ts: 0 });
    // The user opens the row: the body arrives on demand, after the row.
    browserActions.setResponseBody(KEY, cdpId, { body: "cdp-body", base64Encoded: false, truncated: false });

    // Local rows push their own bodies eagerly, far past the local cap.
    for (let i = 0; i < NETWORK_BODY_CAP + 5; i++) {
      const requestId = `local-${i}`;
      browserActions.pushNetwork(KEY, { requestId, method: "GET", url: `https://a.dev/${i}`, status: 200, durationMs: 1, ts: i + 1 });
      browserActions.setResponseBody(KEY, requestId, { body: `body-${i}`, base64Encoded: false, truncated: false });
    }

    const bodies = browserStore.state.byKey[KEY].responseBodies;
    expect(bodies[cdpId]?.body).toBe("cdp-body");
    const localIds = Object.keys(bodies).filter(isLocalRequestId);
    expect(localIds).toHaveLength(NETWORK_BODY_CAP);
    expect(bodies["local-0"]).toBeUndefined();

    // Once the ring buffer drops the CDP row, its body goes with it.
    for (let i = 0; i < NETWORK_CAP; i++) {
      browserActions.pushNetwork(KEY, { requestId: `filler-${i}`, method: "GET", url: "https://a.dev/f", status: 200, durationMs: 1, ts: i + 1000 });
    }
    expect(browserStore.state.byKey[KEY].networkEvents.some((e) => e.requestId === cdpId)).toBe(false);
    expect(browserStore.state.byKey[KEY].responseBodies[cdpId]).toBeUndefined();
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

describe("syncPageURL", () => {
  it("applies a same-origin page hint without adding history, rejects cross-origin", () => {
    const key = "tab:sync1" as const;
    browserActions.open(key);
    browserActions.navigate(key, "https://example.com/a");
    const got = browserActions.syncPageURL(key, "https://example.com/b?x=1");
    expect(got).toEqual({ url: "https://example.com/b?x=1", idx: 0 });
    const t = browserStore.get().byKey[key];
    expect(t.url).toBe("https://example.com/b?x=1");
    expect(t.history).toHaveLength(1);
    expect(browserActions.syncPageURL(key, "https://evil.com/")).toBeNull();
    expect(browserStore.get().byKey[key].url).toBe("https://example.com/b?x=1");
  });
});

describe("syncPageURL push", () => {
  it("appends a history entry for pushState and keeps back working", () => {
    const key = "tab:sync2" as const;
    browserActions.open(key);
    browserActions.navigate(key, "https://example.com/a");
    expect(browserActions.syncPageURL(key, "https://example.com/b", "push")).toEqual({ url: "https://example.com/b", idx: 1 });
    const t = browserStore.get().byKey[key];
    expect(t.history.slice(-2)).toEqual(["https://example.com/a", "https://example.com/b"]);
    browserActions.back(key);
    expect(browserStore.get().byKey[key].url).toBe("https://example.com/a");
  });
});

describe("syncPageURL traverse (popstate/hashchange)", () => {
  it("selects the entry the page moved to instead of overwriting the one it left", () => {
    const key = "tab:trav1" as const;
    browserActions.open(key);
    for (const p of ["a", "b", "c"]) browserActions.navigate(key, `https://example.com/${p}`);
    // history.back() from C: popstate reports B. Must not become [A,B,B].
    expect(browserActions.syncPageURL(key, "https://example.com/b", "traverse")).toEqual({ url: "https://example.com/b", idx: 1 });
    const t = browserStore.get().byKey[key];
    expect(t.history).toEqual(["https://example.com/a", "https://example.com/b", "https://example.com/c"]);
    expect(t.historyIndex).toBe(1);
    expect(t.url).toBe("https://example.com/b");
  });

  it("picks the nearest matching entry when the URL repeats in the list", () => {
    const key = "tab:trav2" as const;
    browserActions.open(key);
    for (const p of ["a", "b", "c", "b", "d"]) browserActions.navigate(key, `https://example.com/${p}`);
    expect(browserActions.syncPageURL(key, "https://example.com/b", "traverse")).toEqual({ url: "https://example.com/b", idx: 3 });
    expect(browserStore.get().byKey[key].history).toHaveLength(5);
  });

  it("an unknown URL with no entry to select overwrites the current entry", () => {
    const key = "tab:trav3" as const;
    browserActions.open(key);
    browserActions.navigate(key, "https://example.com/a");
    browserActions.navigate(key, "https://example.com/b");
    expect(browserActions.syncPageURL(key, "https://example.com/c", "traverse")).toEqual({ url: "https://example.com/c", idx: 1 });
    expect(browserStore.get().byKey[key].history).toEqual(["https://example.com/a", "https://example.com/c"]);
  });

  it("replace still overwrites the current entry and keeps earlier ones", () => {
    const key = "tab:trav4" as const;
    browserActions.open(key);
    for (const p of ["a", "b", "c"]) browserActions.navigate(key, `https://example.com/${p}`);
    expect(browserActions.syncPageURL(key, "https://example.com/d", "replace")).toEqual({ url: "https://example.com/d", idx: 2 });
    expect(browserStore.get().byKey[key].history).toEqual(["https://example.com/a", "https://example.com/b", "https://example.com/d"]);
  });

  it("a hint for the URL already current changes nothing", () => {
    const key = "tab:trav5" as const;
    browserActions.open(key);
    browserActions.navigate(key, "https://example.com/a");
    const before = browserStore.get().byKey[key];
    expect(browserActions.syncPageURL(key, "https://example.com/a", "traverse")).toBeNull();
    expect(browserStore.get().byKey[key]).toBe(before);
  });
});

describe("history cap", () => {
  it("navigate drops the oldest entries past HISTORY_CAP and keeps the index at the end", () => {
    const key = "tab:cap1" as const;
    browserActions.open(key);
    const total = HISTORY_CAP + 5;
    for (let i = 0; i < total; i++) browserActions.navigate(key, `https://example.com/p${i}`);
    const t = browserStore.get().byKey[key];
    expect(t.history).toHaveLength(HISTORY_CAP);
    expect(t.history[0]).toBe("https://example.com/p5");
    expect(t.history[HISTORY_CAP - 1]).toBe(`https://example.com/p${total - 1}`);
    expect(t.historyIndex).toBe(HISTORY_CAP - 1);
    expect(t.url).toBe(`https://example.com/p${total - 1}`);
  });

  it("page pushState past the cap is capped the same way", () => {
    const key = "tab:cap2" as const;
    browserActions.open(key);
    browserActions.navigate(key, "https://example.com/p0");
    for (let i = 1; i <= HISTORY_CAP + 4; i++) {
      expect(browserActions.syncPageURL(key, `https://example.com/p${i}`, "push")).toEqual({
        url: `https://example.com/p${i}`,
        idx: Math.min(i, HISTORY_CAP - 1),
      });
    }
    const t = browserStore.get().byKey[key];
    expect(t.history).toHaveLength(HISTORY_CAP);
    expect(t.historyIndex).toBe(HISTORY_CAP - 1);
  });
});

describe("retainBodies equivalence", () => {
  // The full-recompute retention that retainBodies replaced, copied here as a
  // fixed reference: every row is walked, then every body is checked against it.
  function referenceRetainBodies(
    bodies: Record<string, ResponseBody>,
    networkEvents: NetworkEvent[],
  ): Record<string, ResponseBody> {
    const recentLocal = new Set<string>();
    const rowIds = new Set<string>();
    for (let i = networkEvents.length - 1; i >= 0; i--) {
      const id = networkEvents[i].requestId;
      if (!id) continue;
      rowIds.add(id);
      if (isLocalRequestId(id) && recentLocal.size < NETWORK_BODY_CAP) recentLocal.add(id);
    }
    const next: Record<string, ResponseBody> = {};
    for (const id of Object.keys(bodies)) {
      const keep = isLocalRequestId(id) ? recentLocal.has(id) : rowIds.has(id);
      if (keep) next[id] = bodies[id];
    }
    return Object.keys(next).length === Object.keys(bodies).length ? bodies : next;
  }

  // mulberry32: a small seeded PRNG, so a failing sequence reproduces exactly.
  function seededRandom(seed: number): () => number {
    let a = seed >>> 0;
    return () => {
      a = (a + 0x6d2b79f5) >>> 0;
      let t = a;
      t = Math.imul(t ^ (t >>> 15), t | 1);
      t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
  }

  it.each([7, 99])("matches the full-recompute reference over a random sequence (seed %i)", (seed) => {
    browserActions.open(KEY);
    const rand = seededRandom(seed);
    const pick = (n: number) => Math.floor(rand() * n);
    const body = (tag: string): ResponseBody => ({ body: tag, base64Encoded: false, truncated: false });
    let unique = 0;
    // Row and body ids: local and CDP rows, duplicate CDP ids (redirect reuse),
    // empty ids, and body ids that never get a row (orphans). Local ids repeat
    // past NETWORK_BODY_CAP distinct rows.
    const requestId = () => {
      const r = rand();
      if (r < 0.05) return "";
      if (r < 0.35) return `local-${pick(40)}`;
      if (r < 0.6) return `cdp-${pick(30)}`;
      if (r < 0.9) return `${rand() < 0.5 ? "local" : "cdp"}-u${unique++}`;
      return `${rand() < 0.5 ? "local" : "cdp"}-orphan-${pick(10)}`;
    };

    let shadowRows: NetworkEvent[] = [];
    let shadowBodies: Record<string, ResponseBody> = {};
    let evictions = 0;
    let droppedOnPush = 0;
    let keptOnPush = 0;
    for (let step = 0; step < 4000; step++) {
      const op = rand();
      // One clear, after the ring has evicted: the sequence must cover both.
      if (step === 2500) {
        browserActions.clearNetwork(KEY);
        shadowRows = [];
        shadowBodies = {};
      } else if (op < 0.35) {
        const id = requestId();
        const b = body(`b${step}`);
        browserActions.setResponseBody(KEY, id, b);
        shadowBodies = { ...shadowBodies, [id]: b };
      } else {
        const ev: NetworkEvent = {
          requestId: requestId(),
          method: "GET",
          url: `https://a.dev/${step}`,
          status: 200,
          durationMs: 1,
          ts: step,
        };
        const storeBefore = browserStore.state.byKey[KEY].responseBodies;
        const shadowBefore = shadowBodies;
        browserActions.pushNetwork(KEY, ev);
        const grown = [...shadowRows, ev];
        if (grown.length > NETWORK_CAP) evictions++;
        shadowRows = grown.length > NETWORK_CAP ? grown.slice(grown.length - NETWORK_CAP) : grown;
        shadowBodies = referenceRetainBodies(shadowBodies, shadowRows);
        // Identity is observable (selectors re-render on it): the map is reused
        // exactly when the reference reuses it.
        const storeKept = browserStore.state.byKey[KEY].responseBodies === storeBefore;
        expect(storeKept).toBe(shadowBodies === shadowBefore);
        if (storeKept) keptOnPush++;
        else droppedOnPush++;
      }
      const state = browserStore.state.byKey[KEY];
      expect(state.networkEvents.map((e) => e.ts)).toEqual(shadowRows.map((e) => e.ts));
      expect(Object.keys(state.responseBodies)).toEqual(Object.keys(shadowBodies));
      expect(state.responseBodies).toEqual(shadowBodies);
    }
    // The sequence must reach the paths under test, or the equivalence proves little.
    expect(evictions).toBeGreaterThan(0);
    expect(droppedOnPush).toBeGreaterThan(0);
    expect(keptOnPush).toBeGreaterThan(0);
  });
});

describe("reader mode flag", () => {
  it("defaults to false and setReader flips it per surface", () => {
    browserActions.open(KEY);
    expect(browserStore.state.byKey[KEY].reader).toBe(false);
    browserActions.setReader(KEY, true);
    expect(browserStore.state.byKey[KEY].reader).toBe(true);
    browserActions.setReader(KEY, false);
    expect(browserStore.state.byKey[KEY].reader).toBe(false);
  });

  it("setReader leaves other surfaces and the collapsed flag alone", () => {
    browserActions.open(KEY);
    browserActions.open("tab:other" as const);
    browserActions.setCollapsed(KEY, true);
    browserActions.setReader(KEY, true);
    expect(browserStore.state.byKey[KEY].collapsed).toBe(true);
    expect(browserStore.state.byKey["tab:other" as const].reader).toBe(false);
  });

  it("setReader on an unknown key is a no-op", () => {
    browserActions.setReader("tab:missing" as const, true);
    expect(browserStore.state.byKey["tab:missing" as const]).toBeUndefined();
  });
});
