import { Store, useSelector } from "@tanstack/react-store";

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

/** Ring-buffer caps for the in-memory telemetry lists (never persisted).
 *  Newest-relevant order: the arrays are append-ordered and the oldest
 *  entries are dropped past the cap, so consumers render the tail. */
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
