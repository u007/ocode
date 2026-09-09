# Design: `window.ocodeDebug` Debug Helper

**Date**: 2026-08-18
**Status**: Approved
**Author**: ocode agent

---

## Advisor Review (2026-08-18)

Key findings incorporated:
1. `agentRuns()` N+1 is not bounded parallelism — use a concurrency limiter (max 5)
2. `agentRuns()` without session returns empty from `HandleListRuns` — document as "best-effort active runs"
3. `LogEntry` is wrong — `GET /api/logs` returns `{kind, message, session_id?}`, no `time` field
4. `sessionStatus()` returns a global snapshot, not truly per-session — rename to clarify scope
5. Reuse existing `AgentRun` type from `web/src/api/types.ts` instead of local `AgentRunDTO`
6. Use typed global declaration instead of `(window as any)`

## Problem

The ocode desktop/web app's built-in browser DevTools (View → Open Developer Tools) only shows generic browser diagnostics. There's no way to inspect ocode-specific state — active sessions, agent runs, background processes, or projects — without writing ad-hoc fetch calls each time.

## Goal

Add a `window.ocodeDebug` helper object that's auto-injected into every page load, always available in the DevTools console. Users can type `ocodeDebug.sessions()` or `ocodeDebug.agentRuns()` to inspect the agent system's internal state.

## Approach

**SPA-injected TS module** — a small `web/src/debug.ts` file imported from `web/src/main.tsx`. Zero backend changes. All methods call existing API endpoints.

## Methods

| Method | Returns | Endpoint | Notes |
|--------|---------|----------|-------|
| `sessions()` | `Promise<SessionInfo[]>` | `GET /api/sessions` | All sessions across all projects |
| `sessionStatus(id)` | `Promise<TUIStatus>` | `GET /api/sessions/{id}/status` | Hybrid: global TUI fields + session identity, project root, context estimates |
| `agentRuns(sessionId?)` | `Promise<AgentRun[]>` | `GET /api/agents/runs?session=X` | Best-effort active runs. If no sessionId, fetches all sessions first (max 5 concurrent). Only returns runs for sessions with in-memory agents. |
| `projects()` | `Promise<Project[]>` | `GET /api/projects` | Saved project paths |
| `runtime()` | `Promise<RuntimeStats>` | `GET /api/debug/runtime` | Heap, goroutines, GC, uptime |
| `status()` | `Promise<TUIStatus>` | `GET /api/tui-status` | Global TUI status snapshot (model, advisor, OCR, cwd, context) |
| `lspStatus()` | `Promise<LSPStatus[]>` | `GET /api/lsp/statuses` | Running language servers |
| `logs()` | `Promise<LogEntry[]>` | `GET /api/logs` | Debug log entries (kind, message, session_id) |
| `help()` | `void` | — | Prints usage to console |

## Key Design Decisions

1. **Auth**: Reuse `authHeaders()` from `web/src/api/client.ts` (reads `?token=` from URL). No localStorage token persistence.
2. **Types**: Reuse existing DTOs from `web/src/api/types.ts` (`SessionInfo`, `TUIStatus`, `Project`, `LSPStatus`). Only `RuntimeStats` and `LogEntry` need local interfaces.
3. **No backend changes**: All methods call existing endpoints. `processes()` is deliberately omitted — interactive terminal WS sessions aren't tracked in any registry, and background bash processes lack a REST endpoint.
4. **Lazy execution**: Nothing runs on page load. Methods are called on-demand from DevTools console.
5. **Wails-compatible**: The webview loads the loopback HTTP server, so `fetch` works same-origin. Vite dev uses `/api` proxy via `apiPath()`.
6. **No token exposure**: The bearer token is read from the URL query param via existing helpers, never stored on `window.ocodeDebug`.

## File Changes

### New: `web/src/debug.ts`
```typescript
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

// AgentRun and Project are imported from api/types.ts — no local duplication needed.

async function debugFetch<T>(path: string): Promise<T> {
  const resp = await fetch(apiPath(path), { headers: authHeaders() });
  if (!resp.ok) throw new Error(`ocodeDebug: ${path} returned ${resp.status}`);
  return resp.json();
}

// Bounded concurrency helper (max N simultaneous fetches)
async function boundedAll<T>(items: T[], fn: (item: T) => Promise<any>, max = 5): Promise<PromiseSettledResult<any>[]> {
  const results: PromiseSettledResult<any>[] = [];
  for (let i = 0; i < items.length; i += max) {
    const batch = items.slice(i, i + max);
    results.push(...await Promise.allSettled(batch.map(fn)));
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
    const status = await debugFetch<TUIStatus>(`/api/sessions/${encodeURIComponent(id)}/status`);
    console.log(status);
    return status;
  },

  async agentRuns(sessionId?: string): Promise<AgentRun[]> {
    if (sessionId) {
      const runs = await debugFetch<AgentRun[]>(`/api/agents/runs?session=${encodeURIComponent(sessionId)}`);
      console.table(runs.map(r => ({ id: r.id, name: r.name, status: r.status, model: r.model })));
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
      s => debugFetch<AgentRun[]>(`/api/agents/runs?session=${encodeURIComponent(s.id)}`)
    );
    const seen = new Set<string>();
    const all = results
      .filter((r): r is PromiseFulfilledResult<AgentRun[]> => r.status === 'fulfilled')
      .flatMap(r => r.value)
      .filter(r => r.id && !seen.has(r.id) && seen.add(r.id)); // deduplicate by ID
    const failed = results.filter(r => r.status === 'rejected');
    if (failed.length > 0) {
      console.warn(`ocodeDebug: ${failed.length}/${sessions.length} session fetches failed`);
    }
    console.table(all.map(r => ({ id: r.id, name: r.name, status: r.status, model: r.model })));
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
  ocodeDebug.sessionStatus(id)  Per-session status (model, turn, context)
  ocodeDebug.agentRuns(sid?)    Agent run tree (all sessions if no sid)
  ocodeDebug.projects()         Saved project paths
  ocodeDebug.runtime()          Go runtime stats (heap, goroutines)
  ocodeDebug.status()           Global TUI status snapshot
  ocodeDebug.lspStatus()        Running language servers
  ocodeDebug.logs()             Debug log entries
  ocodeDebug.help()             This message
    `);
  },
};

// Expose globally
window.ocodeDebug = ocodeDebug; // typed via debug.d.ts
```

### Edit: `web/src/main.tsx`
Add one import line at the top (module-level side effect):
```typescript
import './debug';
```

### New: `web/src/debug.d.ts`
```typescript
export {};

import type { SessionInfo, TUIStatus, LSPStatus, AgentRun, Project } from './api/types';

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

interface OcodeDebug {
  sessions(): Promise<SessionInfo[]>;
  sessionStatus(id: string): Promise<TUIStatus>;
  agentRuns(sessionId?: string): Promise<AgentRun[]>;
  projects(): Promise<Project[]>;
  runtime(): Promise<RuntimeStats>;
  status(): Promise<TUIStatus>;
  lspStatus(): Promise<LSPStatus[]>;
  logs(limit?: number): Promise<LogEntry[]>;
  help(): void;
}

declare global {
  interface Window {
    ocodeDebug: OcodeDebug;
  }
}
```

## Testing

1. Start dev server: `cd web && npm run dev`
2. Open browser, open DevTools console
3. Type `ocodeDebug.help()` — should print usage
4. Type `ocodeDebug.sessions()` — should list sessions with `console.table`
5. Type `ocodeDebug.runtime()` — should show heap/goroutines
6. Verify no errors in console on page load (the import is a side effect, shouldn't break anything)
7. Run `npm run build` in `web/` — should compile without type errors
8. Verify `sessionStatus('nonexistent')` returns meaningful error (not empty TUIStatus)
9. Verify `agentRuns()` deduplicates when RC bridge is active
10. Verify `logs(0)` returns all logs reversed

## Out of Scope

- `processes()` method — interactive terminal WS sessions and background bash processes aren't exposed via REST
- Persistent debug panel UI (stays console-only for now)
- Backend endpoint additions
