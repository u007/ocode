import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { formatEntry, logClear, logSnapshot, logSubscribe, type LogLevel } from "../log";

/** The pane's activity log: bridge, ocode requests, tool calls and errors, with timings. */
export function LogsView() {
  const entries = useSyncExternalStore(logSubscribe, logSnapshot);
  const [only, setOnly] = useState<"all" | LogLevel>("all");
  const [copied, setCopied] = useState<string | null>(null);
  const end = useRef<HTMLDivElement>(null);
  const shown = only === "all" ? entries : entries.filter((e) => e.level === only);
  useEffect(() => { end.current?.scrollIntoView?.({ block: "end" }); }, [shown.length]);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(shown.map(formatEntry).join("\n"));
      setCopied("Copied");
    } catch (e) {
      setCopied(`Copy failed: ${(e as Error).message}`);
    }
  };

  return (
    <div className="logs">
      <div className="row logbar">
        <select value={only} onChange={(e) => setOnly(e.target.value as "all" | LogLevel)} aria-label="Level">
          <option value="all">All</option><option value="info">Info</option><option value="warn">Warnings</option><option value="error">Errors</option>
        </select>
        <button onClick={() => void copy()}>Copy</button>
        <button onClick={logClear}>Clear</button>
        <span className="muted">{copied ?? `${shown.length} lines`}</span>
      </div>
      <div className="loglines" role="log">
        {shown.map((e) => (
          <div key={e.id} className={`logline ${e.level}`}>
            <span className="t">{new Date(e.at).toLocaleTimeString([], { hour12: false })}</span>{" "}
            <b>{e.source}</b> {e.msg}{e.ms === undefined ? "" : <span className="ms"> {e.ms} ms</span>}
          </div>
        ))}
        <div ref={end} />
      </div>
    </div>
  );
}
