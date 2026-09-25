import { useCallback, useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import ConfirmDialog from "@/components/common/ConfirmDialog";
import type { CronDelivery, CronJob, CronJobWriteRequest } from "@/api/types";
import { api } from "@/api/client";
import { describeSchedule, lastRunLabel, nextRunLabel } from "./cronFormat";
import CronJobDialog from "./CronJobDialog";
import CronOutboxPanel from "./CronOutboxPanel";
import CronTargetsPanel from "./CronTargetsPanel";
import CronHistoryPanel from "./CronHistoryPanel";
import { CalendarClock, PencilLine, Pause, Play, Plus, RefreshCcw, Trash2, History } from "lucide-react";
import {
  useKeyedLoad,
  type KeyedLoadResult,
  type LoadingEventHandler,
} from "@/hooks/useKeyedLoad";

const REFRESH_INTERVAL = 10_000;

interface Props {
  active?: boolean;
  loadingKey?: string;
  onLoadingEvent?: LoadingEventHandler;
}

type CronLoadData = {
  jobs: CronJob[];
  outbox: CronDelivery[];
  targets: Record<string, number>;
};

export default function CronPanel({ active = true, loadingKey, onLoadingEvent }: Props) {
  const runKeyedLoad = useKeyedLoad(loadingKey, onLoadingEvent);
  const [jobs, setJobs] = useState<CronJob[]>([]);
  const [outbox, setOutbox] = useState<CronDelivery[]>([]);
  const [targets, setTargets] = useState<Record<string, number>>({});
  const [loading, setLoading] = useState(true);
  const [initialReady, setInitialReady] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingJob, setEditingJob] = useState<CronJob | null>(null);
  const [historyJob, setHistoryJob] = useState<CronJob | null>(null);
  // Job awaiting delete confirmation. Native `window.confirm` was the old
  // guard and it silently returns false in the Wails/WKWebView desktop webview,
  // which made deleting a job impossible there.
  const [pendingDelete, setPendingDelete] = useState<CronJob | null>(null);
  const [pendingOutboxClear, setPendingOutboxClear] = useState(false);
  const initialReadyRef = useRef(false);
  const refreshAllRef = useRef<(() => Promise<KeyedLoadResult<CronLoadData>>) | null>(null);

  const loadAll = useCallback(async (): Promise<CronLoadData> => {
    const [jobsRes, outboxRes, targetsRes] = await Promise.all([
      api.listCronJobs(),
      api.getCronOutbox(),
      api.getCronTargets(),
    ]);
    return {
      jobs: jobsRes.jobs ?? [],
      outbox: outboxRes.entries ?? [],
      targets: targetsRes.targets,
    };
  }, []);

  // Every path — initial load, manual refresh, mutations, and polling — uses
  // this one latest-wins request. A poll therefore cannot leave a half-loaded
  // jobs/targets snapshot, and the hook remains the single concurrency guard.
  const refreshAll = useCallback((): Promise<KeyedLoadResult<CronLoadData>> => {
    const wasReady = initialReadyRef.current;
    if (!wasReady) {
      setLoading(true);
      setInitialReady(false);
    }
    return runKeyedLoad(loadAll, {
      empty: (data) =>
        data.jobs.length === 0 && data.outbox.length === 0 && Object.keys(data.targets).length === 0,
      retry: () => {
        void refreshAllRef.current?.();
      },
    }).then((result) => {
      if (result.status === "success" || result.status === "empty") {
        setJobs(result.value.jobs);
        setOutbox(result.value.outbox);
        setTargets(result.value.targets);
        setError(null);
        setLoading(false);
        initialReadyRef.current = true;
        setInitialReady(true);
      } else if (result.status === "error") {
        console.error("Cron data load failed:", result.error);
        setError(result.message);
        if (!wasReady) {
          setLoading(false);
          initialReadyRef.current = false;
          setInitialReady(false);
        }
      }
      return result;
    });
  }, [loadAll, runKeyedLoad]);
  refreshAllRef.current = refreshAll;

  useEffect(() => {
    initialReadyRef.current = false;
    setInitialReady(false);
    setLoading(true);
  }, [loadingKey]);

  useEffect(() => {
    void refreshAll();
  }, [loadingKey, refreshAll]);

  useEffect(() => {
    // Poll only while the Cron view is frontmost — the panel is force-mounted
    // so its DOM survives view switches, and background polling was pure churn.
    if (!active || !initialReady) return;
    const interval = window.setInterval(() => {
      void refreshAll();
    }, REFRESH_INTERVAL);
    return () => window.clearInterval(interval);
  }, [refreshAll, active, initialReady]);

  const openAddDialog = () => {
    setEditingJob(null);
    setDialogOpen(true);
  };

  const openEditDialog = (job: CronJob) => {
    setEditingJob(job);
    setDialogOpen(true);
  };

  const submitJob = useCallback(
    async (request: CronJobWriteRequest) => {
      if (editingJob) {
        await api.updateCronJob(editingJob.id, request);
      } else {
        await api.addCronJob(request);
      }
      await refreshAll();
    },
    [editingJob, refreshAll],
  );

  const toggleEnabled = async (job: CronJob) => {
    try {
      await api.updateCronJob(job.id, { enabled: !job.enabled });
      await refreshAll();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to toggle job");
    }
  };

  // Runs only from the delete confirm. Rejections propagate to ConfirmDialog,
  // which renders the reason inline and keeps the confirm open — closing it
  // would make a failed delete look like a completed one.
  const deleteJob = async (job: CronJob) => {
    await api.deleteCronJob(job.id);
    await refreshAll();
  };

  // Opens the clear-outbox confirm; the drain itself happens in the dialog's
  // onConfirm so a failure can be reported without an app-wide banner.
  const clearOutbox = async () => {
    setPendingOutboxClear(true);
  };

  // Runs only from the clear confirm. Rejections propagate to ConfirmDialog,
  // which shows the reason inline and keeps the confirm open — a swallowed
  // failure would empty the list on screen while the entries are still queued.
  const drainOutbox = async () => {
    await api.drainCronOutbox();
    await refreshAll();
  };

  const saveTargets = async (nextTargets: Record<string, number>) => {
    const current = targets;
    const ops: Promise<unknown>[] = [];
    for (const [workdir, chatId] of Object.entries(nextTargets)) {
      if (current[workdir] !== chatId) {
        ops.push(api.setCronTarget(workdir, chatId));
      }
    }
    for (const workdir of Object.keys(current)) {
      if (!(workdir in nextTargets)) {
        ops.push(api.setCronTarget(workdir, 0));
      }
    }
    await Promise.all(ops);
    await refreshAll();
  };

  return (
    <div className="flex h-full flex-col overflow-hidden bg-background text-foreground">
      <div className="flex items-center justify-between gap-4 border-b border-border px-4 py-3">
        <div>
          <div className="flex items-center gap-2 text-sm font-semibold">
            <CalendarClock className="h-4 w-4 text-blue-400" />
            Cron
          </div>
          <div className="text-xs text-muted-foreground">Schedule jobs, manage delivery history, and map Telegram targets.</div>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => void refreshAll()}>
            <RefreshCcw className="mr-2 h-4 w-4" />
            Refresh
          </Button>
          <Button size="sm" onClick={openAddDialog}>
            <Plus className="mr-2 h-4 w-4" />
            Add job
          </Button>
        </div>
      </div>

      <div className="flex-1 overflow-hidden p-4">
        <div className="flex h-full flex-col gap-4 overflow-hidden">
          {error && (
            <div className="rounded-md border border-red-900 bg-red-950/60 px-3 py-2 text-sm text-red-200">
              {error}
            </div>
          )}

          <div className="overflow-hidden rounded-lg border border-border bg-card/80">
            <div className="overflow-auto">
              <table className="min-w-full border-collapse text-sm">
                <thead className="sticky top-0 bg-card text-xs uppercase tracking-wide text-muted-foreground">
                  <tr>
                    <th className="px-4 py-3 text-left font-medium">Name</th>
                    <th className="px-4 py-3 text-left font-medium">Schedule</th>
                    <th className="px-4 py-3 text-left font-medium">Next Run</th>
                    <th className="px-4 py-3 text-left font-medium">Last Run</th>
                    <th className="px-4 py-3 text-left font-medium">Last Status</th>
                    <th className="px-4 py-3 text-left font-medium">Runs</th>
                    <th className="px-4 py-3 text-left font-medium">Enabled</th>
                    <th className="px-4 py-3 text-right font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {loading ? (
                    <tr>
                      <td colSpan={9} className="px-4 py-8 text-center text-muted-foreground">
                        Loading cron jobs…
                      </td>
                    </tr>
                  ) : jobs.length === 0 ? (
                    <tr>
                      <td colSpan={9} className="px-4 py-8 text-center text-muted-foreground">
                        No cron jobs yet.
                      </td>
                    </tr>
                  ) : (
                    jobs.map((job) => {
                      const status = job.state.last_status || "—";
                      const lastStatusClass =
                        status === "error"
                          ? "text-red-300"
                          : status === "ok"
                            ? "text-emerald-300"
                            : "text-muted-foreground";
                      return (
                        <tr
                          key={job.id}
                          className="cursor-pointer bg-background/40 hover:bg-muted/70"
                          onClick={() => openEditDialog(job)}
                        >
                          <td className="px-4 py-3 align-top">
                            <div className="font-medium text-foreground">{job.name || job.payload.message}</div>
                            <div className="mt-1 text-xs text-muted-foreground">{job.payload.message}</div>
                          </td>
                          <td className="px-4 py-3 align-top text-foreground">{describeSchedule(job.schedule)}</td>
                          <td className="px-4 py-3 align-top text-foreground">{nextRunLabel(job.state.next_run_at_ms)}</td>
                          <td className="px-4 py-3 align-top text-foreground">{lastRunLabel(job.state.last_run_at_ms)}</td>
                          <td className={`px-4 py-3 align-top ${lastStatusClass}`} title={job.state.last_error || undefined}>
                            {status}
                          </td>
                          <td className="px-4 py-3 align-top text-foreground">{job.state.runs ?? 0}</td>
                          <td className="px-4 py-3 align-top">
                            <button
                              type="button"
                              className={`inline-flex items-center gap-1 rounded-full border px-2.5 py-1 text-xs transition-colors ${
                                job.enabled
                                  ? "border-emerald-700 bg-emerald-950/70 text-emerald-300 hover:bg-emerald-900"
                                  : "border-border bg-card text-muted-foreground hover:bg-muted"
                              }`}
                              onClick={(e) => {
                                e.stopPropagation();
                                void toggleEnabled(job);
                              }}
                            >
                              {job.enabled ? <Play className="h-3.5 w-3.5" /> : <Pause className="h-3.5 w-3.5" />}
                              {job.enabled ? "On" : "Off"}
                            </button>
                          </td>
                          <td className="px-4 py-3 align-top text-right">
                            <div className="flex items-center justify-end gap-2">
                              <Button
                                variant="ghost"
                                size="sm"
                                className="h-8 px-2 text-muted-foreground hover:text-blue-300"
                                title="View run history"
                                onClick={(e) => {
                                  e.stopPropagation();
                                  setHistoryJob(job);
                                }}
                              >
                                <History className="h-4 w-4" />
                              </Button>
                              <Button
                                variant="ghost"
                                size="sm"
                                className="h-8 px-2 text-muted-foreground hover:text-foreground"
                                onClick={(e) => {
                                  e.stopPropagation();
                                  openEditDialog(job);
                                }}
                              >
                                <PencilLine className="h-4 w-4" />
                              </Button>
                              <Button
                                variant="ghost"
                                size="sm"
                                className="h-8 px-2 text-muted-foreground hover:text-red-300"
                                title={`Delete ${job.name || "this job"}`}
                                aria-label={`Delete ${job.name || "this job"}`}
                                onClick={(e) => {
                                  e.stopPropagation();
                                  setPendingDelete(job);
                                }}
                              >
                                <Trash2 className="h-4 w-4" />
                              </Button>
                            </div>
                          </td>
                        </tr>
                      );
                    })
                  )}
                </tbody>
              </table>
            </div>
          </div>

          {historyJob && (
            <CronHistoryPanel
              jobId={historyJob.id}
              jobName={historyJob.name || historyJob.payload.message}
              onClose={() => setHistoryJob(null)}
            />
          )}

          <div className="grid gap-4 lg:grid-cols-2">
            <CronOutboxPanel entries={outbox} onClear={clearOutbox} />
            <CronTargetsPanel targets={targets} onSave={saveTargets} />
          </div>
        </div>
      </div>

      <CronJobDialog open={dialogOpen} job={editingJob} onOpenChange={setDialogOpen} onSave={submitJob} />

      <ConfirmDialog
        open={pendingDelete !== null}
        title="Delete cron job?"
        description={
          pendingDelete && (
            <>
              <span className="font-medium text-foreground break-all">
                {pendingDelete.name || pendingDelete.payload.message}
              </span>{" "}
              will be deleted and will stop running on its schedule. Run history is kept on disk,
              but the job is no longer listed.
            </>
          )
        }
        confirmLabel="Delete"
        pendingLabel="Deleting…"
        onConfirm={async () => {
          if (!pendingDelete) return;
          await deleteJob(pendingDelete);
          setPendingDelete(null);
        }}
        onCancel={() => setPendingDelete(null)}
      />

      <ConfirmDialog
        open={pendingOutboxClear}
        title="Clear pending cron results?"
        // Verified against Outbox.Drain (internal/scheduler/deliver.go): it
        // truncates the JSONL file, and the handler drains only after peeking
        // (internal/server/scheduler.go), so a cleared entry is gone for good
        // and is never delivered. Say exactly that.
        description={
          <>
            {outbox.length} {outbox.length === 1 ? "result" : "results"} waiting to be delivered
            will be dropped. Clearing the outbox deletes them without being delivered — a cron
            result that has not been read yet is lost.
          </>
        }
        confirmLabel="Clear outbox"
        pendingLabel="Clearing…"
        onConfirm={async () => {
          await drainOutbox();
          setPendingOutboxClear(false);
        }}
        onCancel={() => setPendingOutboxClear(false)}
      />
    </div>
  );
}
