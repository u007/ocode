// window.ocodeDebug helper — auto-imported from main.tsx.
// Exposes methods for inspecting sessions, agent runs, projects, and runtime
// state from the browser DevTools console.

import { authHeaders, apiPath } from './api/client';
import type { SessionInfo, TUIStatus, LSPStatus, AgentRun, Project } from './api/types';

// Local types not in api/types.ts
interface RuntimeStats {
  heap_alloc_bytes: number;
  heap_sys_bytes: number;
  sys_bytes: number;
  num_goroutine: number;
  num_gc: number;
  uptime: string;
}

interface LogEntry {
  kind: string;
  message: string;
  session_id?: string;
}

async function debugFetch<T>(path: string): Promise<T> {
  const resp = await fetch(apiPath(path), { headers: authHeaders() });
  if (!resp.ok) throw new Error(`ocodeDebug: ${path} returned ${resp.status}`);
  return resp.json();
}

// Bounded concurrency helper (max N simultaneous fetches)
async function boundedAll<T>(
  items: T[],
  fn: (item: T) => Promise<unknown>,
  max = 5,
): Promise<PromiseSettledResult<unknown>[]> {
  const results: PromiseSettledResult<unknown>[] = [];
  for (let i = 0; i < items.length; i += max) {
    const batch = items.slice(i, i + max);
    results.push(...(await Promise.allSettled(batch.map(fn))));
  }
  return results;
}

const ocodeDebug = {
  async sessions(): Promise<SessionInfo[]> {
    const data = await debugFetch<{ sessions: SessionInfo[] }>('/api/sessions');
    console.table(data.sessions);
    return data.sessions;
  },

  async sessionStatus(id: string): Promise<TUIStatus> {
    const status = await debugFetch<TUIStatus>(
      `/api/sessions/${encodeURIComponent(id)}/status`,
    );
    console.log(status);
    return status;
  },

  async agentRuns(sessionId?: string): Promise<AgentRun[]> {
    if (sessionId) {
      const runs = await debugFetch<AgentRun[]>(
        `/api/agents/runs?session=${encodeURIComponent(sessionId)}`,
      );
      console.table(
        runs.map((r) => ({ id: r.id, name: r.name, status: r.status, model: r.model })),
      );
      return runs;
    }
    // No sessionId: fetch all sessions, then fetch runs for each (bounded parallel, max 5).
    // Note: only sessions with in-memory agents return runs; persisted sessions return empty.
    // RC-mode caveat: when an RC bridge is active, every session query returns the
    // RC agent's runs (the bridge agent is returned before per-session lookup).
    // Deduplicate by run ID to avoid showing the same tree multiple times.
    const { sessions } = await debugFetch<{ sessions: SessionInfo[] }>('/api/sessions');
    const results = await boundedAll(
      sessions,
      (s) =>
        debugFetch<AgentRun[]>(
          `/api/agents/runs?session=${encodeURIComponent(s.id)}`,
        ),
    );
    const seen = new Set<string>();
    const all = results
      .filter(
        (r): r is PromiseFulfilledResult<AgentRun[]> => r.status === 'fulfilled',
      )
      .flatMap((r) => r.value)
      .filter((r) => r.id && !seen.has(r.id) && seen.add(r.id));
    const failed = results.filter((r) => r.status === 'rejected');
    if (failed.length > 0) {
      console.warn(
        `ocodeDebug: ${failed.length}/${sessions.length} session fetches failed`,
      );
    }
    console.table(
      all.map((r) => ({ id: r.id, name: r.name, status: r.status, model: r.model })),
    );
    return all;
  },

  async projects(): Promise<Project[]> {
    const projects = await debugFetch<Project[]>('/api/projects');
    console.table(projects);
    return projects;
  },

  async runtime(): Promise<RuntimeStats> {
    const stats = await debugFetch<RuntimeStats>('/api/debug/runtime');
    console.log(stats);
    return stats;
  },

  async status(): Promise<TUIStatus> {
    const status = await debugFetch<TUIStatus>('/api/tui-status');
    console.log(status);
    return status;
  },

  async lspStatus(): Promise<LSPStatus[]> {
    const data = await debugFetch<{ lsp_servers: LSPStatus[] }>('/api/lsp/statuses');
    console.table(data.lsp_servers);
    return data.lsp_servers;
  },

  async logs(limit = 50): Promise<LogEntry[]> {
    const logs = await debugFetch<LogEntry[]>('/api/logs');
    // limit <= 0 means all logs; slice(-0) returns entire array
    const sliced = limit > 0 ? logs.slice(-limit).reverse() : logs.slice().reverse();
    console.table(sliced);
    return sliced;
  },

  help() {
    console.log(`
ocodeDebug — ocode agent system inspector

  ocodeDebug.sessions()         List all sessions
  ocodeDebug.sessionStatus(id)  Session status (global fields + session identity)
  ocodeDebug.agentRuns(sid?)    Agent run tree (all sessions if no sid)
  ocodeDebug.projects()         Saved project paths
  ocodeDebug.runtime()          Go runtime stats (heap, goroutines)
  ocodeDebug.status()           Global TUI status snapshot
  ocodeDebug.lspStatus()        Running language servers
  ocodeDebug.logs(limit?)       Debug log entries (default 50, most recent first)
  ocodeDebug.help()             This message
    `);
  },
};

// Expose globally
declare global {
  interface Window {
    ocodeDebug: typeof ocodeDebug;
  }
}
window.ocodeDebug = ocodeDebug;

// TEMPORARY diagnostic (2026-08-27): a residual, un-identified
// Date.prototype.toLocaleString() call keeps showing up in native `sample`
// profiles during real streaming sessions (icu::DateFormatSymbols /
// resolveLocale — full ICU formatter construction) after the known
// StatusBar.tsx hot path was fixed and memoized. Native sampling can't
// symbolicate the calling JS frame, so this captures the actual call site
// from inside the JS engine instead: patches toLocaleString once, grabs a
// stack trace on the first few calls, and ships each one to the backend log
// via the existing frontend-stats debug pipe (readable through
// GET /api/logs) — an SSE/console round-trip a DevTools-less Wails window
// can't otherwise surface. Remove this whole block (and the
// debug_note field in internal/server/frontend_stats.go) once the call
// site is found and fixed.
(() => {
  const MAX_CAPTURES = 3;
  let captured = 0;
  const orig = Date.prototype.toLocaleString;
  Date.prototype.toLocaleString = function (...args: Parameters<typeof orig>) {
    if (captured < MAX_CAPTURES) {
      captured++;
      const stack = new Error(`toLocaleString call #${captured}`).stack || "(no stack)";
      // Fire-and-forget; must never throw or block the real call.
      fetch(apiPath("/api/debug/frontend-stats"), {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-Ocode-Desktop": "1", ...authHeaders() },
        body: JSON.stringify({
          window_id: "toLocaleString-probe",
          terminal_count: 0,
          terminal_lines: 0,
          session_count: 0,
          message_count: 0,
          message_bytes: 0,
          dom_node_count: 0,
          debug_note: stack,
        }),
      }).catch(() => {});
    }
    return orig.apply(this, args);
  };
})();
