import { useEffect, useMemo, useState } from "react";
import { Gauge } from "lucide-react";
import { eventBus } from "@/lib/eventBus";
import { api } from "@/api/client";
import { loadProjectTerminals } from "./terminalPersistence";

interface TerminalProcessStat {
  id: string;
  pid: number;
  cpu_percent: number;
  mem_bytes: number;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/**
 * Chrome-Task-Manager-style view of every open terminal in the active project:
 * CPU% and memory summed across the whole process tree (the shell plus
 * whatever it launched — a nested `ocode`, its `/rc` server, etc.), so a
 * runaway grandchild process surfaces even though the pty's own pid stays
 * quiet. Rows come from the `terminal_processes` envelope the server emitter
 * publishes (see internal/server/emitters.go); titles come from the same
 * localStorage-persisted terminal list TerminalTabs itself reads (now
 * project-scoped so the view survives session switches).
 */
export default function ProcessesPanel({ projectPath }: { projectPath: string }) {
  const [stats, setStats] = useState<Record<string, TerminalProcessStat>>({});
  const [titles, setTitles] = useState<Record<string, string>>(() => {
    const saved = loadProjectTerminals(projectPath);
    if (!saved) return {};
    return Object.fromEntries(saved.terminals.map((t) => [t.id, t.title]));
  });

  function refreshTitles() {
    const saved = loadProjectTerminals(projectPath);
    if (saved) {
      setTitles(Object.fromEntries(saved.terminals.map((t) => [t.id, t.title])));
    }
  }

  function mergeRows(rows: TerminalProcessStat[], mode: "replace" | "merge" = "merge") {
    // SSE envelopes are per-project, so a single envelope never represents
    // the global live set. Replacing the global map with one project's
    // snapshot clobbers other projects' terminals (flicker in multi-project
    // servers). Merge per-project envelopes; only the global REST snapshot
    // replaces the whole map. Stale entries for terminals no longer in the
    // snapshot are cleaned up to prevent unbounded memory growth.
    if (mode === "replace") {
      const next: Record<string, TerminalProcessStat> = {};
      for (const row of rows) next[row.id] = row;
      setStats(next);
    } else {
      setStats((prev) => {
        const next = { ...prev };
        for (const row of rows) next[row.id] = row;
        // Remove entries for terminals no longer present in the latest
        // snapshot. Without this, closed terminals accumulate forever.
        const liveIds = new Set(rows.map((r) => r.id));
        for (const id of Object.keys(next)) {
          if (!liveIds.has(id)) delete next[id];
        }
        return next;
      });
    }
    refreshTitles();
  }

  useEffect(() => {
    // Sync titles immediately so rows are not filtered out while waiting for
    // the first SSE envelope (localStorage already has the terminal list).
    refreshTitles();

    // Fetch a live snapshot so the tab shows memory instantly even if it
    // mounted between SSE ticks or before the emitter's first tick.
    let cancelled = false;
    api
      .getTerminalProcesses()
      .then((rows) => {
        if (cancelled || !Array.isArray(rows) || rows.length === 0) return;
        mergeRows(rows as TerminalProcessStat[], "replace");
      })
      .catch(() => {
        // Non-fatal — SSE will still populate.
      });

    const off = eventBus.on("terminal_processes", (env) => {
      const rows = env.data as TerminalProcessStat[];
      if (!Array.isArray(rows)) return;
      mergeRows(rows);
    });
    return () => {
      cancelled = true;
      off();
    };
  }, [projectPath]);

  const rows = useMemo(() => {
    return Object.values(stats)
      .filter((s) => titles[s.id] !== undefined)
      .sort((a, b) => b.cpu_percent - a.cpu_percent);
  }, [stats, titles]);

  return (
    <div className="flex h-full flex-col bg-zinc-900">
      <div className="flex h-9 shrink-0 items-center gap-2 border-b border-zinc-700 px-3 text-sm text-zinc-400">
        <Gauge className="h-4 w-4" />
        Processes
      </div>
      <div className="flex-1 overflow-y-auto">
        {rows.length === 0 ? (
          <div className="p-4 text-sm text-zinc-500">
            No terminal process data yet. Open a terminal to see it here.
          </div>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-zinc-800 text-left text-xs uppercase text-zinc-500">
                <th className="px-3 py-2 font-medium">Terminal</th>
                <th className="px-3 py-2 font-medium">PID</th>
                <th className="px-3 py-2 font-medium">CPU</th>
                <th className="px-3 py-2 font-medium">Memory</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id} className="border-b border-zinc-800/50 text-zinc-300">
                  <td className="px-3 py-2">{titles[row.id] ?? row.id}</td>
                  <td className="px-3 py-2 text-zinc-500">{row.pid}</td>
                  <td
                    className={`px-3 py-2 tabular-nums ${
                      row.cpu_percent > 50 ? "text-red-400" : row.cpu_percent > 15 ? "text-yellow-400" : ""
                    }`}
                  >
                    {row.cpu_percent.toFixed(1)}%
                  </td>
                  <td className="px-3 py-2 tabular-nums">{formatBytes(row.mem_bytes)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
