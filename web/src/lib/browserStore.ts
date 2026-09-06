import { Store, useSelector } from "@tanstack/react-store";
import { normalizeBrowseURL } from "./browseURL";
// Same function the client's browseSrc uses, so stored URLs and proxied URLs
// never diverge. Re-exported for existing importers of browserStore.
export { normalizeBrowseURL };

/** Routing predicate shared with the Go `hostIsLiteralPrivate`: the host of a
 *  URL is local (reverse-proxy iframe) when it is a loopback/RFC1918/CGNAT
 *  literal or a `*.localhost` name. Single-label names like "mybox" are
 *  hostnames — they route to Chrome and are blocked at egress if they resolve
 *  private (same as the Go router). Expects the host WITH its `:port` suffix
 *  and IPv6 brackets intact, exactly as `new URL(u).host` yields. */
export function isPrivateHost(host: string): boolean {
  const h = host.toLowerCase();
  if (h === "localhost" || h.endsWith(".localhost")) return true;
  if (h === "[::1]") return true;
  // Strip a port suffix: "127.0.0.1:80" → "127.0.0.1". IPv6 with port keeps
  // its brackets ("[::1]:5173" → "[::1]"); bare "::1" handled above and by
  // the bracket forms in PRIVATE_HOST_RE.
  const bare = h.replace(/:\d+$/, "");
  return privateHostRe.test(bare);
}

// Mirrors browseURL's PRIVATE_HOST_RE but accepts the port-stripped bare host.
const privateHostRe =
  /^(localhost|[^/]*\.localhost|127(?:\.\d{1,3}){3}|0\.0\.0\.0|10(?:\.\d{1,3}){3}|192\.168(?:\.\d{1,3}){2}|172\.(?:1[6-9]|2\d|3[01])(?:\.\d{1,3}){2}|\[::1\]|\[[^\]]*\])$/;

/** Composite key identifying one browser surface's state. Side panels are
 *  keyed by their host chat/terminal tab; standalone browser tabs by their
 *  own id. Mirrors the Go `stateKey` in internal/browse. */
export type StateKey = `side:${"chat" | "term"}:${string}` | `tab:${string}`;

export interface ConsoleEvent {
  level: string; // log | info | warn | error | debug
  text: string;
  ts: number;
}

export interface ResponseBody {
  body: string;
  base64Encoded: boolean;
  truncated: boolean;
  error?: string;
}

export interface NetworkEvent {
  requestId: string;
  method: string;
  url: string;
  status: number;
  durationMs: number;
  ts: number;
  blocked?: string;
  contentType?: string;
  size?: number;
  requestHeaders?: Record<string, string>;
  responseHeaders?: Record<string, string>;
  postData?: string;
}

/** Server-authoritative navigation update (from the `browse_nav` bus event).
 *  The address bar renders ONLY this — never a page-reported URL (spoofing
 *  defense, see spec § Security model). */
export interface NavEvent {
  state_key: string;
  url: string;
  status: number;
  mode: BrowseMode;
  error?: string;
}

/** Server-authoritative page-title update (from the `browse_title` bus
 *  event, fed by Chrome targetInfo titles). The tab strip renders this —
 *  never the address bar. `url` is the page URL from the same payload; the
 *  title applies only when it matches the surface's current URL. */
export interface TitleEvent {
  state_key: string;
  title: string;
  url?: string;
}

/** Rendering mode for one browser surface: local = reverse-proxy iframe
 *  (private hosts), chrome = headless-Chrome screencast (public hosts). */
export type BrowseMode = "local" | "chrome";

export interface BrowserTabState {
  url: string; // authoritative current URL (server-driven once loaded)
  status: number; // last HTTP status (0 = loading)
  loading: boolean;
  mode: BrowseMode | null;
  /** User-forced viewport override (e.g. dev-server pages → Chrome/CDP).
   *  Server nav events keep updating `mode`; this only biases which surface
   *  renders until the user exits the override. */
  userMode: BrowseMode | null;
  error: string | null;
  history: string[];
  historyIndex: number;
  panelOpen: boolean;
  collapsed: boolean;
  consoleEvents: ConsoleEvent[];
  networkEvents: NetworkEvent[];
  responseBodies: Record<string, ResponseBody>;
  pageTitle: string | null;
  /** Last reported vertical scroll offset (CSS px) for the current URL. */
  scrollY: number;
  /** Persisted scroll offsets keyed by normalized page URL (restore on
   *  reload, back/forward, and app restart). */
  scrollByUrl: Record<string, number>;
  perfMetrics: Record<string, number>;
  /** Live-metrics collection state (CDP Performance domain). Authoritative
   *  value arrives from the server (`perfState` on attach + toggle acks);
   *  defaults true to match the backend's enable-on-attach. Stop only pauses
   *  collection — the last snapshot is kept, never erased. */
  perfRecording: boolean;
}

interface BrowserState {
  byKey: Record<string, BrowserTabState>;
}

/** Ring-buffer caps for the in-memory telemetry lists (never persisted).
 *  Newest-relevant order: the arrays are append-ordered and the oldest
 *  entries are dropped past the cap, so consumers render the tail. */
export const CONSOLE_CAP = 1000;
export const NETWORK_CAP = 1000;

export const browserStore = new Store<BrowserState>({ byKey: {} });

function defaultTab(persistedUrl = ""): BrowserTabState {
  persistedUrl = normalizeBrowseURL(persistedUrl);
  return {
    url: persistedUrl,
    status: 0,
    loading: false,
    mode: null,
    userMode: null,
    error: null,
    history: persistedUrl ? [persistedUrl] : [],
    historyIndex: persistedUrl ? 0 : -1,
    panelOpen: true,
    collapsed: false,
    consoleEvents: [],
    networkEvents: [],
    responseBodies: {},
    pageTitle: null,
    scrollY: 0,
    scrollByUrl: {},
    perfMetrics: {},
    perfRecording: true,
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

  setUserMode(key: StateKey, mode: BrowseMode | null) {
    mutate(key, (t) => ({ ...t, userMode: mode }));
  },

  navigate(key: StateKey, url: string) {
    url = normalizeBrowseURL(url);
    mutate(key, (t) => {
      const trimmed = t.history.slice(0, t.historyIndex + 1);
      trimmed.push(url);
      return { ...t, url, history: trimmed, historyIndex: trimmed.length - 1, loading: true, error: null, pageTitle: null, scrollY: t.scrollByUrl[url] ?? 0 };
    });
  },

  back(key: StateKey) {
    mutate(key, (t) => {
      if (t.historyIndex <= 0) return t;
      const url = t.history[t.historyIndex - 1];
      return { ...t, historyIndex: t.historyIndex - 1, url, loading: true, pageTitle: null, scrollY: t.scrollByUrl[url] ?? 0 };
    });
  },

  forward(key: StateKey) {
    mutate(key, (t) => {
      if (t.historyIndex >= t.history.length - 1) return t;
      const url = t.history[t.historyIndex + 1];
      return { ...t, historyIndex: t.historyIndex + 1, url, loading: true, pageTitle: null, scrollY: t.scrollByUrl[url] ?? 0 };
    });
  },

  /** Apply a page title (tab strip only). An empty title is an explicit
   *  clear (page removed its `<title>`): the strip falls back to the manual
   *  rename or "New tab" instead of sticking on a stale title. When the
   *  event carries a URL it must match the surface's current URL or the
   *  update is stale (closed-tab echo, pre-navigation title) and dropped —
   *  clears included. */
  setPageTitle(key: string, title: string, url?: string) {
    const clean = (title ?? "").trim() || null;
    mutate(key, (t) => {
      if (url) {
        try {
          if (normalizeBrowseURL(url) !== t.url) return t;
        } catch {
          return t;
        }
      }
      if (t.pageTitle === clean) return t;
      return { ...t, pageTitle: clean };
    });
  },

  /** Record a scroll offset for the surface's current URL (or an explicit
   *  one). The per-URL map is what survives reloads and restarts. */
  setScrollY(key: string, y: number, url?: string) {
    if (!Number.isFinite(y) || y < 0) return;
    mutate(key, (t) => {
      const at = url ?? t.url;
      if (!at) return t;
      if (t.scrollByUrl[at] === y && t.scrollY === y) return t;
      return { ...t, scrollY: y, scrollByUrl: { ...t.scrollByUrl, [at]: y } };
    });
  },

  /** Restore a persisted surface wholesale (app-restart path). */
  restoreSurface(key: string, snap: {
    url: string;
    history: string[];
    historyIndex: number;
    userMode: BrowseMode | null;
    pageTitle: string | null;
    scrollByUrl: Record<string, number>;
  }) {
    browserStore.setState((s) => ({
      byKey: {
        ...s.byKey,
        [key]: {
          ...defaultTab(snap.url),
          history: snap.history,
          historyIndex: snap.historyIndex,
          userMode: snap.userMode,
          pageTitle: snap.pageTitle,
          scrollY: snap.scrollByUrl[snap.url] ?? 0,
          scrollByUrl: snap.scrollByUrl,
        },
      },
    }));
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

  setResponseBody(key: StateKey, requestId: string, body: ResponseBody) {
    mutate(key, (t) => ({
      ...t,
      responseBodies: { ...t.responseBodies, [requestId]: body },
    }));
  },

  setPerformanceMetrics(key: StateKey, metrics: Record<string, number>) {
    mutate(key, (t) => ({ ...t, perfMetrics: metrics }));
  },

  setPerfRecording(key: StateKey, recording: boolean) {
    mutate(key, (t) => ({ ...t, perfRecording: recording }));
  },

  clearPerformance(key: StateKey) {
    mutate(key, (t) => ({ ...t, perfMetrics: {} }));
  },

  setError(key: StateKey, error: string) {
    mutate(key, (t) => ({ ...t, loading: false, error }));
  },

  applyNavEvent(key: string, ev: NavEvent) {
    mutate(key, (t) => {
      const url = ev.url || t.url;
      return {
        ...t,
        url,
        status: ev.status,
        loading: ev.status === 0,
        mode: ev.mode,
        error: ev.error ?? null,
        // A new document drops the previous page title; same-URL status
        // updates (loading → loaded) keep it.
        pageTitle: url !== t.url ? null : t.pageTitle,
        scrollY: url !== t.url ? (t.scrollByUrl[url] ?? 0) : t.scrollY,
      };
    });
  },
};

// revokeBrowseSession lives in client.ts; declared here to avoid a cycle at
// import time. Wired via __setRevoker from App.tsx at startup.
let revokeBrowseSession: (key: string) => Promise<void> = async () => {
  // intentionally not wired yet (startup race window only): the real revoker
  // is installed by App.tsx before any panel can close; until then the server
  // grant/cookie expires on its own.
};
export function __setRevoker(fn: (key: string) => Promise<void>) {
  revokeBrowseSession = fn;
}

/** React hook: subscribe to one surface's state. Returns undefined when the
 *  panel is closed. */
export function useBrowserTab(key: StateKey): BrowserTabState | undefined {
  return useSelector(browserStore, (s) => s.byKey[key]);
}

/** INDEX/Part 09 naming: the same selector hook under the contract name. */
export const useBrowserStore = useBrowserTab;

/** INDEX/Part 09 naming: hook-returning accessor for the action object (the
 *  actions are a stable module singleton; the hook is API-shape sugar). */
export function useBrowserActions(): typeof browserActions {
  return browserActions;
}
