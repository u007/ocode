import { useEffect, useState } from "react";
import { api } from "../api/client";
import { eventBus } from "../lib/eventBus";
import type { AgentRun } from "../api/types";

export interface AgentRunsState {
  runs: AgentRun[];
  // loaded becomes true once the run tree has been delivered — either by the
  // initial fetch (the bus only publishes on change, so a fresh subscriber
  // must seed itself) or by a `runs` event. Until then `runs` is the empty
  // placeholder — callers must not treat an empty tree as "no runs" while
  // loaded is false.
  loaded: boolean;
}

// The bus only publishes a `runs` envelope when a session's serialized tree
// *changes*, so a component mounting after the last change would never see a
// frame. Seed with one fetch per session, then patch from events. The cache
// lets AgentPreview and AgentsPanel (both mounted for the same session at
// once) share the seed fetch instead of doubling it.
const latestCache = new Map<string, AgentRun[]>();
// Active-consumer count per session id. The cache exists so simultaneously
// mounted consumers (AgentPreview rail + AgentsPanel) share one seed fetch;
// when the last consumer unmounts (session tab closed) its entry is dropped,
// otherwise run trees would accumulate for every session ever viewed.
const cacheRefs = new Map<string, number>();

// useAgentRuns subscribes to the live agent-run tree for the given session and
// returns the current snapshot. The stream pushes a full tree on every change.
export function useAgentRuns(sessionId: string | null): AgentRunsState {
  const [runs, setRuns] = useState<AgentRun[]>([]);
  const [loaded, setLoaded] = useState(false);
  // A `new-*` id is a temp tab id, not a real session yet — there is nothing
  // on the server to stream until the first message creates a session.
  const realSessionId = sessionId && !sessionId.startsWith("new-") ? sessionId : null;

  useEffect(() => {
    // Reset when the session changes so stale runs don't linger.
    setRuns([]);
    setLoaded(false);
    if (!realSessionId) return;

    cacheRefs.set(realSessionId, (cacheRefs.get(realSessionId) ?? 0) + 1);

    const cached = latestCache.get(realSessionId);
    if (cached) {
      setRuns(cached);
      setLoaded(true);
    }

    let cancelled = false;
    const seed = () => {
      api
        .listAgentRuns(realSessionId)
        .then((next) => {
          if (cancelled) return;
          latestCache.set(realSessionId, next);
          setRuns(next);
          setLoaded(true);
        })
        .catch((err) => {
          // Not fatal — the next `runs` event or reconnect seed will retry.
          if (!cancelled) console.warn("failed to fetch agent runs", err);
        });
    };
    if (!cached) seed();

    const off = eventBus.on("runs", (env) => {
      if (env.session_id !== realSessionId) return;
      const next = env.data as AgentRun[];
      latestCache.set(realSessionId, next);
      setRuns(next);
      setLoaded(true);
    });
    // After a reconnect the server may have produced runs while we were
    // disconnected — reseed from the API (events since then are not replayed).
    const offReconnect = eventBus.onReconnect(seed);

    return () => {
      cancelled = true;
      off();
      offReconnect();
      const refs = (cacheRefs.get(realSessionId) ?? 1) - 1;
      if (refs <= 0) {
        cacheRefs.delete(realSessionId);
        latestCache.delete(realSessionId);
      } else {
        cacheRefs.set(realSessionId, refs);
      }
    };
  }, [realSessionId]);

  return { runs, loaded };
}
