import { useEffect, useState } from "react";
import { connectAgentRunsSSE } from "../api/client";
import type { AgentRun } from "../api/types";

export interface AgentRunsState {
  runs: AgentRun[];
  // loaded becomes true once the SSE stream has delivered its first snapshot
  // (the server sends an initial frame on connect, even when there are no
  // runs). Until then `runs` is the empty placeholder — callers must not
  // treat an empty tree as "no runs" while loaded is false.
  loaded: boolean;
}

interface SharedConnection {
  close: () => void;
  listeners: Set<(runs: AgentRun[]) => void>;
  latest: AgentRun[] | null;
}

// AgentPreview and AgentsPanel both mount useAgentRuns for the same session at
// once. Each independently opening `connectAgentRunsSSE` doubled the SSE
// connections in flight; on plain HTTP (no TLS, so no h2 multiplexing) that
// eats into Chrome's 6-connections-per-origin cap and can starve/delay other
// streams (chat mirror, log tab). Share one EventSource per session key and
// fan its frames out to every subscriber instead.
const connections = new Map<string, SharedConnection>();

function subscribe(
  sessionId: string | null,
  listener: (runs: AgentRun[]) => void,
): () => void {
  const key = sessionId ?? "";
  let conn = connections.get(key);
  if (!conn) {
    const entry: SharedConnection = { close: () => {}, listeners: new Set(), latest: null };
    entry.close = connectAgentRunsSSE(sessionId ?? undefined, (runs) => {
      entry.latest = runs;
      entry.listeners.forEach((l) => l(runs));
    });
    connections.set(key, entry);
    conn = entry;
  }
  conn.listeners.add(listener);
  if (conn.latest) listener(conn.latest);

  return () => {
    conn.listeners.delete(listener);
    if (conn.listeners.size === 0) {
      conn.close();
      connections.delete(key);
    }
  };
}

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
    return subscribe(realSessionId, (next) => {
      setRuns(next);
      setLoaded(true);
    });
  }, [realSessionId]);

  return { runs, loaded };
}
