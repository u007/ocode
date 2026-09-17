import { useEffect, useMemo, useState } from "react";
import { api } from "../api/client";
import { eventBus } from "../lib/eventBus";
import { getTrustedTerminalProject } from "../lib/trustedProject";
import { findProjectPathForTab, useProjectState } from "../stores/projectStore";
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
// once) share the seed fetch instead of doubling it. Entries are keyed by
// `host::session` so a remote session and a local session that share an id
// (ids are random per server, but the cache must not be the thing that
// conflates them) never seed each other's tree.
const latestCache = new Map<string, AgentRun[]>();
// Active-consumer count per session id. The cache exists so simultaneously
// mounted consumers (AgentPreview rail + AgentsPanel) share one seed fetch;
// when the last consumer unmounts (session tab closed) its entry is dropped,
// otherwise run trees would accumulate for every session ever viewed.
const cacheRefs = new Map<string, number>();

// useAgentRuns subscribes to the live agent-run tree for the given session and
// returns the current snapshot. The stream pushes a full tree on every change.
// The per-host stream is opened by App via `eventBus.setHosts`; frames from
// every host feed the same subscriber path, routed by `session_id` (ids are
// random per server, so hosts never collide). The seed fetch, however, must go
// to the session's own host so a remote project's runs come from the remote
// server.
export function useAgentRuns(sessionId: string | null): AgentRunsState {
  const [runs, setRuns] = useState<AgentRun[]>([]);
  const [loaded, setLoaded] = useState(false);
  const { state: projectState } = useProjectState();
  // A `new-*` id is a temp tab id, not a real session yet — there is nothing
  // on the server to stream until the first message creates a session.
  const realSessionId = sessionId && !sessionId.startsWith("new-") ? sessionId : null;
  // Resolve the seed route for this session. `resolveSessionHost` alone cannot
  // distinguish "project is local" (host undefined; a local fetch is correct)
  // from "project missing or ambiguous in the snapshot" (`known:false`), and a
  // collapsed cache key let a restored remote tab permanently seed the LOCAL
  // server with a remote session id. Resolve the tab's project path first,
  // then apply the same single-match trust rule; an unresolved project skips
  // the fetch entirely and waits for the snapshot (see the effect below).
  const route = useMemo(() => {
    if (!realSessionId) return { host: undefined, unresolved: false, projectPath: undefined };
    const projectPath = findProjectPathForTab(projectState, realSessionId);
    if (!projectPath) return { host: undefined, unresolved: false, projectPath: undefined };
    const trusted = getTrustedTerminalProject(projectState.projects, projectPath);
    if (!trusted.known) return { host: undefined, unresolved: true, projectPath };
    return { host: trusted.host, unresolved: false, projectPath };
  }, [realSessionId, projectState]);
  const host = route.host;
  const cacheKey = realSessionId ? `${host ?? ""}::${realSessionId}` : null;

  useEffect(() => {
    // Reset when the session changes so stale runs don't linger.
    setRuns([]);
    setLoaded(false);
    if (!realSessionId || !cacheKey) return;
    if (route.unresolved) {
      // The tab exists but its project is absent from (or ambiguous in) the
      // saved snapshot, so there is no way to know whether its runs live
      // locally or on a remote host. Seeding the local server here would cache
      // the remote session's tree under the local key permanently. Leave
      // `loaded` false, log what was attempted, and let the effect re-run once
      // the project snapshot resolves.
      console.warn("useAgentRuns: deferring seed for unresolved project", {
        sessionId: realSessionId,
        projectPath: route.projectPath,
      });
      return;
    }

    cacheRefs.set(cacheKey, (cacheRefs.get(cacheKey) ?? 0) + 1);

    const cached = latestCache.get(cacheKey);
    if (cached) {
      setRuns(cached);
      setLoaded(true);
    }

    let cancelled = false;
    const seed = () => {
      api
        .listAgentRuns(realSessionId, host)
        .then((next) => {
          if (cancelled) return;
          latestCache.set(cacheKey, next);
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
      latestCache.set(cacheKey, next);
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
      const refs = (cacheRefs.get(cacheKey) ?? 1) - 1;
      if (refs <= 0) {
        cacheRefs.delete(cacheKey);
        latestCache.delete(cacheKey);
      } else {
        cacheRefs.set(cacheKey, refs);
      }
    };
  }, [realSessionId, cacheKey, host, route.unresolved, route.projectPath]);

  return { runs, loaded };
}
