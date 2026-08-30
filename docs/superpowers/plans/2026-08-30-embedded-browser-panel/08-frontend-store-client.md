# Part 08 — Frontend browserStore + Client API Additions

**Spec:** `docs/superpowers/specs/2026-08-30-embedded-browser-panel-design.md` (§ Frontend, § Security model — address bar is server-authoritative).

**Goal:** Add the per-`stateKey` browser state store, the client helpers that fetch the browse base URL / mint a grant / build the iframe `src`, and the SSE routing for the server-authoritative `browse_nav` event. No UI yet — Part 09 consumes this store.

> **STACK CORRECTION (verified in-repo):** this project does **not** use zustand. Global stores use **`@tanstack/react-store`** — `new Store(initialState)` at module scope, mutated with `store.setState(...)`, read in React via `useSelector(store, selector)` (see `web/src/stores/projectStore.tsx:2,289,291`). Persistence mirrors `web/src/components/Layout/tabOrderPersistence.ts` (a versioned localStorage file keyed by project path). The SSE stream is routed by `routeBusEnvelope(env, r)` in `web/src/lib/sessionEvents.ts`, and the set of subscribed event names is `ROUTABLE_EVENTS` (`sessionEvents.ts:140`), consumed by `web/src/components/Layout/SessionTabSync.tsx:67`. All code below uses these real patterns.

**Files:**
- Create: `web/src/lib/browserStore.ts`, `web/src/lib/browserStore.test.ts`
- Modify: `web/src/api/client.ts` (add `getBrowseBase`, `mintBrowseGrant`, `browseSrc`), `web/src/lib/sessionEvents.ts` (route `browse_nav`), `web/src/lib/sessionEvents.test.ts` (routing test)

**Interfaces:**
- Consumes: `GET /api/browse/config` → `{ base_url }` and `POST /api/browse/grant` → `{ grant }` (Part 01); the `browse_nav` bus event carrying `NavEvent` (Part 07); `authedFetch`/`apiPath` from `client.ts`.
- Produces (per INDEX contract, consumed by Parts 09/10): `useBrowserStore`, `browserStore`, `browserActions`, `getBrowseBase`, `mintBrowseGrant`, `browseSrc`, and the types `StateKey`, `BrowserTabState`, `ConsoleEvent`, `NetworkEvent`, `NavEvent`.

---

- [ ] **Step 1: Write the failing store test**

Create `web/src/lib/browserStore.test.ts`:

```ts
import { describe, it, expect, beforeEach } from "vitest";
import { browserStore, browserActions, CONSOLE_CAP } from "./browserStore";

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
      state_key: KEY, url: "https://final.com/x", status: 200, mode: "proxied",
    });
    const s = browserStore.state.byKey[KEY];
    expect(s.url).toBe("https://final.com/x");
    expect(s.status).toBe(200);
    expect(s.loading).toBe(false);
  });

  it("applyNavEvent with status 0 marks loading", () => {
    browserActions.open(KEY);
    browserActions.applyNavEvent(KEY, { state_key: KEY, url: "https://x.com", status: 0, mode: "proxied" });
    expect(browserStore.state.byKey[KEY].loading).toBe(true);
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/lib/browserStore.test.ts`
Expected: FAIL — cannot import `browserStore`/`browserActions`/`CONSOLE_CAP`.

- [ ] **Step 3: Implement the store**

Create `web/src/lib/browserStore.ts`:

```ts
import { Store } from "@tanstack/react-store";
import { useSelector } from "@tanstack/react-store";

/** Composite key identifying one browser surface's state. Side panels are
 *  keyed by their host chat/terminal tab; standalone browser tabs by their
 *  own id. Mirrors the Go `stateKey` in internal/browse. */
export type StateKey = `side:${"chat" | "term"}:${string}` | `tab:${string}`;

export interface ConsoleEvent {
  level: string; // log | info | warn | error | debug
  text: string;
  ts: number;
}

export interface NetworkEvent {
  method: string;
  url: string;
  status: number;
  durationMs: number;
  ts: number;
}

/** Server-authoritative navigation update (from the `browse_nav` bus event).
 *  The address bar renders ONLY this — never a page-reported URL (spoofing
 *  defense, see spec § Security model). */
export interface NavEvent {
  state_key: string;
  url: string;
  status: number;
  mode: "local" | "proxied";
  error?: string;
}

export interface BrowserTabState {
  url: string; // authoritative current URL (server-driven once loaded)
  status: number; // last HTTP status (0 = loading)
  loading: boolean;
  mode: "local" | "proxied" | null;
  error: string | null;
  history: string[];
  historyIndex: number;
  panelOpen: boolean;
  collapsed: boolean;
  consoleEvents: ConsoleEvent[];
  networkEvents: NetworkEvent[];
}

interface BrowserState {
  byKey: Record<string, BrowserTabState>;
}

/** Ring-buffer caps for the in-memory telemetry lists (never persisted). */
export const CONSOLE_CAP = 1000;
export const NETWORK_CAP = 1000;

export const browserStore = new Store<BrowserState>({ byKey: {} });

function defaultTab(persistedUrl = ""): BrowserTabState {
  return {
    url: persistedUrl,
    status: 0,
    loading: false,
    mode: null,
    error: null,
    history: persistedUrl ? [persistedUrl] : [],
    historyIndex: persistedUrl ? 0 : -1,
    panelOpen: true,
    collapsed: false,
    consoleEvents: [],
    networkEvents: [],
  };
}

function mutate(key: string, fn: (t: BrowserTabState) => BrowserTabState) {
  browserStore.setState((s) => {
    const cur = s.byKey[key];
    if (!cur) return s;
    return { byKey: { ...s.byKey, [key]: fn(cur) } };
  });
}

function cap<T>(arr: T[], limit: number): T[] {
  return arr.length > limit ? arr.slice(arr.length - limit) : arr;
}

export const browserActions = {
  open(key: StateKey, persistedUrl = "") {
    browserStore.setState((s) => {
      if (s.byKey[key]) {
        return { byKey: { ...s.byKey, [key]: { ...s.byKey[key], panelOpen: true, collapsed: false } } };
      }
      return { byKey: { ...s.byKey, [key]: defaultTab(persistedUrl) } };
    });
  },

  close(key: StateKey) {
    browserStore.setState((s) => {
      const next = { ...s.byKey };
      delete next[key];
      return { byKey: next };
    });
    // Best-effort server revoke; failure is non-fatal (session expires anyway).
    void revokeBrowseSession(key).catch((err) =>
      console.error("browse: revoke failed for", key, err),
    );
  },

  setCollapsed(key: StateKey, collapsed: boolean) {
    mutate(key, (t) => ({ ...t, collapsed }));
  },

  navigate(key: StateKey, url: string) {
    mutate(key, (t) => {
      const trimmed = t.history.slice(0, t.historyIndex + 1);
      trimmed.push(url);
      return { ...t, url, history: trimmed, historyIndex: trimmed.length - 1, loading: true, error: null };
    });
  },

  back(key: StateKey) {
    mutate(key, (t) => (t.historyIndex > 0 ? { ...t, historyIndex: t.historyIndex - 1, url: t.history[t.historyIndex - 1], loading: true } : t));
  },

  forward(key: StateKey) {
    mutate(key, (t) => (t.historyIndex < t.history.length - 1 ? { ...t, historyIndex: t.historyIndex + 1, url: t.history[t.historyIndex + 1], loading: true } : t));
  },

  pushConsole(key: StateKey, ev: ConsoleEvent) {
    mutate(key, (t) => ({ ...t, consoleEvents: cap([...t.consoleEvents, ev], CONSOLE_CAP) }));
  },

  pushNetwork(key: StateKey, ev: NetworkEvent) {
    mutate(key, (t) => ({ ...t, networkEvents: cap([...t.networkEvents, ev], NETWORK_CAP) }));
  },

  clearConsole(key: StateKey) {
    mutate(key, (t) => ({ ...t, consoleEvents: [] }));
  },

  clearNetwork(key: StateKey) {
    mutate(key, (t) => ({ ...t, networkEvents: [] }));
  },

  applyNavEvent(key: string, ev: NavEvent) {
    mutate(key, (t) => ({
      ...t,
      url: ev.url || t.url,
      status: ev.status,
      loading: ev.status === 0,
      mode: ev.mode,
      error: ev.error ?? null,
    }));
  },
};

// revokeBrowseSession lives in client.ts; declared here to avoid a cycle at
// import time. Wired in Step 5.
let revokeBrowseSession: (key: string) => Promise<void> = async () => {};
export function __setRevoker(fn: (key: string) => Promise<void>) {
  revokeBrowseSession = fn;
}

/** React hook: subscribe to one surface's state. Returns undefined when the
 *  panel is closed. */
export function useBrowserTab(key: StateKey): BrowserTabState | undefined {
  return useSelector(browserStore, (s) => s.byKey[key]);
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd web && npx vitest run src/lib/browserStore.test.ts`
Expected: PASS.

- [ ] **Step 5: Write the failing client test**

Create `web/src/api/client.browse.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from "vitest";
import { browseSrc, getBrowseBase, __resetBrowseBaseCache } from "./client";

describe("browseSrc", () => {
  it("builds the /b/ path from a URL and appends the grant on first load", () => {
    const src = browseSrc("http://127.0.0.1:5000", "GRANT123", "tab:x", "https://example.com/foo?q=1");
    expect(src).toBe("http://127.0.0.1:5000/b/tab:x/https/example.com/foo?q=1&__grant=GRANT123");
  });

  it("omits the grant param when grant is null (already authenticated)", () => {
    const src = browseSrc("http://127.0.0.1:5000", null, "tab:x", "http://localhost:5173/");
    expect(src).toBe("http://127.0.0.1:5000/b/tab:x/http/localhost:5173/");
  });
});

describe("getBrowseBase", () => {
  beforeEach(() => __resetBrowseBaseCache());
  it("fetches once and caches", async () => {
    const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ base_url: "http://127.0.0.1:9" }), { status: 200 }),
    );
    expect(await getBrowseBase()).toBe("http://127.0.0.1:9");
    expect(await getBrowseBase()).toBe("http://127.0.0.1:9");
    expect(spy).toHaveBeenCalledTimes(1);
    spy.mockRestore();
  });
});
```

- [ ] **Step 6: Run to verify it fails**

Run: `cd web && npx vitest run src/api/client.browse.test.ts`
Expected: FAIL — `browseSrc`/`getBrowseBase`/`__resetBrowseBaseCache` not exported.

- [ ] **Step 7: Add the client helpers**

Append to `web/src/api/client.ts` (uses the existing `authedFetch` + `apiPath` + `authHeaders` already in the file):

```ts
// ---- Embedded browser (see internal/browse) --------------------------------

let _browseBase: string | null = null;

/** Test-only: clear the cached browse base URL. */
export function __resetBrowseBaseCache(): void {
  _browseBase = null;
}

/** Fetches (once, then cached) the browse-origin base URL from the main
 *  server. The browse origin is a SEPARATE loopback listener so proxied pages
 *  are cross-origin to this SPA. */
export async function getBrowseBase(): Promise<string> {
  if (_browseBase) return _browseBase;
  const res = await authedFetch("/api/browse/config", { method: "GET" });
  if (!res.ok) throw new Error(`browse config: ${res.status}`);
  const body = (await res.json()) as { base_url: string };
  _browseBase = body.base_url;
  return _browseBase;
}

/** Mints a one-time grant for a stateKey; the first iframe navigation carries
 *  it and the browse origin exchanges it for an HttpOnly cookie. */
export async function mintBrowseGrant(stateKey: string): Promise<string> {
  const res = await authedFetch("/api/browse/grant", {
    method: "POST",
    body: JSON.stringify({ state_key: stateKey }),
  });
  if (!res.ok) throw new Error(`browse grant: ${res.status}`);
  const body = (await res.json()) as { grant: string };
  return body.grant;
}

/** Best-effort revoke of a browse session (called on panel close). */
export async function revokeBrowseSession(stateKey: string): Promise<void> {
  await authedFetch("/api/browse/revoke", {
    method: "POST",
    body: JSON.stringify({ state_key: stateKey }),
  });
}

/** Builds the iframe src pointing at the browse origin's stateless route:
 *  {base}/b/{stateKey}/{scheme}/{host}/{path}?{query}[&__grant=...].
 *  Only http/https targets are supported. */
export function browseSrc(base: string, grant: string | null, stateKey: string, url: string): string {
  const u = new URL(url);
  const scheme = u.protocol.replace(":", "");
  const host = u.host; // host:port
  const path = u.pathname === "/" ? "/" : u.pathname;
  let out = `${base}/b/${stateKey}/${scheme}/${host}${path}`;
  const params = u.search ? u.search.slice(1) : "";
  const parts: string[] = [];
  if (params) parts.push(params);
  if (grant) parts.push(`__grant=${encodeURIComponent(grant)}`);
  if (parts.length) out += `?${parts.join("&")}`;
  return out;
}
```

Also add the revoke endpoint on the Go side (main server, next to config/grant from Part 01):

```go
func (s *Server) handleBrowseRevoke(w http.ResponseWriter, r *http.Request) {
	var req struct{ StateKey string `json:"state_key"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.StateKey == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s.browse.Revoke(req.StateKey)
	w.WriteHeader(http.StatusNoContent)
}
```

Register it in `EnableBrowse`: `s.mux.HandleFunc("POST /api/browse/revoke", s.authMiddleware(s.handleBrowseRevoke))`.

- [ ] **Step 8: Wire the revoker into the store (break the import cycle)**

At the bottom of `web/src/lib/browserStore.ts` imports section add a one-time wiring in a module that already runs at startup (e.g. `web/src/main.tsx` or `App.tsx` top-level), NOT inside browserStore.ts (to avoid client→store→client cycle):

```ts
// in web/src/App.tsx (top-level, once):
import { __setRevoker } from "./lib/browserStore";
import { revokeBrowseSession } from "./api/client";
__setRevoker(revokeBrowseSession);
```

- [ ] **Step 9: Run to verify client tests pass**

Run: `cd web && npx vitest run src/api/client.browse.test.ts`
Expected: PASS.

- [ ] **Step 10: Write the failing SSE-routing test**

Add to `web/src/lib/sessionEvents.test.ts` (matches the existing `env(...)` + `routeBusEnvelope` harness there):

```ts
import { browserStore } from "./browserStore";

it("routes browse_nav into the browser store", () => {
  browserStore.setState(() => ({ byKey: { "tab:x": { url: "", status: 0, loading: true, mode: null, error: null, history: [], historyIndex: -1, panelOpen: true, collapsed: false, consoleEvents: [], networkEvents: [] } } }));
  routeBusEnvelope(
    env("browse_nav", { data: { state_key: "tab:x", url: "https://done.com/", status: 200, mode: "proxied" } }),
    makeRouter(), // the test file's existing router factory
  );
  expect(browserStore.state.byKey["tab:x"].url).toBe("https://done.com/");
  expect(browserStore.state.byKey["tab:x"].status).toBe(200);
});
```

(Use the test file's existing router-builder helper name; if it inlines the router object, inline it the same way here.)

- [ ] **Step 11: Run to verify it fails**

Run: `cd web && npx vitest run src/lib/sessionEvents.test.ts -t browse_nav`
Expected: FAIL — `browse_nav` falls through unhandled; store unchanged.

- [ ] **Step 12: Route `browse_nav` in sessionEvents.ts**

In `web/src/lib/sessionEvents.ts`, add the import and a handler branch near the top of `routeBusEnvelope` (alongside the `session_started` / `status` process-global branches, before `routeSessionScoped`):

```ts
import { browserActions, type NavEvent } from "./browserStore";
```

```ts
  if (event === "browse_nav") {
    const nav = data as NavEvent;
    if (nav && nav.state_key) {
      browserActions.applyNavEvent(nav.state_key, nav);
    } else {
      console.error("browse_nav event missing state_key:", data);
    }
    return;
  }
```

Add `"browse_nav"` to `ROUTABLE_EVENTS` so `SessionTabSync` subscribes to it:

```ts
export const ROUTABLE_EVENTS = ["session_started", "status", "browse_nav", ...SESSION_SCOPED_EVENTS];
```

Note: `browse_nav` is process/project-global (keyed by `state_key`, no session id), so it must NOT be added to `SESSION_SCOPED_EVENTS` — handling it before `routeSessionScoped` keeps it out of the session gate.

- [ ] **Step 13: Run to verify pass + full frontend suite for touched files**

Run: `cd web && npx vitest run src/lib/browserStore.test.ts src/api/client.browse.test.ts src/lib/sessionEvents.test.ts`
Expected: PASS.

- [ ] **Step 14: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors in the touched files.

- [ ] **Step 15: Commit**

```bash
git add web/src/lib/browserStore.ts web/src/lib/browserStore.test.ts \
  web/src/api/client.ts web/src/api/client.browse.test.ts \
  web/src/lib/sessionEvents.ts web/src/lib/sessionEvents.test.ts \
  web/src/App.tsx internal/server/server.go
git commit -m "feat(browse): frontend browser store, client helpers, browse_nav SSE routing"
```

## Note for Part 07 (nav events)

This part expects the Go bus event name to be exactly `"browse_nav"` with a payload matching `NavEvent` (`state_key`, `url`, `status`, `mode`, `error`). Part 07 must `Publish("browse_nav", project, "", navEvent)` — SessionID empty (it is not session-scoped). If Part 07 chooses a different event name, update `ROUTABLE_EVENTS` and the switch branch here to match.
