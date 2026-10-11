import { useState } from "react";
import { ChevronDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useProjectState } from "@/stores/projectStore";
import { cn } from "../../lib/utils";
import { useJumpToTerminal } from "../../lib/jumpToSession";
import type { PulseTerminal } from "../../api/types";
import { pulseProjectBasename } from "./pulseFilter";
import { usePulseTerminals } from "./usePulseTerminals";

/**
 * PulseTerminals — the live terminals across every project, shown above the
 * session sections. A running program (a dev server, a build, a test watcher)
 * is what someone opens this dashboard to see, so it leads each row and its
 * command line is shown in full. Idle shells sort last and read as idle.
 *
 * Each row has a chevron that expands its details (full command, project path,
 * pid, terminal id) and an Open button that jumps to the terminal in its project.
 * A terminal is not a session, so there is no ask to answer here. The row's
 * controls are buttons, but the rows keep no `role="listitem"`, so they stay out
 * of PulseView's arrow-key roving over the session cards.
 */
export function PulseTerminals() {
  const { terminals, total, error, unavailable, hasMore, loadMore } = usePulseTerminals();
  // The server refuses the list outright (no auth on a non-loopback bind, or no
  // pty on Windows), so there is no section to show.
  if (unavailable) return null;

  return (
    <section aria-label="Terminals" data-testid="pulse-terminals">
      <h2 className="text-xs font-medium uppercase tracking-wide text-muted-foreground mb-2">
        Terminals ({total})
      </h2>
      {error && (
        <p role="alert" className="mb-2 text-xs text-destructive">
          {error}
        </p>
      )}
      {!error && total === 0 && <p className="text-xs text-muted-foreground">No open terminals.</p>}
      {terminals.length > 0 && (
        <ul role="list" className="grid grid-cols-1 gap-2 sm:grid-cols-2">
          {terminals.map((t) => (
            <PulseTerminalRow key={t.id} terminal={t} />
          ))}
        </ul>
      )}
      {hasMore && (
        <div className="mt-2 flex justify-center">
          <Button variant="outline" size="sm" onClick={loadMore}>
            Load more
          </Button>
        </div>
      )}
    </section>
  );
}

function PulseTerminalRow({ terminal }: { terminal: PulseTerminal }) {
  const label = terminal.title || terminal.id;
  const [expanded, setExpanded] = useState(false);
  const openTerminal = useJumpToTerminal();
  const { state } = useProjectState();
  // Pulse terminals are local, so only a local sidebar project can be opened.
  const projectKnown = state.projects.some((p) => p.path === terminal.project && !p.host);
  return (
    <li
      data-testid="pulse-terminal-row"
      data-running={terminal.running ? "true" : "false"}
      className={cn(
        "flex min-w-0 flex-col gap-1.5 rounded-md border border-border bg-card px-2 py-1.5",
        terminal.running && "border-emerald-500/40",
      )}
    >
      <div className="flex min-w-0 items-center gap-2">
        <button
          type="button"
          aria-expanded={expanded}
          aria-label={expanded ? "Hide terminal details" : "Show terminal details"}
          title={expanded ? "Hide terminal details" : "Show terminal details"}
          onClick={() => setExpanded((v) => !v)}
          className="shrink-0 rounded p-0.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <ChevronDown className={cn("h-3 w-3 transition-transform", expanded && "rotate-180")} aria-hidden />
        </button>
        <span
          aria-label={terminal.running ? "running program" : "idle shell"}
          className={cn(
            "shrink-0 text-xs leading-none",
            terminal.running ? "text-emerald-400" : "text-muted-foreground",
          )}
        >
          {terminal.running ? "●" : "○"}
        </span>
        <span title={terminal.project} className="shrink-0 truncate text-xs text-muted-foreground">
          {pulseProjectBasename(terminal.project)}
        </span>
        <span className="min-w-0 flex-1 truncate text-xs">{label}</span>
        <span
          data-testid="pulse-terminal-command"
          title={terminal.command}
          className={cn(
            "min-w-0 max-w-[40%] truncate font-mono text-[11px]",
            terminal.running ? "text-foreground" : "text-muted-foreground",
          )}
        >
          {terminal.running ? terminal.command : "idle"}
        </span>
        <Button
          variant="outline"
          size="sm"
          className="h-6 shrink-0 px-2 text-xs"
          disabled={!projectKnown}
          title={projectKnown ? `Open ${label} in its project` : "This project is not in the sidebar"}
          onClick={() =>
            void openTerminal({ projectPath: terminal.project, terminalId: terminal.id, title: terminal.title })
          }
        >
          Open
        </Button>
      </div>
      {expanded && (
        <dl data-testid="pulse-terminal-details" className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 pl-5 text-[11px]">
          <dt className="text-muted-foreground">Command</dt>
          <dd className="break-all font-mono">{terminal.command}</dd>
          <dt className="text-muted-foreground">Project</dt>
          <dd className="break-all font-mono">{terminal.project}</dd>
          <dt className="text-muted-foreground">PID</dt>
          <dd className="tabular-nums">{terminal.pid}</dd>
          <dt className="text-muted-foreground">Terminal ID</dt>
          <dd className="break-all font-mono">{terminal.id}</dd>
        </dl>
      )}
    </li>
  );
}
