import { useCallback, useMemo, useRef, useState } from "react";
import type { ConsoleEvent, NetworkEvent, ResponseBody } from "../../lib/browserStore";

export interface DevConsoleProps {
  consoleEvents: ConsoleEvent[];
  networkEvents: NetworkEvent[];
  responseBodies: Record<string, ResponseBody>;
  perfMetrics: Record<string, number>;
  onClearConsole: () => void;
  onClearNetwork: () => void;
  onRequestBody: (requestId: string) => void;
}

const levelColor: Record<string, string> = {
  error: "text-red-400",
  warn: "text-yellow-400",
  info: "text-sky-400",
  log: "text-neutral-200",
  debug: "text-neutral-400",
};

const levelBadge: Record<string, string> = {
  error: "Error",
  warn: "Warning",
  info: "Info",
  log: "",
  debug: "Debug",
};

const DEFAULT_HEIGHT = 240;
const MIN_HEIGHT = 80;
const MAX_HEIGHT_RATIO = 0.6;

function statusClass(status: number): string {
  if (status >= 500) return "text-red-400";
  if (status >= 400) return "text-orange-400";
  if (status >= 300) return "text-yellow-400";
  if (status >= 200) return "text-green-400";
  return "text-neutral-400";
}

function formatSize(bytes: number | undefined): string {
  if (bytes == null || bytes <= 0) return "—";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatDuration(ms: number): string {
  if (ms < 1) return "<1ms";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

/** Human-readable labels for common performance metrics. */
const perfLabels: Record<string, string> = {
  "Nodes": "DOM Nodes",
  "JSEventListeners": "JS Event Listeners",
  "Documents": "Documents",
  "Frames": "Frames",
  "JSMemoryUsed": "JS Memory Used",
  "Memory": "Heap Size",
  "LayoutCount": "Layout Count",
  "RecalcStyleCount": "Style Recalc Count",
  "LayoutDuration": "Layout Duration",
  "RecalcStyleDuration": "Style Recalc Duration",
  "ScriptDuration": "Script Duration",
  "TaskDuration": "Task Duration",
  "JSHeapUsedSize": "JS Heap Used",
  "JSHeapTotalSize": "JS Heap Total",
};

function formatPerfValue(key: string, value: number): string {
  if (key.includes("Duration")) return `${(value * 1000).toFixed(1)}ms`;
  if (key.includes("Size") || key === "Memory") return formatSize(value);
  if (Number.isInteger(value)) return value.toLocaleString();
  return value.toFixed(2);
}

export function DevConsole(props: DevConsoleProps) {
  const [tab, setTab] = useState<"console" | "network" | "performance">("console");
  const [filter, setFilter] = useState("");
  const [open, setOpen] = useState(false);
  const [height, setHeight] = useState(DEFAULT_HEIGHT);
  const [expandedRow, setExpandedRow] = useState<string | null>(null);
  const [bodyTab, setBodyTab] = useState<"headers" | "body">("headers");
  const dragRef = useRef<{ startY: number; startHeight: number } | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);

  const consoleRows = useMemo(
    () => props.consoleEvents.filter((e) => e.text.toLowerCase().includes(filter.toLowerCase())),
    [props.consoleEvents, filter],
  );
  const netRows = useMemo(
    () => props.networkEvents.filter((e) => e.url.toLowerCase().includes(filter.toLowerCase())),
    [props.networkEvents, filter],
  );

  const onDragStart = useCallback((e: React.PointerEvent) => {
    e.preventDefault();
    const container = containerRef.current;
    const maxHeight = container ? Math.floor(container.parentElement!.clientHeight * MAX_HEIGHT_RATIO) : 600;
    dragRef.current = { startY: e.clientY, startHeight: height };
    const onMove = (ev: PointerEvent) => {
      if (!dragRef.current) return;
      const delta = dragRef.current.startY - ev.clientY;
      const newH = Math.min(maxHeight, Math.max(MIN_HEIGHT, dragRef.current.startHeight + delta));
      setHeight(newH);
    };
    const onUp = () => {
      dragRef.current = null;
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  }, [height]);

  const toggleRow = useCallback((id: string) => {
    setExpandedRow((prev) => {
      const next = prev === id ? null : id;
      // Request body when expanding a row that doesn't have one yet.
      if (next && !props.responseBodies[next]) {
        props.onRequestBody(next);
      }
      return next;
    });
    setBodyTab("headers");
  }, [props.responseBodies, props.onRequestBody]);

  return (
    <div
      ref={containerRef}
      className="flex flex-col shrink-0 border-t border-neutral-700 dark:border-neutral-800 bg-neutral-900 dark:bg-neutral-950 text-xs font-mono select-none min-h-0"
      style={open ? { height } : undefined}
    >
      {open && (
        <div
          role="separator"
          aria-orientation="horizontal"
          aria-label="Resize console"
          tabIndex={0}
          onPointerDown={onDragStart}
          onKeyDown={(e) => {
            const step = e.shiftKey ? 50 : 10;
            if (e.key === "ArrowUp") { e.preventDefault(); setHeight((h) => Math.min(h + step, 600)); }
            else if (e.key === "ArrowDown") { e.preventDefault(); setHeight((h) => Math.max(h - step, MIN_HEIGHT)); }
          }}
          className="h-1.5 cursor-row-resize flex items-center justify-center hover:bg-neutral-700 active:bg-neutral-600 transition-colors shrink-0 group"
        >
          <div className="w-8 h-0.5 rounded-full bg-neutral-600 group-hover:bg-neutral-400" />
        </div>
      )}
      {/* Tab bar */}
      <div className="flex items-center gap-0 px-1 shrink-0 border-b border-neutral-700 dark:border-neutral-800">
        <button
          aria-label={open ? "Collapse console" : "Expand console"}
          aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
          className="px-1.5 py-1 text-neutral-400 hover:text-neutral-200 transition-colors"
        >
          {open ? "▾" : "▸"}
        </button>
        {(["console", "network", "performance"] as const).map((t) => (
          <button
            key={t}
            role="tab"
            aria-selected={tab === t}
            onClick={() => setTab(t)}
            className={`px-3 py-1.5 text-[11px] transition-colors border-b-2 capitalize ${
              tab === t
                ? "border-sky-500 text-neutral-100"
                : "border-transparent text-neutral-400 hover:text-neutral-200"
            }`}
          >
            {t}
          </button>
        ))}
        <div className="flex-1" />
        {tab !== "performance" && (
          <>
            <input
              aria-label="Filter"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter"
              className="w-32 rounded bg-neutral-800 border border-neutral-700 text-neutral-300 px-2 py-0.5 text-[11px] placeholder:text-neutral-500 focus:outline-none focus:border-neutral-500"
            />
            <button
              aria-label="Clear"
              onClick={tab === "console" ? props.onClearConsole : props.onClearNetwork}
              className="px-2 py-0.5 ml-1 text-[11px] text-neutral-400 hover:text-neutral-200 hover:bg-neutral-800 rounded transition-colors"
            >
              Clear
            </button>
          </>
        )}
      </div>
      {/* Content */}
      {open && (
        <div className="flex-1 overflow-auto text-[11px] leading-relaxed">
          {/* Console tab */}
          {tab === "console" && (
            <div className="px-2 py-1">
              {consoleRows.length === 0 ? (
                <div className="text-neutral-500 italic">No console output</div>
              ) : (
                consoleRows.map((e, i) => (
                  <div key={i} className={`flex items-start gap-1.5 py-px ${levelColor[e.level] ?? "text-neutral-200"}`}>
                    {levelBadge[e.level] && (
                      <span className="text-[10px] font-semibold uppercase opacity-70 shrink-0 mt-px">{levelBadge[e.level]}</span>
                    )}
                    <span className="break-all">{e.text}</span>
                  </div>
                ))
              )}
            </div>
          )}
          {/* Network tab */}
          {tab === "network" && (
            <div>
              {netRows.length === 0 ? (
                <div className="px-2 py-2 text-neutral-500 italic">No network activity</div>
              ) : (
                netRows.map((e, i) => {
                  const id = e.requestId || `${e.ts}-${i}`;
                  const isExpanded = expandedRow === id;
                  const hasDetails = !!(e.requestHeaders || e.responseHeaders || e.blocked || e.requestId);
                  const body: ResponseBody | undefined = props.responseBodies[e.requestId];
                  return (
                    <div key={id}>
                      <div
                        className={`flex items-center gap-2 px-2 py-0.5 border-b border-neutral-800/50 hover:bg-neutral-800/30 ${hasDetails ? "cursor-pointer" : ""}`}
                        onClick={() => hasDetails && toggleRow(id)}
                      >
                        <span className={`w-16 shrink-0 font-medium ${statusClass(e.status)}`}>
                          {e.status || "—"}
                        </span>
                        <span className="w-12 shrink-0 text-neutral-400">{e.method}</span>
                        <span className="flex-1 truncate text-neutral-200">{e.url}</span>
                        {e.blocked && (
                          <span className="shrink-0 px-1 py-0.5 rounded bg-red-900/40 text-red-400 text-[10px]">blocked</span>
                        )}
                        <span className="shrink-0 text-neutral-500 tabular-nums w-16 text-right">{formatDuration(e.durationMs)}</span>
                        <span className="shrink-0 text-neutral-500 tabular-nums w-14 text-right">{formatSize(e.size)}</span>
                        {hasDetails && (
                          <span className="shrink-0 text-neutral-600">{isExpanded ? "▾" : "▸"}</span>
                        )}
                      </div>
                      {/* Expanded detail */}
                      {isExpanded && (
                        <div className="px-3 py-2 bg-neutral-900/50 border-b border-neutral-800">
                          {e.contentType && (
                            <div className="mb-2">
                              <span className="text-neutral-500">Type: </span>
                              <span className="text-neutral-300">{e.contentType}</span>
                            </div>
                          )}
                          {e.blocked && (
                            <div className="mb-2 px-2 py-1 rounded bg-red-900/20 border border-red-800/40">
                              <span className="text-red-400 font-medium">Blocked: </span>
                              <span className="text-red-300">{e.blocked}</span>
                            </div>
                          )}
                          {/* Sub-tabs for headers/body */}
                          <div className="flex gap-2 mb-2 border-b border-neutral-800 pb-1">
                            <button
                              className={`text-[10px] ${bodyTab === "headers" ? "text-neutral-100 border-b border-sky-500" : "text-neutral-500"}`}
                              onClick={() => setBodyTab("headers")}
                            >
                              Headers
                            </button>
                            <button
                              className={`text-[10px] ${bodyTab === "body" ? "text-neutral-100 border-b border-sky-500" : "text-neutral-500"}`}
                              onClick={() => setBodyTab("body")}
                            >
                              Body
                            </button>
                          </div>
                          {bodyTab === "headers" && (
                            <div className="space-y-2">
                              {e.requestHeaders && Object.keys(e.requestHeaders).length > 0 && (
                                <div>
                                  <div className="text-neutral-500 mb-1 font-medium">Request Headers</div>
                                  <div className="pl-2 space-y-0.5">
                                    {Object.entries(e.requestHeaders).map(([k, v]) => (
                                      <div key={k}>
                                        <span className="text-sky-400">{k}</span>
                                        <span className="text-neutral-500">: </span>
                                        <span className="text-neutral-300 break-all">{v}</span>
                                      </div>
                                    ))}
                                  </div>
                                </div>
                              )}
                              {e.responseHeaders && Object.keys(e.responseHeaders).length > 0 && (
                                <div>
                                  <div className="text-neutral-500 mb-1 font-medium">Response Headers</div>
                                  <div className="pl-2 space-y-0.5">
                                    {Object.entries(e.responseHeaders).map(([k, v]) => (
                                      <div key={k}>
                                        <span className="text-green-400">{k}</span>
                                        <span className="text-neutral-500">: </span>
                                        <span className="text-neutral-300 break-all">{v}</span>
                                      </div>
                                    ))}
                                  </div>
                                </div>
                              )}
                              {!e.requestHeaders && !e.responseHeaders && (
                                <div className="text-neutral-500 italic">No headers available</div>
                              )}
                            </div>
                          )}
                          {bodyTab === "body" && (
                            <div className="space-y-3">
                              {/* Request Body */}
                              {e.postData && (
                                <div>
                                  <div className="text-neutral-500 mb-1 font-medium">Request Body</div>
                                  <pre className="whitespace-pre-wrap break-all text-neutral-300 max-h-32 overflow-auto bg-neutral-800/30 rounded p-2">{e.postData}</pre>
                                </div>
                              )}
                              {/* Response Body */}
                              <div>
                                <div className="text-neutral-500 mb-1 font-medium">Response Body</div>
                                {body?.error && (
                                  <div className="text-red-400 mb-1">{body.error}</div>
                                )}
                                {body?.body != null && (
                                  <div>
                                    {body.truncated && (
                                      <div className="text-yellow-400 mb-1 text-[10px]">Body truncated at 256KB</div>
                                    )}
                                    {body.base64Encoded ? (
                                      <div className="text-neutral-500 italic">Binary content ({formatSize(body.body.length)} base64)</div>
                                    ) : (
                                      <pre className="whitespace-pre-wrap break-all text-neutral-300 max-h-32 overflow-auto bg-neutral-800/30 rounded p-2">{body.body}</pre>
                                    )}
                                  </div>
                                )}
                                {body == null && (
                                  <div className="text-neutral-500 italic">
                                    {e.requestId ? "Loading body…" : "Body not available"}
                                  </div>
                                )}
                              </div>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  );
                })
              )}
            </div>
          )}
          {/* Performance tab */}
          {tab === "performance" && (
            <div className="px-2 py-1">
              {Object.keys(props.perfMetrics).length === 0 ? (
                <div className="text-neutral-500 italic">No performance data yet</div>
              ) : (
                <table className="w-full text-left">
                  <thead>
                    <tr className="text-neutral-500 border-b border-neutral-800">
                      <th className="font-normal pr-4 pb-0.5">Metric</th>
                      <th className="font-normal pb-0.5 text-right">Value</th>
                    </tr>
                  </thead>
                  <tbody>
                    {Object.entries(props.perfMetrics)
                      .sort(([a], [b]) => a.localeCompare(b))
                      .map(([key, value]) => (
                        <tr key={key} className="border-b border-neutral-800/50">
                          <td className="py-0.5 pr-4 text-neutral-300">{perfLabels[key] ?? key}</td>
                          <td className="py-0.5 text-right tabular-nums text-neutral-200">{formatPerfValue(key, value)}</td>
                        </tr>
                      ))}
                  </tbody>
                </table>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
