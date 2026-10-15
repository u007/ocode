import { Store, useSelector } from "@tanstack/react-store";
import { readPref, writePref } from "./pulseStorage";

/**
 * Height of the live stream on Pulse cards: the default cap or the tall one (see
 * STREAM_TALL_MAX_H in PulseCard).
 *
 * `tall` is the per-viewer default. The toolbar's "Expand active" and "Collapse
 * all" set it for every live card at once, and each write is mirrored to
 * localStorage so it survives a reload. A `storage` event from another tab
 * updates `tall` here too, so two open Pulse tabs agree on the default.
 *
 * `overrides` holds the cards the viewer toggled by hand since the last bulk
 * action, keyed by session id. A card shows `overrides[id]` when present, else
 * `tall`. A bulk action clears the whole map, which is how it reaches a card the
 * user had already toggled. Overrides are in memory only, so they outlive a view
 * switch but not a reload. PulseView prunes the map against the listed rows, so
 * a session that has left the board does not keep its entry.
 */

const KEY_TALL = "pulse.card.streamTall";

export interface StreamPrefs {
  tall: boolean;
  overrides: Record<string, boolean>;
}

function readTall(): boolean {
  return readPref(KEY_TALL) === "1";
}

export const streamPrefsStore = new Store<StreamPrefs>({ tall: readTall(), overrides: {} });

window.addEventListener("storage", (e: StorageEvent) => {
  if (e.key !== KEY_TALL) return;
  streamPrefsStore.setState((s) => ({ ...s, tall: e.newValue === "1" }));
});

/** Sets the height of every live card's stream at once, dropping every manual override. */
export function setAllLiveStreamsTall(tall: boolean): void {
  streamPrefsStore.setState(() => ({ tall, overrides: {} }));
  writePref(KEY_TALL, tall ? "1" : "0");
}

/** One card's manual toggle. Lasts until the next bulk action. */
export function setStreamTall(sessionId: string, tall: boolean): void {
  streamPrefsStore.setState((s) => ({ ...s, overrides: { ...s.overrides, [sessionId]: tall } }));
}

/** Drops overrides for sessions no longer listed. */
export function pruneStreamOverrides(listed: ReadonlySet<string>): void {
  streamPrefsStore.setState((s) => {
    const stale = Object.keys(s.overrides).filter((id) => !listed.has(id));
    if (stale.length === 0) return s;
    const overrides = { ...s.overrides };
    for (const id of stale) delete overrides[id];
    return { ...s, overrides };
  });
}

/** The current value, read outside React. */
export function getStreamPrefs(): StreamPrefs {
  return streamPrefsStore.state;
}

/** Whether one card's stream is tall: its override when it has one, else the viewer's default. */
export function useStreamTall(sessionId: string): boolean {
  return useSelector(streamPrefsStore, (s) => (sessionId in s.overrides ? s.overrides[sessionId] : s.tall));
}

/** Test-only: re-read the persisted value and drop every override, as a fresh page load would. */
export function resetStreamPrefsForTests(): void {
  streamPrefsStore.setState(() => ({ tall: readTall(), overrides: {} }));
}
