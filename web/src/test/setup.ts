import "@testing-library/jest-dom/vitest";

// Polyfill localStorage for jsdom environments where it is not available
// (Node 22 experimental warning and vitest 3.x jsdom quirks). Ensures
// terminal tests that call window.localStorage.clear() don't throw.
if (typeof HTMLElement !== "undefined" && !HTMLElement.prototype.scrollIntoView) {
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
    configurable: true,
    writable: true,
    value: () => {},
  });
}

// jsdom ships no matchMedia, but real code calls it on first render (the
// `useIsMobile` breakpoint hook used by App and the Pulse assistant window, and
// media-query reads in theme/speech code). Without a default, mounting any of
// those throws "window.matchMedia is not a function" in every suite that does
// not stub it itself — which is a test-environment artifact, not the app's
// behaviour.
//
// Supports the queries the app actually uses: `(max-width: Npx)` and
// `(min-width: Npx)`, TOGETHER when a query has both (a compound
// `(min-width: 768px) and (max-width: 1023px)` must not ignore one bound).
// Evaluated against `window.innerWidth` on READ and on window `resize`, so a
// test can pick a viewport by defining innerWidth before mount and a hook that
// listens for `change` observes the new value. Any other query reports false —
// a `prefers-*` test must stub matchMedia itself.
if (typeof window !== "undefined" && typeof window.matchMedia !== "function") {
  type MatchListener = (event: MediaQueryListEvent) => void;
  const listeners = new Map<string, Set<MatchListener>>();
  const evaluateWidthQuery = (query: string): boolean => {
    const max = /max-width:\s*(\d+)px/.exec(query);
    const min = /min-width:\s*(\d+)px/.exec(query);
    if (!max && !min) return false;
    const width = window.innerWidth;
    if (min && width < Number(min[1])) return false;
    if (max && width > Number(max[1])) return false;
    return true;
  };
  window.addEventListener("resize", () => {
    for (const [query, set] of listeners) {
      const matches = evaluateWidthQuery(query);
      for (const listener of set) {
        listener({ matches, media: query } as MediaQueryListEvent);
      }
    }
  });
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: (query: string): MediaQueryList => {
      const add = (listener: MatchListener) => {
        let set = listeners.get(query);
        if (!set) {
          set = new Set();
          listeners.set(query, set);
        }
        set.add(listener);
      };
      const remove = (listener: MatchListener) => {
        listeners.get(query)?.delete(listener);
      };
      return {
        get matches() {
          return evaluateWidthQuery(query);
        },
        media: query,
        onchange: null,
        addEventListener: (type: string, listener: MatchListener) => {
          if (type === "change") add(listener);
        },
        removeEventListener: (type: string, listener: MatchListener) => {
          if (type === "change") remove(listener);
        },
        // The legacy API is still used by some libraries.
        addListener: add,
        removeListener: remove,
        dispatchEvent: () => false,
      } as unknown as MediaQueryList;
    },
  });
}

if (typeof window !== "undefined" && !window.localStorage) {
  const store = new Map<string, string>();
  const polyfill = {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, String(v)),
    removeItem: (k: string) => void store.delete(k),
    clear: () => void store.clear(),
    key: (i: number) => Array.from(store.keys())[i] ?? null,
    get length() { return store.size; },
  };
  Object.defineProperty(window, "localStorage", { value: polyfill, writable: true, configurable: true });
  // @ts-ignore
  if (typeof globalThis !== "undefined" && !globalThis.localStorage) {
    // @ts-ignore
    globalThis.localStorage = polyfill;
  }
}
