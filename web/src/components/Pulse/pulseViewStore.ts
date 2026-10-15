import { Store, useSelector } from "@tanstack/react-store";

/**
 * Pulse dashboard UI state that must survive leaving the view. App mounts
 * PulseView only while `activeView === "pulse"`, so anything held in its own
 * `useState` is discarded on every switch away, and so is each card's pin.
 *
 * In memory on purpose: it outlives the unmount but not a reload, which resets it.
 * Nothing here is worth a localStorage round-trip. The stream height is different
 * (see pulseStreamPrefs), since it is a layout choice that should persist.
 *
 * Pins are keyed by session id. PulseView prunes them against the listed rows, so
 * a session that has left the board does not keep its entry.
 */
export interface PulseViewState {
  /** The project/title filter typed into the toolbar. */
  filter: string;
  /** Session ids whose details panel is pinned open. Absent means not pinned. */
  pinned: Record<string, true>;
}

export const pulseViewStore = new Store<PulseViewState>({ filter: "", pinned: {} });

export function setPulseFilter(filter: string): void {
  pulseViewStore.setState((s) => ({ ...s, filter }));
}

export function setPulsePinned(sessionId: string, pinned: boolean): void {
  pulseViewStore.setState((s) => {
    if ((s.pinned[sessionId] === true) === pinned) return s;
    const next = { ...s.pinned };
    if (pinned) next[sessionId] = true;
    else delete next[sessionId];
    return { ...s, pinned: next };
  });
}

/** Drops pins for sessions no longer listed. */
export function prunePulsePins(listed: ReadonlySet<string>): void {
  pulseViewStore.setState((s) => {
    const stale = Object.keys(s.pinned).filter((id) => !listed.has(id));
    if (stale.length === 0) return s;
    const pinned = { ...s.pinned };
    for (const id of stale) delete pinned[id];
    return { ...s, pinned };
  });
}

export function usePulseFilter(): string {
  return useSelector(pulseViewStore, (s) => s.filter);
}

export function usePulsePinned(sessionId: string): boolean {
  return useSelector(pulseViewStore, (s) => s.pinned[sessionId] === true);
}

/** Test-only: back to the initial state, so one test's filter or pin cannot leak into the next. */
export function resetPulseViewStoreForTests(): void {
  pulseViewStore.setState(() => ({ filter: "", pinned: {} }));
}
