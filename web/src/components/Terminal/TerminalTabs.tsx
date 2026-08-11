import { useEffect, useRef, useState } from "react";
import { Plus, X } from "lucide-react";
import TerminalPanel from "./TerminalPanel";
import { useTerminalConfig } from "@/hooks/useTerminalConfig";

interface TerminalInstance {
  id: string;
  title: string;
}

let nextTerminalSeq = 1;

function newTerminal(): TerminalInstance {
  const n = nextTerminalSeq++;
  return { id: `term-${n}-${Date.now()}`, title: `Terminal ${n}` };
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
export default function TerminalTabs({ active, projectPath }: { active: boolean; projectPath: string }) {
  const { available, loading, error, scrollbackLines } = useTerminalConfig(projectPath);
  const [terminals, setTerminals] = useState<TerminalInstance[]>([]);
  const [activeId, setActiveId] = useState<string>("");
  // This panel is force-mounted for every session tab (matching LogPanel's
  // hidden-but-mounted pattern), so the first shell is spawned only once the
  // Terminal sub-tab is actually opened — otherwise every session tab would
  // start a pty on page load.
  const autoOpened = useRef(false);

  const openTerminal = () => {
    const term = newTerminal();
    setTerminals((prev) => [...prev, term]);
    setActiveId(term.id);
  };

  useEffect(() => {
    if (!active || !available || autoOpened.current) return;
    autoOpened.current = true;
    openTerminal();
  }, [active, available]);

  const closeTerminal = (id: string) => {
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
        authentication or a loopback bind address, and the selected project must match
        the server&apos;s working directory.
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
              <TerminalPanel active={active && t.id === activeId} scrollbackLines={scrollbackLines} />
            </div>
          ))
        )}
      </div>
    </div>
  );
}
