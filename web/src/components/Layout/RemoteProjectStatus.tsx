import { useEffect, useMemo, useState } from "react";
import { MessageSquare, Play, RotateCw, SquareTerminal, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { authedFetch, remoteApiBase } from "@/api/client";
import type { AgentRun, Project } from "@/api/types";
import type { RemoteHostStatusState } from "@/hooks/useRemoteHostStatus";
import { useRemoteTerminals } from "@/hooks/useRemoteTerminals";
import { eventBus } from "@/lib/eventBus";
import { projectSessionKey, useProjectState } from "@/stores/projectStore";
import { getProjectTerminals, useTerminalState } from "@/stores/terminalStore";

function anyRunRunning(runs: AgentRun[]): boolean {
  return runs.some((run) => run.status === "running" || anyRunRunning(run.children ?? []));
}

/** Session ids with at least one live run, patched from the shared `runs` bus. */
function useRunningSessions(): Set<string> {
  const [running, setRunning] = useState<Set<string>>(() => new Set());
  useEffect(() => {
    const off = eventBus.on("runs", (env) => {
      const sessionId = env.session_id;
      if (!sessionId) return;
      const active = anyRunRunning((env.data as AgentRun[]) ?? []);
      setRunning((prev) => {
        if (active === prev.has(sessionId)) return prev;
        const next = new Set(prev);
        if (active) next.add(sessionId);
        else next.delete(sessionId);
        return next;
      });
    });
    return off;
  }, []);
  return running;
}

/**
 * RemoteProjectStatus — the status line + expandable inventory for one remote
 * (SSH/WSL) project in the sidebar.
 *
 * Collapsed: `v1.2.3 · 2 chats (1 running) · 3 terminals`, or `not connected`
 * with a Connect action. An outdated server gets an amber dot and a Restart
 * action. Clicking the line expands a chat list (open as tabs; running badges)
 * and the host's live terminal list (attach by id; kill).
 *
 * The status hook is owned by the parent row (so its context menu can trigger
 * the same Restart); this component owns expansion and the terminal inventory.
 */
export function RemoteProjectStatus({ project, statusState }: { project: Project; statusState: RemoteHostStatusState }) {
  const host = project.host ?? "";
  const [expanded, setExpanded] = useState(false);
  const { status, loading, busy, error, connect, restart } = statusState;
  const { state: projectState, prefetchProjectSessions, openSessionTab } = useProjectState();
  const { state: terminalState, attachTerminal } = useTerminalState();
  const running = useRunningSessions();

  const connected = status?.connected ?? false;
  const outdated = status?.outdated ?? false;
  // Fetch the inventory once the host is connected so the collapsed line can
  // show the count; the expanded list reuses the same data.
  const { terminals, refresh } = useRemoteTerminals(host, project.path, connected);

  const sessions = projectState.sessionsByProject?.[projectSessionKey(project.path, host)]?.sessions ?? [];
  const openSessionIds = new Set((projectState.tabsByProject?.[project.path] ?? []).map((t) => t.id));
  const openTerminalIds = useMemo(
    () => new Set(getProjectTerminals(terminalState, project.path, host).terminals.map((t) => t.id)),
    [terminalState, project.path, host],
  );

  useEffect(() => {
    if (expanded) prefetchProjectSessions(project);
  }, [expanded, prefetchProjectSessions, project]);

  const runningCount = sessions.filter((s) => running.has(s.id)).length;

  let line: string;
  if (busy === "connecting") line = "connecting…";
  else if (busy === "restarting") line = "restarting…";
  else if (!connected) line = loading && !status ? "checking…" : "not connected";
  else line = `${status?.version ?? ""} · ${sessions.length} chats${runningCount > 0 ? ` (${runningCount} running)` : ""} · ${terminals.length} terminals`;

  const busyNow = busy !== "idle";

  const killTerminal = (id: string) => {
    const headers = new Headers();
    headers.set("X-Ocode-Project", project.path);
    void authedFetch(`${remoteApiBase(host)}/api/terminal/${encodeURIComponent(id)}`, { method: "DELETE", headers })
      .then(() => refresh())
      .catch((err) => console.error(`failed to kill remote terminal ${id}:`, err));
  };

  const stop = (e: { stopPropagation: () => void }) => e.stopPropagation();

  return (
    <div className="mt-0.5">
      <div
        className="flex items-center gap-1 text-xs text-muted-foreground cursor-pointer select-none"
        onClick={() => setExpanded((v) => !v)}
        role="button"
        tabIndex={0}
        aria-expanded={expanded}
        data-testid="remote-project-status"
      >
        {outdated && <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-amber-500" aria-label="outdated" />}
        <span className="truncate">{line}</span>
        {!connected && (
          <Button
            variant="ghost"
            size="sm"
            className="h-4 px-1 text-[10px]"
            disabled={busyNow}
            onClick={(e) => {
              stop(e);
              connect();
            }}
          >
            Connect
          </Button>
        )}
        {connected && outdated && (
          <Button
            variant="ghost"
            size="sm"
            className="h-4 px-1 text-[10px] gap-0.5"
            disabled={busyNow}
            onClick={(e) => {
              stop(e);
              restart();
            }}
          >
            <RotateCw className="w-2.5 h-2.5" />
            Restart
          </Button>
        )}
        {error && <span className="truncate text-destructive" title={error}>· {error}</span>}
      </div>

      {expanded && connected && (
        <div className="mt-1 space-y-1 pl-3">
          <div>
            <div className="text-[10px] uppercase tracking-wide text-muted-foreground">Chats</div>
            {sessions.length === 0 ? (
              <div className="text-xs text-muted-foreground">No chats</div>
            ) : (
              sessions.map((s) => (
                <button
                  key={s.id}
                  type="button"
                  className="flex w-full items-center gap-1 truncate rounded px-1 py-0.5 text-left text-xs hover:bg-accent"
                  onClick={stop}
                  onDoubleClick={stop}
                  onMouseDown={stop}
                  onMouseUp={stop}
                  onPointerDown={stop}
                  onPointerUp={(e) => {
                    e.stopPropagation();
                    openSessionTab(s.id, s.title || s.id);
                  }}
                >
                  <MessageSquare className="w-3 h-3 shrink-0" />
                  <span className="truncate">{s.title || s.id}</span>
                  {running.has(s.id) && <Play className="w-2.5 h-2.5 shrink-0 text-emerald-500" aria-label="running" />}
                  {openSessionIds.has(s.id) && <span className="ml-auto shrink-0 text-[10px] text-muted-foreground">open</span>}
                </button>
              ))
            )}
          </div>
          <div>
            <div className="text-[10px] uppercase tracking-wide text-muted-foreground">Terminals</div>
            {terminals.length === 0 ? (
              <div className="text-xs text-muted-foreground">No terminals</div>
            ) : (
              terminals.map((t) => (
                <div
                  key={t.id}
                  className="flex w-full items-center gap-1 rounded px-1 py-0.5 text-xs hover:bg-accent"
                  onMouseDown={stop}
                  onPointerDown={stop}
                >
                  <SquareTerminal className="w-3 h-3 shrink-0" />
                  <button
                    type="button"
                    className="min-w-0 flex-1 truncate text-left"
                    onClick={(e) => {
                      stop(e);
                      attachTerminal(project.path, host, t.id, t.title);
                    }}
                  >
                    {t.title || `Terminal ${t.id.slice(0, 6)}`}
                  </button>
                  {openTerminalIds.has(t.id) && <span className="shrink-0 text-[10px] text-muted-foreground">open</span>}
                  <button
                    type="button"
                    aria-label={`kill terminal ${t.id}`}
                    className="shrink-0 text-muted-foreground hover:text-destructive"
                    onClick={(e) => {
                      stop(e);
                      killTerminal(t.id);
                    }}
                  >
                    <X className="w-3 h-3" />
                  </button>
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
}
