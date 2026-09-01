import { useMemo, useState } from "react";
import type { ConsoleEvent, NetworkEvent } from "../../lib/browserStore";

export interface DevConsoleProps {
  consoleEvents: ConsoleEvent[];
  networkEvents: NetworkEvent[];
  onClearConsole: () => void;
  onClearNetwork: () => void;
}

const levelColor: Record<string, string> = {
  error: "text-red-500",
  warn: "text-amber-500",
  info: "text-blue-400",
  log: "text-neutral-300",
  debug: "text-neutral-500",
};

// Entries are appended in arrival order (oldest→newest); store ring-buffers at
// CONSOLE_CAP/NETWORK_CAP (1000) each, so no pagination is needed — the cap
// bounds the list.
export function DevConsole(props: DevConsoleProps) {
  const [tab, setTab] = useState<"console" | "network">("console");
  const [filter, setFilter] = useState("");
  const [open, setOpen] = useState(false); // drawer collapsed by default

  const consoleRows = useMemo(
    () => props.consoleEvents.filter((e) => e.text.toLowerCase().includes(filter.toLowerCase())),
    [props.consoleEvents, filter],
  );
  const netRows = useMemo(
    () => props.networkEvents.filter((e) => e.url.toLowerCase().includes(filter.toLowerCase())),
    [props.networkEvents, filter],
  );

  return (
    <div className={`flex flex-col text-xs font-mono border-t border-neutral-200 dark:border-neutral-800 shrink-0 ${open ? "h-48" : ""}`}>
      <div className="flex items-center gap-2 px-2 py-1 shrink-0">
        <button aria-label={open ? "Collapse console" : "Expand console"} aria-expanded={open}
          onClick={() => setOpen((o) => !o)} className="w-4 opacity-60">{open ? "▾" : "▸"}</button>
        <button role="tab" aria-selected={tab === "console"} onClick={() => setTab("console")}
          className={tab === "console" ? "font-bold" : "opacity-60"}>Console</button>
        <button role="tab" aria-selected={tab === "network"} onClick={() => setTab("network")}
          className={tab === "network" ? "font-bold" : "opacity-60"}>Network</button>
        <input aria-label="Filter" value={filter} onChange={(e) => setFilter(e.target.value)}
          placeholder="filter" className="ml-auto rounded bg-neutral-100 dark:bg-neutral-900 px-2 py-0.5" />
        <button aria-label="Clear"
          onClick={tab === "console" ? props.onClearConsole : props.onClearNetwork}>Clear</button>
      </div>
      {open && <div className="flex-1 overflow-auto px-2 pb-2">
        {tab === "console"
          ? consoleRows.map((e, i) => (
              <div key={i} className={levelColor[e.level] ?? ""}>{e.text}</div>
            ))
          : (
            <table className="w-full">
              <tbody>
                {netRows.map((e, i) => (
                  <tr key={i} className={e.status >= 400 ? "text-red-500" : ""}>
                    <td className="pr-2">{e.method}</td>
                    <td className="pr-2 truncate max-w-96">{e.url}</td>
                    <td className="pr-2 tabular-nums">{e.status}</td>
                    <td className="tabular-nums">{e.durationMs}ms</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
      </div>}
    </div>
  );
}
