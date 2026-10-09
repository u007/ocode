import { useEffect, useMemo, useState } from "react";
import { ChevronDown, ChevronRight, MessageSquare, Play, RotateCw, SquareTerminal, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { AgentRun, Project } from "@/api/types";
import type { RemoteHostStatusState } from "@/hooks/useRemoteHostStatus";
import { useRemoteTerminals } from "@/hooks/useRemoteTerminals";
import { eventBus } from "@/lib/eventBus";
import { tabFocusActions } from "@/lib/tabFocus";
import { projectSessionKey, useProjectState } from "@/stores/projectStore";
import { getProjectTerminals, terminalDisplayTitle, useTerminalState } from "@/stores/terminalStore";
import { CopyValueButton } from "../common/CopyValueButton";

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
 * and the host's live terminal list (attach by id; kill). The kill X renders
 * only for the selected project — the inventory is browsable from any row, but
 * killing a shell on another machine requires the project to be active first.
 *
 * The chat inventory is a LIVE view, not a session browser: it lists only chats
 * the user is actually working with — open as a tab in this window, or running
 * an agent turn on the host. The host's full history belongs in the Sessions
 * dialog; once remote session listing actually loaded, rendering every session
 * here flooded the row (dozens of past chats) and buried the live ones. The
 * collapsed `N chats` count tracks this same filtered set, so the count always
 * matches the rows the expansion shows.
 *
 * The status hook is owned by the parent row (so its context menu can trigger
 * the same Restart); this component owns expansion and the terminal inventory.
 */
export function RemoteProjectStatus({
  project,
  statusState,
  onRevealTab,
}: {
  project: Project;
  statusState: RemoteHostStatusState;
  /** Called after a chat/terminal is revealed. The sidebar uses it to dismiss
   *  the off-canvas drawer on mobile: the inventory's own controls stop
   *  propagation, so the row's `onSelect` (which dismisses the drawer) never
   *  runs, and the revealed tab would stay hidden behind the drawer. */
  onRevealTab?: () => void;
}) {
  const host = project.host ?? "";
  const [expanded, setExpanded] = useState(false);
  const { status, loading, busy, error, connect, restart } = statusState;
  const { state: projectState, prefetchProjectSessions, openSessionTab, selectProject } = useProjectState();
  const { state: terminalState, attachTerminal, killTerminal: killTerminalTab } = useTerminalState();
  const running = useRunningSessions();

  const connected = status?.connected ?? false;
  const outdated = status?.outdated ?? false;
  // Fetch the inventory once the host is connected so the collapsed line can
  // show the count; the expanded list reuses the same data.
  const { terminals, refresh } = useRemoteTerminals(host, project.path, connected);

  const sessions = projectState.sessionsByProject?.[projectSessionKey(project.path, host)]?.sessions ?? [];
  const openSessionIds = new Set((projectState.tabsByProject?.[project.path] ?? []).map((t) => t.id));
  // Only the chats the user is actually working with: open as a tab here, or
  // running a turn on the host. `running` is fed by the host's `runs` events
  // (relayed over the one event stream and routed by session_id), so a remote
  // session running with no local tab is still listed. Everything else — the
  // host's full history — is reachable through the Sessions dialog.
  const visibleSessions = sessions.filter((s) => openSessionIds.has(s.id) || running.has(s.id));
  const localTerminals = useMemo(
    () => getProjectTerminals(terminalState, project.path, host).terminals,
    [terminalState, project.path, host],
  );
  const openTerminalIds = useMemo(() => new Set(localTerminals.map((t) => t.id)), [localTerminals]);
  // The host's inventory title is the shell's last OSC 0/2 title. A shell that
  // was already idle when the remote server started never emitted one in this
  // process, so fall back to this window's persisted title for the same id
  // instead of dropping to the "Terminal <id>" placeholder.
  const localTitleById = useMemo(() => {
    const map = new Map<string, string>();
    for (const t of localTerminals) {
      const title = terminalDisplayTitle(t);
      if (title) map.set(t.id, title);
    }
    return map;
  }, [localTerminals]);

  // Warm the session list when the inventory is expanded — but only once the
  // host is connected. For an unconnected host the fetch goes through
  // /api/remote/{host}/... and would cold-connect it (SSH provision + server
  // start + tunnel), which must stay an explicit Connect action.
  useEffect(() => {
    if (expanded && connected) prefetchProjectSessions(project);
  }, [expanded, connected, prefetchProjectSessions, project]);

  // Re-read the host's terminal list when the row is expanded. The list is
  // fetched on mount and on terminal_tabs_changed, but a host that was already
  // connected can gain terminals while this row sits collapsed (a shell started
  // elsewhere, or a session restored on the host), and expanding used to reveal
  // that stale count rather than the live one.
  useEffect(() => {
    if (expanded && connected) refresh();
  }, [expanded, connected, refresh]);

  const runningCount = visibleSessions.filter((s) => running.has(s.id)).length;

  let line: string;
  if (busy === "connecting") line = "connecting…";
  else if (busy === "restarting") line = "restarting…";
  else if (!connected) line = loading && !status ? "checking…" : "not connected";
  else line = `${status?.version ?? ""} · ${visibleSessions.length} chats${runningCount > 0 ? ` (${runningCount} running)` : ""} · ${terminals.length} terminals`;

  const busyNow = busy !== "idle";

  const killTerminal = (id: string) => {
    // Route through the store: it removes this window's tab (live or peeked)
    // so the panel cannot reconnect and respawn the shell right after the
    // DELETE, then sends the proxied DELETE with the project header. Await it
    // so the inventory refresh reflects the host's post-kill state.
    void killTerminalTab(project.path, id, host).then(() => refresh());
  };

  const stop = (e: { stopPropagation: () => void }) => e.stopPropagation();

  // Is this the project the user is currently working in? One predicate, two
  // consumers: `revealTab` (which must select a non-active project before it
  // acts on it) and the terminal kill control below.
  const active = projectState.activeProject;
  const isActive = active?.path === project.path && (active?.host ?? "") === (project.host ?? "");

  // Opening a chat/terminal from this inventory must reveal it: the user needs
  // to see the tab they just picked, not an unchanged main pane. This row lives
  // outside App's tree, so it cannot set activeView/focusedKind itself — it
  // queues the request in tabFocusStore, which the app shell applies once this
  // project is the active one (see useTabFocusRequest in App.tsx). Selecting the
  // project first is what makes this correct for a NON-active remote project:
  // otherwise the session tab would be bound to whichever project is active and
  // `resolveSessionHost` would route the remote session through the local server.
  const revealTab = (focus: { kind: "chat" | "terminal"; terminalId?: string }) => {
    if (!isActive) void selectProject(project);
    tabFocusActions.request({ ...focus, projectPath: project.path, host: project.host });
    onRevealTab?.();
  };

  return (
    <div className="mt-0.5">
      <div
        className="flex items-center gap-1 text-xs text-muted-foreground cursor-pointer select-none"
        // Stop propagation: this line is the inventory's expand/collapse
        // control (role=button + aria-expanded), not a project selector — same
        // as the Connect/Restart buttons inside it. Without this the click
        // bubbled to the sidebar row's `onSelect`, which on mobile is what
        // dismisses the drawer, so tapping the line to expand it closed the
        // drawer before the Chats/Terminals list could be used.
        onClick={(e) => {
          e.stopPropagation();
          setExpanded((v) => !v);
        }}
        role="button"
        tabIndex={0}
        aria-expanded={expanded}
        data-testid="remote-project-status"
      >
        {connected &&
          (expanded ? (
            <ChevronDown className="w-3 h-3 shrink-0" aria-hidden />
          ) : (
            <ChevronRight className="w-3 h-3 shrink-0" aria-hidden />
          ))}
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
            {visibleSessions.length === 0 ? (
              <div className="text-xs text-muted-foreground">No open chats</div>
            ) : (
              visibleSessions.map((s) => {
                const open = () => {
                  revealTab({ kind: "chat" });
                  openSessionTab(s.id, s.title || s.id, project.path, project.host);
                };
                // A <div role="button">, not a <button>: the row hosts the
                // session-ID copy control, and a real <button> inside a real
                // <button> is invalid HTML. The Terminals row below already
                // uses this outer-div shape for the same reason.
                return (
                <div
                  key={s.id}
                  role="button"
                  tabIndex={0}
                  className="group flex w-full cursor-pointer items-center gap-1 truncate rounded px-1 py-0.5 text-left text-xs hover:bg-accent"
                  onClick={stop}
                  onDoubleClick={stop}
                  onMouseDown={stop}
                  onMouseUp={stop}
                  onPointerDown={stop}
                  onPointerUp={(e) => {
                    e.stopPropagation();
                    open();
                  }}
                  onKeyDown={(e) => {
                    // The row opens on pointer-UP, which a native <button> would
                    // have covered with a synthetic click — so keyboard
                    // activation has to be wired explicitly here.
                    if (e.key !== "Enter" && e.key !== " ") return;
                    e.preventDefault();
                    e.stopPropagation();
                    open();
                  }}
                >
                  <MessageSquare className="w-3 h-3 shrink-0" />
                  <span className="truncate">{s.title || s.id}</span>
                  {running.has(s.id) && <Play className="w-2.5 h-2.5 shrink-0 text-emerald-500" aria-label="running" />}
                  <CopyValueButton
                    value={s.id}
                    label="Copy session ID"
                    className="ml-auto"
                    testId={`sidebar-copy-session-id-${s.id}`}
                  />
                  {openSessionIds.has(s.id) && <span className="shrink-0 text-[10px] text-muted-foreground">open</span>}
                </div>
                );
              })
            )}
          </div>
          <div>
            <div className="text-[10px] uppercase tracking-wide text-muted-foreground">Terminals</div>
            {terminals.length === 0 ? (
              <div className="text-xs text-muted-foreground">No terminals</div>
            ) : (
              terminals.map((t) => {
                const title = t.title || localTitleById.get(t.id) || "";
                return (
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
                      attachTerminal(project.path, host, t.id, title);
                      revealTab({ kind: "terminal", terminalId: t.id });
                    }}
                  >
                    {title || `Terminal ${t.id.slice(0, 6)}`}
                  </button>
                  {openTerminalIds.has(t.id) && <span className="shrink-0 text-[10px] text-muted-foreground">open</span>}
                  {/* The kill X is destructive and one stray click away on
                      every row, so it is reserved for the project the user has
                      actually selected — the same precondition revealTab
                      applies. The inventory stays browsable without it. */}
                  {isActive && (
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
                  )}
                </div>
                );
              })
            )}
          </div>
        </div>
      )}
    </div>
  );
}
