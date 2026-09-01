import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Gauge } from "lucide-react";
import { eventBus } from "@/lib/eventBus";
import { api } from "@/api/client";
import { browserStore } from "@/lib/browserStore";
import { estimateBrowseSurfaceBytes, estimateSessionSliceBytes } from "@/lib/memoryEstimate";
import { useBrowserTabs } from "@/stores/browserTabsStore";
import { useChatStateRef } from "@/stores/chatStore";
import { useProjectState } from "@/stores/projectStore";
import { loadProjectTerminals } from "./terminalPersistence";

interface TerminalProcessStat {
  id: string;
  pid: number;
  cpu_percent: number;
  mem_bytes: number;
  // command is the best-effort running command captured server-side from the
  // process tree. It is absent for terminals whose process has already exited.
  command?: string;
}

/**
 * A frontend-attributed row for a surface that has no OS process of its own
 * (the chat session and each browser tab live in the same renderer as the
 * whole SPA). `bytes` is an estimate of the surface's retained JS state — see
 * memoryEstimate.ts for what each estimate covers and its caveats. These rows
 * are appended after the CPU-sorted terminal rows; `subLabel` is the muted
 * second line shown under the Name.
 */
interface LocalFootprintRow {
  id: string;
  name: string;
  subLabel: string;
  command: string;
  bytes: number;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

// How often the local estimate rows (chat session, browser tabs) re-sample.
// Terminal rows ride the server's 1.5s `terminal_processes` SSE ticks; the
// local estimators are cache-backed (see memoryEstimate.ts) so 2s is cheap.
const localFootprintInterval = 2000;

type ColumnKey = "name" | "command" | "pid" | "cpu" | "mem";

// PID/CPU/Memory stay compact; Name and Command take the bulk of the space and
// are the user-resizable columns.
const DEFAULT_WIDTHS: Record<ColumnKey, number> = {
  name: 200,
  command: 360,
  pid: 70,
  cpu: 70,
  mem: 90,
};

const MIN_WIDTH = 48;
const MAX_WIDTH = 600;

/**
 * Chrome-Task-Manager-style view of every open terminal in the active project:
 * CPU% and memory summed across the whole process tree (the shell plus
 * whatever it launched — a nested `ocode`, its `/rc` server, etc.), so a
 * runaway grandchild process surfaces even though the pty's own pid stays
 * quiet. Rows come from the `terminal_processes` envelope the server emitter
 * publishes (see internal/server/emitters.go); titles come from the same
 * localStorage-persisted terminal list TerminalTabs itself reads (now
 * project-scoped so the view survives session switches). The live command comes
 * from the envelope's `command` field (best-effort process-tree command line).
 *
 * Below the terminal rows, the panel appends frontend-attributed rows for the
 * surfaces that have no OS process of their own — every open chat session for
 * this project and each open browser tab. Their Memory values are estimates of
 * the retained JS state each surface owns (see memoryEstimate.ts), computed
 * locally on a 2s ticker and labeled `est.`; PID/CPU show "—" because there is
 * no process to measure. Browser rows cover the standalone `tab:` surfaces
 * (side panels are excluded).
 */
export default function ProcessesPanel({ projectPath }: { projectPath: string }) {
  const [stats, setStats] = useState<Record<string, TerminalProcessStat>>({});
  const [titles, setTitles] = useState<Record<string, string>>(() => {
    const saved = loadProjectTerminals(projectPath);
    if (!saved) return {};
    return Object.fromEntries(saved.terminals.map((t) => [t.id, t.title]));
  });
  const { widths, startResize } = useColumnWidths(projectPath);

  const { state: projectState } = useProjectState();
  const projectTabs = projectState.tabsByProject[projectPath] ?? [];
  const activeTabId = projectState.activeTabByProject[projectPath] ?? null;

  // Standalone browser tabs of this project (tab: stateKeys — side panels are
  // excluded). Titles live in the tab strip store; per-tab state lives in
  // browserStore (a module singleton, read directly in the ticker below).
  const { tabs: rawBrowserTabs } = useBrowserTabs(projectPath);
  const chatStateRef = useChatStateRef();

  // Both tab arrays fall back to a fresh `?? []` when the project has no
  // tabs yet, so their identities are unstable across renders — never use
  // them as effect dependencies. The `*Key` strings are stable content
  // signatures. Refs keep the ticker reading the latest value without
  // re-subscribing on every render.
  const browserTabsRef = useRef(rawBrowserTabs);
  browserTabsRef.current = rawBrowserTabs;
  const browserTabsKey = rawBrowserTabs.map((t) => `${t.id}:${t.title}`).join("|");
  const chatTabsRef = useRef(projectTabs);
  chatTabsRef.current = projectTabs;
  const chatTabsKey = projectTabs.map((t) => `${t.id}:${t.title}`).join("|");
  const activeTabRef = useRef(activeTabId);
  activeTabRef.current = activeTabId;

  const [localRows, setLocalRows] = useState<LocalFootprintRow[]>([]);
  const lastRowsJson = useRef("");

  // Reset estimated rows when switching projects so stale rows from the
  // previous project don't flash before the next tick, and identical
  // snapshots still trigger a render for the new project.
  useEffect(() => {
    lastRowsJson.current = "";
    setLocalRows([]);
  }, [projectPath]);

  useEffect(() => {
    const tick = () => {
      const rows: LocalFootprintRow[] = [];
      for (const tab of chatTabsRef.current) {
        const slice = chatStateRef.current.sessions[tab.id];
        if (!slice) {
          const isActive = tab.id === activeTabRef.current;
          rows.push({
            id: `chat:${tab.id}`,
            name: tab.title || "Chat session",
            subLabel: isActive ? "not loaded yet · active" : "not loaded yet",
            command: "—",
            bytes: 0,
          });
        } else {
          const stats_ = estimateSessionSliceBytes(tab.id, slice);
          const isActive = tab.id === activeTabRef.current;
          rows.push({
            id: `chat:${tab.id}`,
            name: tab.title || "Chat session",
            subLabel: `${stats_.messageCount} message${stats_.messageCount === 1 ? "" : "s"} · est.${isActive ? " · active" : ""}`,
            command: "—",
            bytes: stats_.bytes,
          });
        }
      }
      for (const tab of browserTabsRef.current) {
        const surface = browserStore.state.byKey[`tab:${tab.id}`];
        if (!surface) {
          rows.push({
            id: `browser:${tab.id}`,
            name: tab.title,
            subLabel: "not loaded yet",
            command: "—",
            bytes: 0,
          });
          continue;
        }
        const est = estimateBrowseSurfaceBytes(surface);
        rows.push({
          id: `browser:${tab.id}`,
          name: tab.title,
          subLabel: est.bytes > 0 ? `${est.consoleCount} console · ${est.networkCount} network · est.` : "not loaded yet",
          command: surface.url || "—",
          bytes: est.bytes,
        });
      }
      // The estimates change rarely; skip the re-render when the snapshot is
      // byte-identical so a hidden Processes panel stays quiet between ticks.
      const json = JSON.stringify(rows);
      if (json !== lastRowsJson.current) {
        lastRowsJson.current = json;
        setLocalRows(rows);
      }
    };
    tick();
    const interval = setInterval(tick, localFootprintInterval);
    return () => clearInterval(interval);
    // chatStateRef is intentionally not a dependency: it is a stable ref in
    // production (useChatStateRef returns the same object forever) and
    // depending on it would re-subscribe the ticker on every render.
  }, [projectPath, chatTabsKey, browserTabsKey]);

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
      if (env.project !== projectPath) return;
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

  const total = widths.name + widths.command + widths.pid + widths.cpu + widths.mem;

  return (
    <div className="flex h-full flex-col bg-card">
      <div className="flex h-9 shrink-0 items-center gap-2 border-b border-border px-3 text-sm text-muted-foreground">
        <Gauge className="h-4 w-4" />
        Processes
      </div>
      <div className="flex-1 overflow-auto">
        {rows.length === 0 && localRows.length === 0 ? (
          <div className="p-4 text-sm text-muted-foreground">
            No terminal process data yet. Open a terminal to see it here.
          </div>
        ) : (
          <table
            className="table-fixed border-collapse text-sm"
            style={{ width: total, minWidth: "100%" }}
          >
            <colgroup>
              <col style={{ width: widths.name }} />
              <col style={{ width: widths.command }} />
              <col style={{ width: widths.pid }} />
              <col style={{ width: widths.cpu }} />
              <col style={{ width: widths.mem }} />
            </colgroup>
            <thead>
              <tr className="border-b border-border text-left text-xs uppercase text-muted-foreground">
                <Th w={widths.name} resizable onResize={(e) => startResize("name", e)}>
                  Name
                </Th>
                <Th w={widths.command} resizable onResize={(e) => startResize("command", e)}>
                  Command
                </Th>
                {/* PID / CPU / Memory are fixed-width: no resize handle. */}
                <Th w={widths.pid}>PID</Th>
                <Th w={widths.cpu}>CPU</Th>
                <Th w={widths.mem}>Memory</Th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id} className="border-b border-border/50 text-foreground">
                  <td className="px-3 py-2">
                    <div className="truncate">{titles[row.id] ?? row.id}</div>
                  </td>
                  <td className="px-3 py-2 font-mono text-xs">
                    <div className="truncate">{row.command || "—"}</div>
                  </td>
                  <td className="px-3 py-2 text-muted-foreground">{row.pid}</td>
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
              {localRows.length > 0 && (
                <tr key="__frontend-header" className="bg-muted/30">
                  <td colSpan={5} className="px-3 py-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                    Frontend surfaces (estimated) — ~ memory, same renderer
                  </td>
                </tr>
              )}
              {localRows.map((row) => (
                <tr key={row.id} className="border-b border-border/50 text-foreground">
                  <td className="px-3 py-2">
                    <div className="truncate" title={row.name}>
                      {row.name}
                    </div>
                    <div className="truncate text-xs text-muted-foreground" title={row.subLabel}>
                      {row.subLabel}
                    </div>
                  </td>
                  <td className="px-3 py-2 font-mono text-xs">
                    <div className="truncate" title={row.command}>
                      {row.command}
                    </div>
                  </td>
                  {/* No OS process: PID/CPU are not applicable to these rows. */}
                  <td className="px-3 py-2 text-muted-foreground">—</td>
                  <td className="px-3 py-2 text-muted-foreground">—</td>
                  <td className="px-3 py-2 tabular-nums text-muted-foreground">
                    {row.bytes > 0 ? `~${formatBytes(row.bytes)}` : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}

/**
 * A `th` with a drag-to-resize handle on its right edge. Resizing updates the
 * shared width state (and persists it, see useColumnWidths); the handle is an
 * inert separator for assistive tech.
 */
function Th({
  w,
  onResize,
  resizable = false,
  children,
}: {
  w: number;
  onResize?: (e: React.MouseEvent) => void;
  resizable?: boolean;
  children: React.ReactNode;
}) {
  return (
    <th className="relative border-b border-border px-3 py-2 font-medium" style={{ width: w }}>
      <span className="block truncate">{children}</span>
      {resizable && (
        <div
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize column"
          onMouseDown={onResize}
          className="absolute right-0 top-0 h-full w-1.5 cursor-col-resize select-none hover:bg-accent"
        />
      )}
    </th>
  );
}

/**
 * Per-project resizable column widths, persisted to localStorage. Widths are
 * clamped to [MIN_WIDTH, MAX_WIDTH]; malformed or partially-present stored
 * values fall back to defaults so a corrupted entry never breaks the layout.
 * Changing `projectPath` reloads the saved layout for that project.
 */
function useColumnWidths(projectPath: string) {
  const [widths, setWidths] = useState<Record<ColumnKey, number>>(() =>
    sanitizeWidths(loadRaw(widthsKey(projectPath)), projectPath),
  );
  const widthsRef = useRef(widths);
  widthsRef.current = widths;

  // Reload the saved layout whenever the active project changes.
  useEffect(() => {
    setWidths(sanitizeWidths(loadRaw(widthsKey(projectPath)), projectPath));
  }, [projectPath]);

  const persist = useCallback(
    (w: Record<ColumnKey, number>) => {
      try {
        localStorage.setItem(widthsKey(projectPath), JSON.stringify(w));
      } catch {
        // Ignore quota / private-mode failures — widths are a cosmetic pref.
      }
    },
    [projectPath],
  );

  const startResize = useCallback(
    (col: ColumnKey, e: React.MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      const startX = e.clientX;
      const startW = widthsRef.current[col];
      const prevUserSelect = document.body.style.userSelect;
      document.body.style.userSelect = "none";
      const onMove = (ev: MouseEvent) => {
        const next = {
          ...widthsRef.current,
          [col]: Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, startW + (ev.clientX - startX))),
        };
        widthsRef.current = next;
        setWidths(next);
      };
      const onUp = () => {
        document.removeEventListener("mousemove", onMove);
        document.removeEventListener("mouseup", onUp);
        document.body.style.userSelect = prevUserSelect;
        persist(widthsRef.current);
      };
      document.addEventListener("mousemove", onMove);
      document.addEventListener("mouseup", onUp);
    },
    [persist],
  );

  return { widths, startResize };
}

function widthsKey(projectPath: string): string {
  return `ocode.ui.processes.colwidths.v1.${projectPath}`;
}

function loadRaw(key: string): unknown {
  try {
    const raw = localStorage.getItem(key);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}

function sanitizeWidths(raw: unknown, _projectPath: string): Record<ColumnKey, number> {
  const out: Record<ColumnKey, number> = { ...DEFAULT_WIDTHS };
  if (raw && typeof raw === "object") {
    const obj = raw as Record<string, unknown>;
    for (const k of Object.keys(DEFAULT_WIDTHS) as ColumnKey[]) {
      const v = obj[k];
      if (typeof v === "number" && Number.isFinite(v)) {
        out[k] = Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, Math.round(v)));
      }
    }
  }
  return out;
}
