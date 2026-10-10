import { useEffect, useImperativeHandle, useRef, forwardRef } from "react";
import TerminalPanel from "./TerminalPanel";
import ProcessesPanel from "./ProcessesPanel";
import { useTerminalConfig } from "@/hooks/useTerminalConfig";
import { useTerminalState, getProjectTerminals, PROCESSES_TAB_ID } from "../../stores/terminalStore";
import { focusTerminalById } from "./terminalFocus";

export interface TerminalTabsHandle {
  openTerminal: () => void;
  /** Close the active terminal instance. Returns `null` when there was none to
   *  close (no active terminal, or the Processes sentinel), so the caller
   *  (Cmd/Ctrl+W) can fall through to closing the session tab; otherwise the
   *  number of terminals the project has LEFT, `0` meaning this close emptied
   *  it. The count comes from the store after the removal, so callers never
   *  have to reconcile it against a pre-close snapshot. */
  closeActiveTerminal: () => number | null;
  /** Focus the xterm of the given terminal id, or the currently active one if omitted. */
  focusTerminal: (id?: string) => void;
}

/**
 * Content-only: renders the active terminal/Processes panel for one project.
 * The tab strip (open/close/rename/reorder/+) lives in UnifiedTabBar, which
 * shares this project's terminal state via terminalStore. This component
 * still triggers activation (restoring persisted terminals, or spawning one
 * fresh, the first time this project's terminal region is actually shown —
 * never just from the project becoming active) and stays always-mounted per
 * open project so ptys survive tab/project switches (see App.tsx).
 */
const TerminalTabs = forwardRef<TerminalTabsHandle, { active: boolean; projectPath: string; host?: string }>(
  function TerminalTabs({ active, projectPath, host }, ref) {
    const { available, loading, error, scrollbackLines, fontFamily, fontSize } = useTerminalConfig();
    const { state: terminalState, activate, openTerminal, closeTerminal } = useTerminalState();
    const { terminals: peekedTerminals, activeId, live } = getProjectTerminals(terminalState, projectPath, host);
    // Real panels (real pty + WebSocket) only exist once this project is live —
    // a peeked (never-visited) project's saved ids must stay pty-less until the
    // user actually switches to it, or every registered project would open a
    // shell in the background the moment the app loads. See terminalStore's
    // getProjectTerminals doc for the peek/live split this depends on.
    const terminals = live ? peekedTerminals : [];

    useEffect(() => {
      if (!active || !available) return;
      activate(projectPath, host);
    }, [active, available, projectPath, host, activate]);

    useImperativeHandle(ref, () => ({
      openTerminal: () => openTerminal(projectPath, host),
      closeActiveTerminal: () => {
        // Only the Processes sentinel (and "no active terminal") short-circuits
        // here. Whether the id is actually still open is deliberately NOT
        // checked against the rendered `terminals`: that snapshot can be one
        // commit behind the store when a cross-client refetch lands just before
        // the click, and `closeTerminal` re-checks against live state anyway
        // (returning null rather than removing a neighbour). Two synchronous
        // calls in one tick therefore behave the same: the first closes, the
        // second returns null.
        if (!activeId || activeId === PROCESSES_TAB_ID) return null;
        return closeTerminal(projectPath, activeId, host);
      },
      focusTerminal: (id?: string) => {
        const target = id ?? activeId;
        if (!target || target === PROCESSES_TAB_ID) return;
        focusTerminalById(target);
      },
    }));

    // Lazy panel mount: a TerminalPanel attaches a WebSocket, restores its
    // history and creates a WebGL context, and TerminalTabs is mounted
    // (hidden) for EVERY project with terminals — so mounting every panel up
    // front is what turned a restart with a dozen persisted terminals into a
    // boot stall. A panel mounts the first time its terminal is the active
    // one while this project's terminal pane is shown, and stays mounted
    // (hidden) afterwards so switching back never re-restores it.
    const shownRef = useRef<Set<string>>(new Set());
    if (active && activeId && activeId !== PROCESSES_TAB_ID) shownRef.current.add(activeId);

    if (loading || (available && scrollbackLines <= 0)) {
      return <div className="p-4 text-sm text-muted-foreground">Checking terminal availability…</div>;
    }

    if (error) {
      return <div className="p-4 text-sm text-red-400">Failed to read terminal setting: {error}</div>;
    }

    if (!available) {
      return (
        <div className="p-4 text-sm text-muted-foreground">
          The interactive terminal is unavailable on this server: it requires server
          authentication or a loopback bind address.
        </div>
      );
    }

    return (
      <div className="relative h-full bg-card">
        {active && (
          <div className={activeId === PROCESSES_TAB_ID ? "absolute inset-0" : "absolute inset-0 hidden"}>
            <ProcessesPanel projectPath={projectPath} host={host} />
          </div>
        )}

        {terminals.filter((t) => shownRef.current.has(t.id)).map((t) => (
          <div key={t.id} className={t.id === activeId ? "absolute inset-0" : "absolute inset-0 hidden"}>
            <TerminalPanel
              id={t.id}
              active={active && t.id === activeId}
              scrollbackLines={scrollbackLines}
              fontFamily={fontFamily}
              fontSize={fontSize}
              projectPath={projectPath}
              host={host}
            />
          </div>
        ))}
        {terminals.length === 0 && activeId !== PROCESSES_TAB_ID && (
          <div className="p-4 text-sm text-muted-foreground">No terminals open. Use ⌨️+ in the tab bar to start one.</div>
        )}
      </div>
    );
  },
);

export default TerminalTabs;
