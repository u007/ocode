import { useEffect, useImperativeHandle, useRef, useState, forwardRef } from "react";
import { Plus, X } from "lucide-react";
import TerminalPanel from "./TerminalPanel";
import { useTerminalConfig } from "@/hooks/useTerminalConfig";
import { loadSessionTerminals, saveSessionTerminals } from "./terminalPersistence";

interface TerminalInstance {
  id: string;
  title: string;
}

let nextTerminalSeq = 1;

function newTerminal(): TerminalInstance {
  const n = nextTerminalSeq++;
  return { id: `term-${n}-${Date.now()}`, title: `Terminal ${n}` };
}

/** Keeps the module-level title counter ahead of any restored terminal
 *  numbers, so a newly opened terminal after a restore doesn't reuse a
 *  "Terminal N" title that's already on screen. */
function bumpSeqPast(titles: string[]) {
  for (const title of titles) {
    const m = /^Terminal (\d+)$/.exec(title);
    if (m) nextTerminalSeq = Math.max(nextTerminalSeq, Number(m[1]) + 1);
  }
}

/**
 * Tab strip over N independent terminals. Each tab owns a TerminalPanel, which
 * owns one WebSocket and therefore one pty process on the server; closing a
 * tab unmounts its panel, which closes the socket, which kills the pty.
 *
 * Inactive tabs stay mounted (hidden with `display: none`) so background
 * shells keep running and keep their scrollback. State is local to this
 * component on purpose — it is scoped to the session tab, like the other
 * sub-tab panels, and has no reason to live in the global project store.
 */
export interface TerminalTabsHandle {
  openTerminal: () => void;
}

const TerminalTabs = forwardRef<TerminalTabsHandle, { active: boolean; projectPath: string; sessionId: string }>(
  function TerminalTabs({ active, projectPath, sessionId }, ref) {
    const { available, loading, error, scrollbackLines } = useTerminalConfig();
    const [terminals, setTerminals] = useState<TerminalInstance[]>([]);
    const [activeId, setActiveId] = useState<string>("");
    // This panel is force-mounted for every session tab (matching LogPanel's
    // hidden-but-mounted pattern), so the first shell is spawned only once the
    // Terminal sub-tab is actually opened — otherwise every session tab would
    // start a pty on page load.
    const autoOpened = useRef(false);
    // Set once a restore (or its no-op fallback) has run, so the persistence
    // effect below never overwrites storage with an empty in-flight state.
    const restored = useRef(false);
    // The restore effect's setTerminals/setActiveId calls don't take effect
    // until the next render, but the persistence effect below runs in the
    // same commit with the pre-restore (empty) `terminals` closure — so skip
    // exactly one save right after restoring to avoid clobbering storage with
    // that stale empty state before the real state renders.
    const skipNextSave = useRef(false);

    const openTerminal = () => {
      const term = newTerminal();
      setTerminals((prev) => [...prev, term]);
      setActiveId(term.id);
    };

    useImperativeHandle(ref, () => ({
      openTerminal,
    }));

    // Restore this session's previously open terminal tabs (fresh shells, prior
    // scrollback replayed by TerminalPanel) instead of always starting with one
    // new terminal. Runs once the terminal sub-tab is actually opened, matching
    // the original lazy-open behaviour.
    useEffect(() => {
      if (!active || !available || autoOpened.current) return;
      autoOpened.current = true;
      const saved = loadSessionTerminals(sessionId);
      skipNextSave.current = true;
      if (saved) {
        bumpSeqPast(saved.terminals.map((t) => t.title));
        setTerminals(saved.terminals);
        setActiveId(
          saved.activeId && saved.terminals.some((t) => t.id === saved.activeId)
            ? saved.activeId
            : saved.terminals[saved.terminals.length - 1].id,
        );
        restored.current = true;
        return;
      }
      restored.current = true;
      openTerminal();
    }, [active, available, sessionId]);

    useEffect(() => {
      if (!restored.current) return;
      if (skipNextSave.current) {
        skipNextSave.current = false;
        return;
      }
      saveSessionTerminals(sessionId, terminals, activeId);
    }, [sessionId, terminals, activeId]);

    const closeTerminal = (id: string) => {
      // Buffer cleanup happens via GC in the persistence effect once this id
      // drops out of `terminals` — not here, since TerminalPanel's own unmount
      // (which fires after this state update) re-saves its buffer one last
      // time, which would race an eager clear.
      setTerminals((prev) => {
        const next = prev.filter((t) => t.id !== id);
        if (id === activeId) {
          setActiveId(next.length > 0 ? next[next.length - 1].id : "");
        }
        return next;
      });
    };

    if (loading || (available && scrollbackLines <= 0)) {
      return <div className="p-4 text-sm text-zinc-500">Checking terminal availability…</div>;
    }

    if (error) {
      return <div className="p-4 text-sm text-red-400">Failed to read terminal setting: {error}</div>;
    }

    if (!available) {
      return (
        <div className="p-4 text-sm text-zinc-400">
          The interactive terminal is unavailable on this server: it requires server
          authentication or a loopback bind address.
        </div>
      );
    }

    return (
      <div className="flex h-full flex-col bg-zinc-900">
        <div className="flex h-9 shrink-0 items-center gap-1 overflow-x-auto border-b border-zinc-700 bg-zinc-900 px-2">
          {terminals.map((t) => (
            <div
              key={t.id}
              className={`flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-sm transition-colors ${
                t.id === activeId
                  ? "bg-zinc-700 text-white"
                  : "text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
              }`}
            >
              <button type="button" onClick={() => setActiveId(t.id)} className="whitespace-nowrap">
                {t.title}
              </button>
              <button
                type="button"
                onClick={() => closeTerminal(t.id)}
                aria-label={`Close ${t.title}`}
                title={`Close ${t.title}`}
                className="rounded p-0.5 text-zinc-500 hover:bg-zinc-600 hover:text-white"
              >
                <X className="h-3 w-3" />
              </button>
            </div>
          ))}
          <button
            type="button"
            onClick={openTerminal}
            aria-label="New terminal"
            title="New terminal"
            className="shrink-0 rounded p-1 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
          >
            <Plus className="h-4 w-4" />
          </button>
        </div>

        <div className="relative flex-1">
          {terminals.length === 0 ? (
            <div className="p-4 text-sm text-zinc-500">
              No terminals open. Use + to start one.
            </div>
          ) : (
            terminals.map((t) => (
              <div
                key={t.id}
                className={
                  t.id === activeId ? "absolute inset-0" : "absolute inset-0 hidden"
                }
              >
                <TerminalPanel
                  id={t.id}
                  active={active && t.id === activeId}
                  scrollbackLines={scrollbackLines}
                  projectPath={projectPath}
                />
              </div>
            ))
          )}
        </div>
      </div>
    );
  },
);

export default TerminalTabs;
