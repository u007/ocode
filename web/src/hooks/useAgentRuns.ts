import { useEffect, useState } from "react";
import { connectAgentRunsSSE } from "../api/client";
import type { AgentRun } from "../api/types";

// useAgentRuns subscribes to the live agent-run tree for the given session and
// returns the current snapshot. The stream pushes a full tree on every change.
export function useAgentRuns(sessionId: string | null): AgentRun[] {
  const [runs, setRuns] = useState<AgentRun[]>([]);

  useEffect(() => {
    // Reset when the session changes so stale runs don't linger.
    setRuns([]);
    const cleanup = connectAgentRunsSSE(sessionId ?? undefined, setRuns);
    return cleanup;
  }, [sessionId]);

  return runs;
}
