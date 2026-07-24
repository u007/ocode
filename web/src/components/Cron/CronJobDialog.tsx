import { useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import type { CronJob, CronJobWriteRequest, CronPermissionMode, CronScheduleKind } from "@/api/types";
import { datetimeLocalFromMs, msFromDatetimeLocal } from "./cronFormat";

interface Props {
  open: boolean;
  job: CronJob | null;
  onOpenChange: (open: boolean) => void;
  onSave: (job: CronJobWriteRequest) => Promise<void>;
}

type FormState = {
  name: string;
  message: string;
  notes: string;
  owner: string;
  deliverTo: string;
  permMode: CronPermissionMode;
  scheduleKind: CronScheduleKind;
  atValue: string;
  everyMs: string;
  expr: string;
  tz: string;
};

function defaultState(job: CronJob | null): FormState {
  if (!job) {
    return {
      name: "",
      message: "",
      notes: "",
      owner: "",
      deliverTo: "",
      permMode: "normal",
      scheduleKind: "every",
      atValue: datetimeLocalFromMs(Date.now() + 60 * 60 * 1000),
      everyMs: String(60_000),
      expr: "",
      tz: "",
    };
  }
  return {
    name: job.name ?? "",
    message: job.payload.message ?? "",
    notes: job.payload.notes ?? "",
    owner: job.payload.owner ?? "",
    deliverTo: job.payload.deliver_to ?? "",
    permMode: job.payload.perm_mode ?? "normal",
    scheduleKind: job.schedule.kind,
    atValue: datetimeLocalFromMs(job.schedule.at_ms),
    everyMs: String(job.schedule.every_ms ?? 60_000),
    expr: job.schedule.expr ?? "",
    tz: job.schedule.tz ?? "",
  };
}

function buildSchedule(state: FormState) {
  switch (state.scheduleKind) {
    case "at": {
      const atMs = msFromDatetimeLocal(state.atValue);
      if (!atMs) throw new Error("At time is required");
      return { kind: "at" as const, at_ms: atMs };
    }
    case "every": {
      const everyMs = Number(state.everyMs);
      if (!Number.isFinite(everyMs) || everyMs <= 0) throw new Error("Interval must be greater than zero");
      return { kind: "every" as const, every_ms: Math.floor(everyMs) };
    }
    case "cron": {
      if (!state.expr.trim()) throw new Error("Cron expression is required");
      return {
        kind: "cron" as const,
        expr: state.expr.trim(),
        tz: state.tz.trim(),
      };
    }
  }
}

export default function CronJobDialog({ open, job, onOpenChange, onSave }: Props) {
  const [state, setState] = useState<FormState>(() => defaultState(job));
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (open) {
      setState(defaultState(job));
      setError(null);
      setSaving(false);
    }
  }, [open, job]);

  const preview = useMemo(() => {
    try {
      return JSON.stringify(
        {
          name: state.name.trim() || state.message.trim(),
          message: state.message.trim(),
          notes: state.notes.trim() || undefined,
          owner: state.owner.trim() || undefined,
          deliver_to: state.deliverTo.trim() || undefined,
          perm_mode: state.permMode,
          schedule: buildSchedule(state),
        },
        null,
        2,
      );
    } catch (err) {
      return String(err instanceof Error ? err.message : err);
    }
  }, [state]);

  const submit = async () => {
    setError(null);
    const message = state.message.trim();
    if (!message) {
      setError("Message is required");
      return;
    }
    let schedule;
    try {
      schedule = buildSchedule(state);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Invalid schedule");
      return;
    }

    setSaving(true);
    try {
      await onSave({
        name: state.name.trim() || message,
        message,
        notes: state.notes.trim() || undefined,
        owner: state.owner.trim() || undefined,
        deliver_to: state.deliverTo.trim() || undefined,
        perm_mode: state.permMode,
        schedule,
      });
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save job");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl max-h-[90vh] overflow-y-auto bg-zinc-900 border-zinc-700 text-zinc-100">
        <DialogHeader>
          <DialogTitle>{job ? "Edit cron job" : "Add cron job"}</DialogTitle>
          <DialogDescription className="text-zinc-400">
            Schedule a prompt to run later, repeat on an interval, or execute on a cron expression.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4 md:grid-cols-2">
          <label className="grid gap-2 text-sm">
            <span className="text-zinc-400">Name</span>
            <Input
              value={state.name}
              onChange={(e) => setState((prev) => ({ ...prev, name: e.target.value }))}
              placeholder="Optional label"
            />
          </label>
          <label className="grid gap-2 text-sm">
            <span className="text-zinc-400">Permission mode</span>
            <select
              value={state.permMode}
              onChange={(e) =>
                setState((prev) => ({ ...prev, permMode: e.target.value as CronPermissionMode }))
              }
              className="h-10 rounded-md border border-zinc-700 bg-zinc-950 px-3 text-sm text-zinc-100"
            >
              <option value="normal">normal</option>
              <option value="yolo">yolo</option>
              <option value="locked">locked</option>
            </select>
          </label>
          <label className="grid gap-2 text-sm md:col-span-2">
            <span className="text-zinc-400">Message</span>
            <textarea
              value={state.message}
              onChange={(e) => setState((prev) => ({ ...prev, message: e.target.value }))}
              rows={5}
              className="min-h-28 rounded-md border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-500"
              placeholder="The prompt to run"
            />
          </label>
          <label className="grid gap-2 text-sm md:col-span-2">
            <span className="text-zinc-400">Notes</span>
            <textarea
              value={state.notes}
              onChange={(e) => setState((prev) => ({ ...prev, notes: e.target.value }))}
              rows={3}
              className="rounded-md border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-500"
              placeholder="Optional description"
            />
          </label>
          <label className="grid gap-2 text-sm">
            <span className="text-zinc-400">Owner / workdir</span>
            <Input
              value={state.owner}
              onChange={(e) => setState((prev) => ({ ...prev, owner: e.target.value }))}
              placeholder="/path/to/project"
            />
          </label>
          <label className="grid gap-2 text-sm">
            <span className="text-zinc-400">Deliver to</span>
            <Input
              value={state.deliverTo}
              onChange={(e) => setState((prev) => ({ ...prev, deliverTo: e.target.value }))}
              placeholder="telegram"
            />
          </label>
        </div>

        <div className="grid gap-4 rounded-lg border border-zinc-700 bg-zinc-950/60 p-4">
          <div className="flex items-center justify-between gap-4">
            <div>
              <div className="text-sm font-medium text-zinc-100">Schedule</div>
              <div className="text-xs text-zinc-500">Choose when the job should fire.</div>
            </div>
            <select
              value={state.scheduleKind}
              onChange={(e) => setState((prev) => ({ ...prev, scheduleKind: e.target.value as CronScheduleKind }))}
              className="h-10 rounded-md border border-zinc-700 bg-zinc-950 px-3 text-sm text-zinc-100"
            >
              <option value="at">once</option>
              <option value="every">every</option>
              <option value="cron">cron</option>
            </select>
          </div>

          {state.scheduleKind === "at" && (
            <label className="grid gap-2 text-sm">
              <span className="text-zinc-400">Run at</span>
              <Input
                type="datetime-local"
                value={state.atValue}
                onChange={(e) => setState((prev) => ({ ...prev, atValue: e.target.value }))}
              />
            </label>
          )}

          {state.scheduleKind === "every" && (
            <label className="grid gap-2 text-sm">
              <span className="text-zinc-400">Interval (ms)</span>
              <Input
                type="number"
                min={1}
                step={1}
                value={state.everyMs}
                onChange={(e) => setState((prev) => ({ ...prev, everyMs: e.target.value }))}
              />
            </label>
          )}

          {state.scheduleKind === "cron" && (
            <div className="grid gap-4 md:grid-cols-2">
              <label className="grid gap-2 text-sm md:col-span-2">
                <span className="text-zinc-400">Cron expression</span>
                <Input
                  value={state.expr}
                  onChange={(e) => setState((prev) => ({ ...prev, expr: e.target.value }))}
                  placeholder="0 9 * * *"
                />
              </label>
              <label className="grid gap-2 text-sm md:col-span-2">
                <span className="text-zinc-400">Timezone</span>
                <Input
                  value={state.tz}
                  onChange={(e) => setState((prev) => ({ ...prev, tz: e.target.value }))}
                  placeholder="UTC"
                />
              </label>
            </div>
          )}
        </div>

        <details className="rounded-lg border border-zinc-700 bg-zinc-950/60 p-4">
          <summary className="cursor-pointer text-sm font-medium text-zinc-200">Raw JSON preview</summary>
          <pre className="mt-3 overflow-auto text-xs text-zinc-300">{preview}</pre>
        </details>

        {error && <div className="rounded-md border border-red-900 bg-red-950/60 px-3 py-2 text-sm text-red-200">{error}</div>}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={saving}>
            {saving ? "Saving…" : job ? "Save changes" : "Add job"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
