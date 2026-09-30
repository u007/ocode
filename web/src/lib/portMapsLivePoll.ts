import type { PortMapView } from "@/api/types";

/** Client-side companion to the server's BACKGROUND auto-start.
 *
 *  `HandleListPortMaps` answers from persisted state immediately and opens the
 *  project's forwards on its own goroutine, so the first list legitimately
 *  reports `live: false` for a forward that is still coming up — a tunnel that
 *  cannot be opened spends ~5s in the server's readiness probe
 *  (`internal/remote` `waitForTunnelReady`), and forwards open one after
 *  another. The panel therefore re-fetches while an enabled forward is pending.
 *
 *  Two rules keep that from becoming an open-ended poll:
 *  - a DISABLED forward is never pending. The user asked for it to be down, so
 *    re-fetching would never converge;
 *  - the attempt budget is finite, so a forward that never comes up (host down)
 *    settles on a truthful "enabled, not live" row instead of polling forever.
 *
 *  Pure functions: the component owns the timer, this owns the decision, so the
 *  budget is testable without waiting it out. */

/** Re-check cadence while a forward is still opening. */
export const LIVE_POLL_INTERVAL_MS = 1200;

/** Ceiling on re-checks per dialog open (~14s), covering several sequentially
 *  opening forwards without polling forever. Reset whenever the dialog opens or
 *  the active project changes. */
export const LIVE_POLL_MAX_ATTEMPTS = 12;

/** The enabled forwards that are not live yet — i.e. what a re-fetch could
 *  still change. */
export function pendingLiveForwards(maps: PortMapView[]): PortMapView[] {
  return maps.filter((m) => m.enabled && !m.live);
}

/** Whether another re-fetch is warranted. `attempts` is how many have already
 *  been made since the dialog opened. */
export function shouldPollForLive(maps: PortMapView[], attempts: number): boolean {
  if (attempts >= LIVE_POLL_MAX_ATTEMPTS) return false;
  return pendingLiveForwards(maps).length > 0;
}
