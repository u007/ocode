import { useCallback, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import type { CronRun } from "@/api/types";
import { api } from "@/api/client";
import { Clock, ChevronDown, ChevronUp, RefreshCw } from "lucide-react";

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  const s = Math.floor(ms / 1000);
  const m = Math.floor(s / 60);
  const remS = s % 60;
  if (m === 0) return `${remS}s`;
  return `${m}m ${remS}s`;
}

function formatDateTime(iso: string): string {
  try {
    const d = new Date(iso);
    return d.toLocaleString();
  } catch {
    return iso;
  }
}

export default function CronHistoryPanel({
  jobId,
  jobName,
  onClose,
}: {
  jobId: string;
  jobName: string;
  onClose: () => void;
}) {
  const [runs, setRuns] = useState<CronRun[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [offset, setOffset] = useState(0);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const limit = 20;

  const load = useCallback(async (off = 0, append = false) => {
    try {
      setError(null);
      if (!append) setLoading(true);
      const res = await api.getCronRuns(jobId, limit, off);
      if (append) {
        setRuns((prev) => [...prev, ...(res.runs ?? [])]);
      } else {
        setRuns(res.runs ?? []);
      }
      setTotal(res.total);
      setOffset(off + (res.runs ?? []).length);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load run history");
    } finally {
      setLoading(false);
    }
  }, [jobId]);

  useEffect(() => {
    setRuns([]);
    setOffset(0);
    setExpandedId(null);
    void load(0, false);
  }, [load]);

  const hasMore = runs.length < total;

  return (
    <section className="rounded-lg border border-border bg-card/80 p-4">
      <div className="mb-4 flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <Clock className="h-4 w-4 text-blue-400" />
          <div>
            <h2 className="text-sm font-semibold text-foreground">Run history — {jobName}</h2>
            <p className="text-xs text-muted-foreground">
              {total} run{total !== 1 ? "s" : ""} · rundate, duration, input/output & datetime logs
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => void load(0, false)}>
            <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
            Refresh
          </Button>
          <Button variant="ghost" size="sm" onClick={onClose}>
            Close
          </Button>
        </div>
      </div>

      {error && (
        <div className="mb-3 rounded-md border border-red-900 bg-red-950/60 px-3 py-2 text-sm text-red-200">
          {error}
        </div>
      )}

      {loading ? (
        <div className="py-8 text-center text-sm text-muted-foreground">Loading run history…</div>
      ) : runs.length === 0 ? (
        <div className="rounded-md border border-dashed border-border px-3 py-6 text-center text-sm text-muted-foreground">
          No runs recorded for this job yet. Runs appear after the scheduler fires the job.
        </div>
      ) : (
        <div className="space-y-2 max-h-[420px] overflow-auto pr-1">
          {runs.map((run) => {
            const isExpanded = expandedId === run.id;
            const statusColor =
              run.status === "error" ? "text-red-300 border-red-800 bg-red-950/40" : "text-emerald-300 border-emerald-800 bg-emerald-950/40";
            return (
              <div key={run.id} className="rounded-md border border-border bg-background/70 p-3 text-sm">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs ${statusColor}`}>
                        {run.status}
                      </span>
                      <span className="text-xs text-muted-foreground">id {run.id}</span>
                      <span className="text-xs text-muted-foreground">· {formatDuration(run.duration_ms)}</span>
                    </div>
                    <div className="mt-1 text-xs text-muted-foreground">
                      <span className="font-medium text-foreground">Rundate:</span> {formatDateTime(run.started_at)} → {formatDateTime(run.finished_at)}
                    </div>
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 px-2 text-xs text-muted-foreground hover:text-foreground"
                    onClick={() => setExpandedId(isExpanded ? null : run.id)}
                  >
                    {isExpanded ? <ChevronUp className="mr-1 h-3.5 w-3.5" /> : <ChevronDown className="mr-1 h-3.5 w-3.5" />}
                    {isExpanded ? "Hide" : "View"}
                  </Button>
                </div>

                {/* Preview when collapsed */}
                {!isExpanded && (
                  <div className="mt-2 text-xs text-muted-foreground truncate">Input: {run.input.slice(0, 80)}{run.input.length > 80 ? "…" : ""}</div>
                )}

                {/* Expanded detail */}
                {isExpanded && (
                  <div className="mt-3 space-y-3 border-t border-border pt-3">
                    <div>
                      <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Input</div>
                      <div className="whitespace-pre-wrap rounded bg-card p-2 text-xs text-foreground">{run.input || "—"}</div>
                    </div>
                    <div>
                      <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Output</div>
                      <div className="whitespace-pre-wrap rounded bg-card p-2 text-xs text-foreground">{run.output || "—"}</div>
                    </div>
                    {run.error && (
                      <div>
                        <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-red-400">Error</div>
                        <div className="whitespace-pre-wrap rounded bg-red-950/40 p-2 text-xs text-red-200">{run.error}</div>
                      </div>
                    )}
                    <div>
                      <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Logs (datetime)</div>
                      {run.logs && run.logs.length > 0 ? (
                        <div className="space-y-1 rounded bg-card p-2">
                          {run.logs.map((log, idx) => (
                            <div key={idx} className="flex gap-2 text-xs">
                              <span className="whitespace-nowrap font-mono text-muted-foreground">{formatDateTime(log.at)}</span>
                              <span className={log.level === "error" ? "text-red-300" : "text-muted-foreground"}>[{log.level || "info"}]</span>
                              <span className="text-foreground">{log.message}</span>
                            </div>
                          ))}
                        </div>
                      ) : (
                        <div className="rounded bg-card p-2 text-xs text-muted-foreground">No logs</div>
                      )}
                    </div>
                  </div>
                )}
              </div>
            );
          })}
          {hasMore && (
            <Button variant="outline" size="sm" className="w-full" onClick={() => void load(offset, true)}>
              Load more ({total - runs.length} remaining)
            </Button>
          )}
        </div>
      )}
    </section>
  );
}
