import { useCallback, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import type { CronDelivery, CronJob, CronJobWriteRequest } from "@/api/types";
import { api } from "@/api/client";
import { describeSchedule, lastRunLabel, nextRunLabel } from "./cronFormat";
import CronJobDialog from "./CronJobDialog";
import CronOutboxPanel from "./CronOutboxPanel";
import CronTargetsPanel from "./CronTargetsPanel";
import { CalendarClock, PencilLine, Pause, Play, Plus, RefreshCcw, Trash2 } from "lucide-react";

const REFRESH_INTERVAL = 10_000;

export default function CronPanel() {
  const [jobs, setJobs] = useState<CronJob[]>([]);
  const [outbox, setOutbox] = useState<CronDelivery[]>([]);
  const [targets, setTargets] = useState<Record<string, number>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingJob, setEditingJob] = useState<CronJob | null>(null);

  const loadJobs = useCallback(async () => {
    const [jobsRes, outboxRes] = await Promise.all([api.listCronJobs(), api.getCronOutbox()]);
    setJobs(jobsRes.jobs);
    setOutbox(outboxRes.entries);
  }, []);

  const loadTargets = useCallback(async () => {
    const res = await api.getCronTargets();
    setTargets(res.targets);
  }, []);

  const refreshJobs = useCallback(async () => {
    try {
      await loadJobs();
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load cron jobs");
    }
  }, [loadJobs]);

  const refreshTargets = useCallback(async () => {
    try {
      await loadTargets();
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load cron targets");
    }
  }, [loadTargets]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        await Promise.all([loadJobs(), loadTargets()]);
        if (!cancelled) setError(null);
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Failed to load cron data");
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [loadJobs, loadTargets]);

  useEffect(() => {
    const interval = window.setInterval(() => {
      void refreshJobs();
    }, REFRESH_INTERVAL);
    return () => window.clearInterval(interval);
  }, [refreshJobs]);

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
      await refreshJobs();
    },
    [editingJob, refreshJobs],
  );

  const toggleEnabled = async (job: CronJob) => {
    try {
      await api.updateCronJob(job.id, { enabled: !job.enabled });
      await refreshJobs();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to toggle job");
    }
  };

  const deleteJob = async (job: CronJob) => {
    if (!window.confirm(`Delete cron job \"${job.name}\"?`)) return;
    try {
      await api.deleteCronJob(job.id);
      await refreshJobs();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to delete job");
    }
  };

  const clearOutbox = async () => {
    try {
      await api.drainCronOutbox();
      await refreshJobs();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to clear outbox");
    }
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
    await refreshTargets();
  };

  return (
    <div className="flex h-full flex-col overflow-hidden bg-zinc-950 text-zinc-100">
      <div className="flex items-center justify-between gap-4 border-b border-zinc-800 px-4 py-3">
        <div>
          <div className="flex items-center gap-2 text-sm font-semibold">
            <CalendarClock className="h-4 w-4 text-blue-400" />
            Cron
          </div>
          <div className="text-xs text-zinc-500">Schedule jobs, manage delivery history, and map Telegram targets.</div>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => void refreshJobs()}>
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

          <div className="overflow-hidden rounded-lg border border-zinc-700 bg-zinc-900/80">
            <div className="overflow-auto">
              <table className="min-w-full border-collapse text-sm">
                <thead className="sticky top-0 bg-zinc-900 text-xs uppercase tracking-wide text-zinc-500">
                  <tr>
                    <th className="px-4 py-3 text-left font-medium">Name</th>
                    <th className="px-4 py-3 text-left font-medium">Schedule</th>
                    <th className="px-4 py-3 text-left font-medium">Next Run</th>
                    <th className="px-4 py-3 text-left font-medium">Last Run</th>
                    <th className="px-4 py-3 text-left font-medium">Last Status</th>
                    <th className="px-4 py-3 text-left font-medium">Runs</th>
                    <th className="px-4 py-3 text-left font-medium">Enabled</th>
                    <th className="px-4 py-3 text-right font-medium">Delete</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-zinc-800">
                  {loading ? (
                    <tr>
                      <td colSpan={8} className="px-4 py-8 text-center text-zinc-500">
                        Loading cron jobs…
                      </td>
                    </tr>
                  ) : jobs.length === 0 ? (
                    <tr>
                      <td colSpan={8} className="px-4 py-8 text-center text-zinc-500">
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
                            : "text-zinc-400";
                      return (
                        <tr
                          key={job.id}
                          className="cursor-pointer bg-zinc-950/40 hover:bg-zinc-800/70"
                          onClick={() => openEditDialog(job)}
                        >
                          <td className="px-4 py-3 align-top">
                            <div className="font-medium text-zinc-100">{job.name || job.payload.message}</div>
                            <div className="mt-1 text-xs text-zinc-500">{job.payload.message}</div>
                          </td>
                          <td className="px-4 py-3 align-top text-zinc-300">{describeSchedule(job.schedule)}</td>
                          <td className="px-4 py-3 align-top text-zinc-300">{nextRunLabel(job.state.next_run_at_ms)}</td>
                          <td className="px-4 py-3 align-top text-zinc-300">{lastRunLabel(job.state.last_run_at_ms)}</td>
                          <td className={`px-4 py-3 align-top ${lastStatusClass}`} title={job.state.last_error || undefined}>
                            {status}
                          </td>
                          <td className="px-4 py-3 align-top text-zinc-300">{job.state.runs ?? 0}</td>
                          <td className="px-4 py-3 align-top">
                            <button
                              type="button"
                              className={`inline-flex items-center gap-1 rounded-full border px-2.5 py-1 text-xs transition-colors ${
                                job.enabled
                                  ? "border-emerald-700 bg-emerald-950/70 text-emerald-300 hover:bg-emerald-900"
                                  : "border-zinc-700 bg-zinc-900 text-zinc-400 hover:bg-zinc-800"
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
                                className="h-8 px-2 text-zinc-400 hover:text-zinc-100"
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
                                className="h-8 px-2 text-zinc-400 hover:text-red-300"
                                onClick={(e) => {
                                  e.stopPropagation();
                                  void deleteJob(job);
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

          <div className="grid gap-4 lg:grid-cols-2">
            <CronOutboxPanel entries={outbox} onClear={clearOutbox} />
            <CronTargetsPanel targets={targets} onSave={saveTargets} />
          </div>
        </div>
      </div>

      <CronJobDialog open={dialogOpen} job={editingJob} onOpenChange={setDialogOpen} onSave={submitJob} />
    </div>
  );
}
