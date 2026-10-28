import { useEffect, useState } from "react";

// One interval per period, shared by every subscriber and alive only while it
// has at least one. A per-component setInterval made every running row (and
// everything under it) re-render on its own timer.
interface Ticker {
  listeners: Set<(now: number) => void>;
  timer: ReturnType<typeof setInterval>;
}
const tickers = new Map<number, Ticker>();

function subscribe(intervalMs: number, listener: (now: number) => void): () => void {
  let ticker = tickers.get(intervalMs);
  if (!ticker) {
    const listeners = new Set<(now: number) => void>();
    const timer = setInterval(() => {
      const now = Date.now();
      listeners.forEach((l) => l(now));
    }, intervalMs);
    ticker = { listeners, timer };
    tickers.set(intervalMs, ticker);
  }
  const owner = ticker;
  owner.listeners.add(listener);
  return () => {
    owner.listeners.delete(listener);
    if (owner.listeners.size === 0) {
      clearInterval(owner.timer);
      tickers.delete(intervalMs);
    }
  };
}

/** Date.now(), refreshed every `intervalMs` from a ticker shared across components. */
export function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => subscribe(intervalMs, setNow), [intervalMs]);
  return now;
}

/** Number of live shared intervals; exists so tests can prove they are torn down. */
export function activeTickerCount(): number {
  return tickers.size;
}
