import { useState, useEffect, useRef } from "react";
import { Button } from "@/components/ui/button";
import { apiPath, authToken, authHeaders } from "@/api/client";
import { Trash2, Pause, Play, Filter } from "lucide-react";

interface LogEntry {
  kind: string;
  message: string;
}

const KIND_COLORS: Record<string, string> = {
  LLM: "text-purple-400",
  TOOL: "text-green-400",
  AGENT: "text-blue-400",
  ERROR: "text-red-400",
  SESSION: "text-yellow-400",
  GIT: "text-cyan-400",
};

const KIND_FILTERS = ["ALL", "LLM", "TOOL", "AGENT", "ERROR", "SESSION", "GIT"];

export default function LogPanel() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [streaming, setStreaming] = useState(true);
  const [filter, setFilter] = useState("ALL");
  const [autoScroll, setAutoScroll] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const eventSourceRef = useRef<EventSource | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    // Load initial logs
    fetch(apiPath("/api/logs"), { headers: authHeaders() })
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then((data) => {
        setLogs(data as LogEntry[]);
        setError(null);
      })
      .catch((err) => {
        console.error("Failed to fetch logs:", err);
        setError("Failed to load logs");
      });
  }, []);

  useEffect(() => {
    if (!streaming) {
      eventSourceRef.current?.close();
      return;
    }

    const token = authToken();
    const es = new EventSource(apiPath(`/api/logs/stream${token ? `?token=${token}` : ""}`));
    eventSourceRef.current = es;

    es.onmessage = (e) => {
      try {
        const entry = JSON.parse(e.data) as LogEntry;
        setLogs((prev) => [...prev, entry]);
      } catch {
        // Ignore parse errors
      }
    };

    es.onerror = () => {
      es.close();
      console.warn("SSE connection lost, reconnecting in 3s…");
      setTimeout(() => {
        if (streaming) setStreaming((s) => s); // trigger reconnect
      }, 3000);
    };

    return () => {
      es.close();
    };
  }, [streaming]);

  useEffect(() => {
    if (autoScroll) {
      bottomRef.current?.scrollIntoView({ behavior: "smooth" });
    }
  }, [logs, autoScroll]);

  const handleClear = async () => {
    if (!window.confirm("Clear all logs?")) return;
    try {
      await fetch(apiPath("/api/logs"), { method: "DELETE", headers: authHeaders() });
      setLogs([]);
    } catch (err) {
      console.error("Failed to clear logs:", err);
    }
  };

  const handleScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 30;
    setAutoScroll(atBottom);
  };

  const filteredLogs = filter === "ALL" ? logs : logs.filter((l) => l.kind === filter);

  return (
    <div className="flex flex-col h-full">
      {/* Toolbar */}
      <div className="flex items-center justify-between px-4 py-2 border-b border-zinc-700 bg-zinc-900">
        <div className="flex items-center gap-2">
          <Filter className="w-4 h-4 text-zinc-500" />
          <div className="flex gap-1">
            {KIND_FILTERS.map((kind) => (
              <button
                key={kind}
                type="button"
                onClick={() => setFilter(kind)}
                className={`px-2 py-1 rounded text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-900 ${
                  filter === kind
                    ? "bg-zinc-700 text-white"
                    : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800"
                }`}
              >
                {kind}
              </button>
            ))}
          </div>
        </div>

        <div className="flex items-center gap-2">
          <span className="text-xs text-zinc-500">{filteredLogs.length} entries</span>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => setStreaming(!streaming)}
            title={streaming ? "Pause streaming" : "Resume streaming"}
          >
            {streaming ? (
              <Pause className="w-4 h-4" />
            ) : (
              <Play className="w-4 h-4" />
            )}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => setAutoScroll(!autoScroll)}
            title={autoScroll ? "Disable auto-scroll" : "Enable auto-scroll"}
            className={autoScroll ? "text-blue-400" : "text-zinc-500"}
          >
            ↓
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={handleClear}
            title="Clear logs"
          >
            <Trash2 className="w-4 h-4" />
          </Button>
        </div>
      </div>

      {/* Log entries */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto font-mono text-xs p-4 bg-zinc-950"
      >
        {error ? (
          <div className="text-red-400 text-center py-8">{error}</div>
        ) : filteredLogs.length === 0 ? (
          <div className="text-zinc-500 text-center py-8">
            {streaming ? "Waiting for logs..." : "No log entries"}
          </div>
        ) : (
          filteredLogs.map((log, i) => (
            <div
              key={i}
              className="flex items-start gap-3 py-1 hover:bg-zinc-900 rounded"
            >
              <span
                className={`flex-shrink-0 w-16 text-right ${
                  KIND_COLORS[log.kind] || "text-zinc-400"
                }`}
              >
                {log.kind}
              </span>
              <span className="flex-1 text-zinc-300 whitespace-pre-wrap break-all">
                {log.message}
              </span>
            </div>
          ))
        )}
        <div ref={bottomRef} />
      </div>
    </div>
  );
}
