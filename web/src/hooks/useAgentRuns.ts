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

// useAgentRuns subscribes to the live agent-run tree for the given session and
// returns the current snapshot. The stream pushes a full tree on every change.
export function useAgentRuns(sessionId: string | null): AgentRunsState {
  const [runs, setRuns] = useState<AgentRun[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    // Reset when the session changes so stale runs don't linger.
    setRuns([]);
    setLoaded(false);
    const cleanup = connectAgentRunsSSE(sessionId ?? undefined, (next) => {
      setRuns(next);
      setLoaded(true);
    });
    return cleanup;
  }, [sessionId]);

  return { runs, loaded };
}
