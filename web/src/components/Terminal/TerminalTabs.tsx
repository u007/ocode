import { useEffect, useImperativeHandle, forwardRef } from "react";
import TerminalPanel from "./TerminalPanel";
import ProcessesPanel from "./ProcessesPanel";
import { useTerminalConfig } from "@/hooks/useTerminalConfig";
import { useTerminalState, getProjectTerminals, PROCESSES_TAB_ID } from "../../stores/terminalStore";
import { focusTerminalById } from "./terminalFocus";

export interface TerminalTabsHandle {
  openTerminal: () => void;
  /** Close the active terminal instance. Returns false when there is none,
   *  so the caller (Cmd/Ctrl+W) can fall through to closing the session tab. */
  closeActiveTerminal: () => boolean;
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
const TerminalTabs = forwardRef<TerminalTabsHandle, { active: boolean; projectPath: string }>(
  function TerminalTabs({ active, projectPath }, ref) {
    const { available, loading, error, scrollbackLines, fontFamily, fontSize } = useTerminalConfig();
    const { state: terminalState, activate, openTerminal, closeTerminal } = useTerminalState();
    const { terminals: peekedTerminals, activeId, live } = getProjectTerminals(terminalState, projectPath);
    // Real panels (real pty + WebSocket) only exist once this project is live —
    // a peeked (never-visited) project's saved ids must stay pty-less until the
    // user actually switches to it, or every registered project would open a
    // shell in the background the moment the app loads. See terminalStore's
    // getProjectTerminals doc for the peek/live split this depends on.
    const terminals = live ? peekedTerminals : [];

    useEffect(() => {
      if (!active || !available) return;
      activate(projectPath);
    }, [active, available, projectPath, activate]);

    useImperativeHandle(ref, () => ({
      openTerminal: () => openTerminal(projectPath),
      closeActiveTerminal: () => {
        if (!activeId || !terminals.some((t) => t.id === activeId)) return false;
        // `closeTerminal` (the store action) returns false if the live terminal
        // is already gone — so a second synchronous call (same render tick)
        // falls through to false instead of removing a neighbour.
        return closeTerminal(projectPath, activeId);
      },
      focusTerminal: (id?: string) => {
        const target = id ?? activeId;
        if (!target || target === PROCESSES_TAB_ID) return;
        focusTerminalById(target);
      },
    }));

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
            <ProcessesPanel projectPath={projectPath} />
          </div>
        )}

        {terminals.map((t) => (
          <div key={t.id} className={t.id === activeId ? "absolute inset-0" : "absolute inset-0 hidden"}>
            <TerminalPanel
              id={t.id}
              active={active && t.id === activeId}
              scrollbackLines={scrollbackLines}
              fontFamily={fontFamily}
              fontSize={fontSize}
              projectPath={projectPath}
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
