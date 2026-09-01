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

/**
 * TEMPORARY DIAGNOSTIC — remove once the root cause of the reported
 * "model dialog / any dialog popup auto-closes while chat is streaming"
 * bug is identified (see CHANGES.md entry + web/src/dialogStreaming.test.tsx:
 * the full-app streaming regression test proves the dispatch path cannot
 * close Radix modals; the remaining triggers are Escape, outside
 * pointerdown, or a parent remount — all event/environment-level). This
 * monitor logs whichever of those actually fires while a dialog is open, so
 * a single live repro names the cause from the console.
 *
 * Cost: three document-level capture listeners plus one MutationObserver.
 * Every handler does a single cheap `querySelector` (no array allocation)
 * and bails immediately when no dialog is open — bounded, non-polling.
 */
export function installDialogCloseMonitor(): () => void {
  let openCount = 0; // our view of open [role="dialog"][data-state="open"]
  let observer: MutationObserver | null = null;

  const dialogIsOpen = () =>
    document.querySelector('[role="dialog"][data-state="open"]') !== null;

  const sync = () => {
    const n = document.querySelectorAll('[role="dialog"][data-state="open"]').length;
    if (n === openCount) return;
    if (n < openCount) {
      // No Escape / outside-pointer/click was logged for this drop (they log
      // above as they happen) — so it was a parent-state close or remount.
      console.warn("[dialog-close] dialog count fell (no Escape/outside-click logged)", {
        before: openCount,
        after: n,
      });
    }
    openCount = n;
    if (openCount === 0 && observer) {
      observer.disconnect();
      observer = null;
    }
    if (openCount > 0 && !observer) {
      // While any dialog is open, watch both DOM structure (unmount/remount)
      // and data-state attribute flips (Radix open→closed without an event).
      observer = new MutationObserver(sync);
      observer.observe(document.body, {
        childList: true,
        subtree: true,
        attributes: true,
        attributeFilter: ["data-state"],
      });
    }
  };

  const onKey = (e: KeyboardEvent) => {
    if (e.key !== "Escape" || openCount === 0) return;
    console.warn("[dialog-close] Escape keydown while dialog open", { target: e.target });
  };
  const onPointer = (e: PointerEvent) => {
    if (openCount === 0) return;
    const target = e.target as Element | null;
    if (target && !target.closest('[role="dialog"]')) {
      console.warn("[dialog-close] pointerdown OUTSIDE dialog while open", { target });
    }
  };
  const onFocus = () => {
    if (openCount === 0 && dialogIsOpen()) sync();
  };

  document.addEventListener("keydown", onKey, true);
  document.addEventListener("pointerdown", onPointer, true);
  document.addEventListener("focusin", onFocus, true);
  sync();

  return () => {
    observer?.disconnect();
    observer = null;
    document.removeEventListener("keydown", onKey, true);
    document.removeEventListener("pointerdown", onPointer, true);
    document.removeEventListener("focusin", onFocus, true);
  };
}

// Auto-installed at boot: console-only, no behavior change. Remove together
// with the function above once the reported dialog-close bug is root-caused.
installDialogCloseMonitor();

// Cached Date.prototype.toLocaleString — avoids reconstructing
// Intl.DateTimeFormat on every hot-path status render. Retains native
// semantics for invalid dates and for calls with no date/time fields.
// Ships in every build (not gated behind import.meta.env.DEV): unlike the
// diagnostic probe docs/gotchas/debug-instrumentation-ships-unconditionally.md
// warns about (which made network calls and altered behavior), this patch is
// semantics-preserving and side-effect-free, so restricting it to dev builds
// only meant the production desktop app — where the memory growth this fixes
// was actually observed — never got the fix (confirmed live 2026-08-30: a
// fresh production build still showed dateProtoFuncToLocaleString/ICU
// construction dominating a `sample` capture during a footprint spike).
(() => {
  const orig = Date.prototype.toLocaleString;
  const cache = new Map<string, Intl.DateTimeFormat>();
  const DTF_CACHE_MAX = 50;
  const hasDateField = (opts?: Intl.DateTimeFormatOptions): boolean => {
    if (!opts) return false;
    return !!(
      opts.weekday !== undefined ||
      opts.era !== undefined ||
      opts.year !== undefined ||
      opts.month !== undefined ||
      opts.day !== undefined ||
      opts.hour !== undefined ||
      opts.minute !== undefined ||
      opts.second !== undefined ||
      opts.timeZoneName !== undefined ||
      opts.dayPeriod !== undefined ||
      opts.dateStyle !== undefined ||
      opts.timeStyle !== undefined
    );
  };
  const canonicalOptionsKey = (opts: Intl.DateTimeFormatOptions): string => {
    const keys = Object.keys(opts)
      .filter((k) => (opts as Record<string, unknown>)[k] !== undefined)
      .sort();
    const sorted: Record<string, unknown> = {};
    for (const k of keys) sorted[k] = (opts as Record<string, unknown>)[k];
    return JSON.stringify(sorted);
  };
  Date.prototype.toLocaleString = function (
    this: Date,
    locales?: string | string[],
    options?: Intl.DateTimeFormatOptions,
  ): string {
    // Preserve native behavior for invalid dates and for calls that don't
    // actually request a date/time formatting (e.g. bare toLocaleString() or
    // {hour12:false} alone) — those never benefit from the cache.
    if (isNaN(this.valueOf()) || !hasDateField(options)) {
      return orig.apply(this, arguments as unknown as Parameters<typeof orig>);
    }
    const key = `${JSON.stringify(locales ?? null)}::${canonicalOptionsKey(options!)}`;
    let fmt = cache.get(key);
    if (!fmt) {
      fmt = new Intl.DateTimeFormat(locales, options);
      if (cache.size >= DTF_CACHE_MAX) {
        const oldestKey = cache.keys().next().value as string | undefined;
        if (oldestKey !== undefined) cache.delete(oldestKey);
      }
      cache.set(key, fmt);
    }
    return fmt.format(this);
  };
  // Test hook — lets the suite reset the FIFO cache between cases so
  // construction counts stay deterministic without reinstalling the patch.
  (Date.prototype.toLocaleString as unknown as Record<string, unknown>).__resetCacheForTests =
    () => cache.clear();
})();
