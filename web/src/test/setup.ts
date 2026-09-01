import "@testing-library/jest-dom/vitest";

// Polyfill localStorage for jsdom environments where it is not available
// (Node 22 experimental warning and vitest 3.x jsdom quirks). Ensures
// terminal tests that call window.localStorage.clear() don't throw.
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
