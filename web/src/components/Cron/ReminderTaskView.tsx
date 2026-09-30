import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import ConfirmDialog from "@/components/common/ConfirmDialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  REMINDER_STATUS_LABELS,
  nextReminderStatuses,
  type ReminderItem,
  type ReminderItemKind,
  type ReminderItemListParams,
  type ReminderItemStatus,
  type ReminderItemWriteRequest,
} from "@/api/types";
import { api } from "@/api/client";
import { cn } from "@/lib/utils";
import { describeDueLabel } from "./reminderFormat";
import CronHistoryPanel from "./CronHistoryPanel";
import ReminderTaskDialog from "./ReminderTaskDialog";
import { History, PencilLine, Plus, RefreshCcw, Trash2, Zap } from "lucide-react";

const REFRESH_INTERVAL = 10_000;
const PAGE_SIZE = 50;
const ALL_STATUSES = "all" as const;
type StatusFilter = ReminderItemStatus | typeof ALL_STATUSES;

interface Props {
  /** The collection this view reads and writes. */
  kind: ReminderItemKind;
  /**
   * The DOM id of the tabpanel, which the switcher's `aria-controls` points at.
   * It is passed in rather than derived from `kind` because the switcher names
   * views in the PLURAL ("reminders"/"tasks") while the API and the Kind type are
   * singular ("reminder"/"task"). Deriving it here produced aria-controls
   * pointing at ids that did not exist, which is an accessibility bug and not
   * just a cosmetic mismatch.
   */
  panelId: string;
  /** False while another sub-view is in front, so polling stops when hidden. */
  active: boolean;
  /** Changes when the project changes; resets the list and the filter. */
  loadingKey?: string;
  /** Called with the total so the parent can badge the sub-tab. */
  onTotalChange?: (kind: ReminderItemKind, total: number) => void;
}

const STATUS_STYLES: Record<ReminderItemStatus, string> = {
  pending: "border-amber-700 bg-amber-950/60 text-amber-300",
  in_progress: "border-blue-700 bg-blue-950/60 text-blue-300",
  completed: "border-emerald-700 bg-emerald-950/60 text-emerald-300",
  cancelled: "border-border bg-card text-muted-foreground",
};

/**
 * The list view for BOTH reminders and tasks. They differ only in which
 * collection they address and in copy, and duplicating the table would be a
 * second copy of the status menu, the confirm-to-delete rule, and the polling
 * guard — the three things most likely to drift apart.
 */
export default function ReminderTaskView({
  kind,
  panelId,
  active,
  loadingKey,
  onTotalChange,
}: Props) {
  const [items, setItems] = useState<ReminderItem[]>([]);
  const [total, setTotal] = useState(0);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>(ALL_STATUSES);
  const [loading, setLoading] = useState(true);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<ReminderItem | null>(null);
  const [pendingDelete, setPendingDelete] = useState<ReminderItem | null>(null);
  const [historyItem, setHistoryItem] = useState<ReminderItem | null>(null);

  const readyRef = useRef(false);
  const inFlight = useRef<Promise<void> | null>(null);
  const filterRef = useRef<StatusFilter>(statusFilter);
  filterRef.current = statusFilter;

  /**
   * Lazy activation. The parent mounts all three panes so their filters,
   * dialogs and scroll positions survive a switch, but a pane that has never
   * been shown must NOT fetch: opening the Cron tab should cost three
   * /api/cron calls, not five, and a hidden list has nothing to display. Once
   * activated it stays activated, so returning to it does not re-create its
   * state from scratch.
   */
  const [activated, setActivated] = useState(active);
  useEffect(() => {
    if (active) setActivated(true);
  }, [active]);

  const isReminder = kind === "reminder";
  const noun = isReminder ? "reminder" : "task";
  const plural = isReminder ? "reminders" : "tasks";
  // The switcher's tab id is the plural view name, the same spelling as
  // `panelId`; both are handed to us rather than re-derived here.
  const ariaLabelledBy = panelId.replace("cron-subpanel-", "cron-subtab-");

  const buildParams = useCallback((): ReminderItemListParams => {
    const filter = filterRef.current;
    return {
      status: filter === ALL_STATUSES ? undefined : filter,
      limit: PAGE_SIZE,
      offset: 0,
    };
  }, []);

  /**
   * Latest-wins load. Overlapping polls are collapsed onto the in-flight
   * request so a slow response can never overwrite a newer one — the same rule
   * the Jobs view follows through `useKeyedLoad`.
   */
  const refresh = useCallback((): Promise<void> => {
    if (inFlight.current) return inFlight.current;
    const params = buildParams();
    const run = api
      .listReminderItems(kind, params)
      .then((res) => {
        setItems(res.items ?? []);
        setTotal(res.total ?? 0);
        onTotalChange?.(kind, res.total ?? 0);
        setError(null);
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : `Failed to load ${plural}`);
      })
      .finally(() => {
        inFlight.current = null;
        setLoading(false);
        readyRef.current = true;
        setReady(true);
      });
    inFlight.current = run;
    return run;
  }, [kind, plural, buildParams, onTotalChange]);

  /**
   * Everything that must trigger a re-read, as explicit inputs to ONE effect.
   *
   * The status filter is in this list on purpose and not by accident: leaving it
   * out made the filter select update local state and then quietly do nothing,
   * because the load effect did not depend on it. Splitting "project changed"
   * from "filter changed" into two effects is what let that happen — the first
   * one reset the filter, and neither effect re-read on the reset value.
   */
  const [reloadToken, setReloadToken] = useState(0);

  // A new project (or a new loadingKey) resets the filter and forces a reload.
  // A counter rather than a boolean, because resetting the filter to "all" when
  // it is ALREADY "all" produces no state change, so a plain dependency would
  // not fire at all.
  useEffect(() => {
    readyRef.current = false;
    setReady(false);
    setLoading(true);
    setStatusFilter(ALL_STATUSES);
    setReloadToken((n) => n + 1);
  }, [loadingKey]);

  // Load once activated, and re-load whenever the filter changes.
  useEffect(() => {
    if (!activated) return;
    void refresh();
  }, [activated, statusFilter, reloadToken, refresh]);

  // Poll only while this view is the frontmost sub-view. The parent force-mounts
  // all three, so an ungated poll is pure churn in the background.
  useEffect(() => {
    if (!active || !ready) return;
    const id = window.setInterval(() => {
      void refresh();
    }, REFRESH_INTERVAL);
    return () => window.clearInterval(id);
  }, [active, ready, refresh]);

  const submit = useCallback(
    async (request: ReminderItemWriteRequest) => {
      if (editing) {
        // A status-only change is not expressible through this dialog, and the
        // status is deliberately NOT sent here: saving an item's fields must
        // never silently move it back to pending. The two operations are
        // separate on the server too (Update vs SetStatus).
        await api.updateReminderItem(kind, editing.id, request);
      } else {
        await api.addReminderItem(kind, request);
      }
      await refresh();
    },
    [editing, kind, refresh],
  );

  /**
   * Change one item's status. An illegal move is answered with a 409, which is
   * reported inline rather than swallowed — the button was offered from the
   * same table the server enforces, so a 409 means the two drifted and the user
   * needs to know the list is stale.
   */
  const setStatus = useCallback(
    async (item: ReminderItem, status: ReminderItemStatus) => {
      setBusyId(item.id);
      try {
        await api.updateReminderItem(kind, item.id, { status });
        await refresh();
      } catch (err) {
        setError(err instanceof Error ? err.message : `Failed to set ${noun} status`);
      } finally {
        setBusyId(null);
      }
    },
    [kind, noun, refresh],
  );

  const runNow = useCallback(
    async (item: ReminderItem) => {
      setBusyId(item.id);
      try {
        await api.runReminderItem(kind, item.id);
        await refresh();
      } catch (err) {
        setError(err instanceof Error ? err.message : `Failed to run ${noun}`);
      } finally {
        setBusyId(null);
      }
    },
    [kind, noun, refresh],
  );

  const deleteItem = useCallback(
    async (item: ReminderItem) => {
      await api.deleteReminderItem(kind, item.id);
      await refresh();
    },
    [kind, refresh],
  );

  const rows = useMemo(() => items, [items]);
  const showPager = total > PAGE_SIZE;

  return (
    <section
      id={panelId}
      role="tabpanel"
      aria-labelledby={ariaLabelledBy}
      data-testid={panelId}
      className="flex min-h-0 flex-col gap-3"
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="text-xs text-muted-foreground">
          {isReminder
            ? "One-shot nudges. Each rings once at its due time, then completes."
            : "Checklist items. A due date and an agent run are both optional."}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Select
            value={statusFilter}
            onValueChange={(v) => setStatusFilter(v as StatusFilter)}
          >
            <SelectTrigger className="h-8 w-[9.5rem] text-xs" data-testid={`${kind}-status-filter`}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL_STATUSES}>All statuses</SelectItem>
              {(Object.keys(REMINDER_STATUS_LABELS) as ReminderItemStatus[]).map((s) => (
                <SelectItem key={s} value={s}>
                  {REMINDER_STATUS_LABELS[s]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button variant="outline" size="sm" onClick={() => void refresh()}>
            <RefreshCcw className="mr-2 h-3.5 w-3.5" />
            Refresh
          </Button>
          <Button
            size="sm"
            onClick={() => {
              setEditing(null);
              setDialogOpen(true);
            }}
          >
            <Plus className="mr-2 h-3.5 w-3.5" />
            Add {noun}
          </Button>
        </div>
      </div>

      {error && (
        <div
          role="alert"
          className="rounded-md border border-red-900 bg-red-950/60 px-3 py-2 text-sm text-red-200"
        >
          {error}
        </div>
      )}

      <div className="overflow-hidden rounded-lg border border-border bg-card/80">
        <div className="overflow-auto">
          <table className="min-w-full border-collapse text-sm">
            <thead className="sticky top-0 bg-card text-xs uppercase tracking-wide text-muted-foreground">
              <tr>
                <th className="px-4 py-3 text-left font-medium">{isReminder ? "Reminder" : "Task"}</th>
                <th className="px-4 py-3 text-left font-medium">Status</th>
                <th className="px-4 py-3 text-left font-medium">Due</th>
                <th className="px-4 py-3 text-left font-medium">Fires</th>
                <th className="px-4 py-3 text-left font-medium">Last run</th>
                <th className="px-4 py-3 text-right font-medium">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {loading && rows.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-8 text-center text-muted-foreground">
                    Loading {plural}…
                  </td>
                </tr>
              ) : rows.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-8 text-center text-muted-foreground">
                    {statusFilter === ALL_STATUSES
                      ? `No ${plural} yet.`
                      : `No ${plural} with status "${REMINDER_STATUS_LABELS[statusFilter]}".`}
                  </td>
                </tr>
              ) : (
                rows.map((item) => {
                  const busy = busyId === item.id;
                  const options = nextReminderStatuses(item.status);
                  return (
                    <tr key={item.id} className="bg-background/40 hover:bg-muted/70">
                      <td className="px-4 py-3 align-top">
                        <div className="font-medium text-foreground">{item.title}</div>
                        {item.message && item.message !== item.title && (
                          <div className="mt-1 text-xs text-muted-foreground">{item.message}</div>
                        )}
                        {item.last_error && (
                          <div className="mt-1 text-xs text-red-300" title={item.last_error}>
                            Last run failed: {item.last_error}
                          </div>
                        )}
                      </td>
                      <td className="px-4 py-3 align-top">
                        <span
                          data-testid={`${kind}-status-${item.id}`}
                          className={cn(
                            "inline-flex items-center rounded-full border px-2.5 py-1 text-xs",
                            STATUS_STYLES[item.status],
                          )}
                        >
                          {REMINDER_STATUS_LABELS[item.status]}
                        </span>
                      </td>
                      <td className="px-4 py-3 align-top text-foreground">
                        {describeDueLabel(item)}
                      </td>
                      <td className="px-4 py-3 align-top text-foreground">
                        {item.action === "agent" ? "Agent" : "Notify"}
                        {item.auto_complete && (
                          <span className="ml-1 text-xs text-muted-foreground">
                            (auto-done)
                          </span>
                        )}
                      </td>
                      <td className="px-4 py-3 align-top text-foreground">
                        {item.fired_at_ms
                          ? new Date(item.fired_at_ms).toLocaleString()
                          : "—"}
                        {item.runs > 0 && (
                          <span className="ml-1 text-xs text-muted-foreground">
                            ×{item.runs}
                          </span>
                        )}
                      </td>
                      <td className="px-4 py-3 align-top text-right">
                        <div className="flex flex-wrap items-center justify-end gap-1">
                          {/*
                            The primary mark/unmark control. `pending` is always
                            offered — reopening is legal from every state, and it
                            is also how a fired-but-still-pending item is
                            re-armed, so it must not be hidden just because the
                            status already reads "pending".
                          */}
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-8 px-2 text-xs"
                            disabled={busy}
                            data-testid={`${kind}-reopen-${item.id}`}
                            onClick={() => void setStatus(item, "pending")}
                          >
                            Reopen
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-8 px-2 text-xs"
                            disabled={busy || !options.includes("completed")}
                            data-testid={`${kind}-complete-${item.id}`}
                            onClick={() => void setStatus(item, "completed")}
                          >
                            Done
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-8 px-2 text-xs"
                            disabled={busy || !options.includes("cancelled")}
                            data-testid={`${kind}-cancel-${item.id}`}
                            onClick={() => void setStatus(item, "cancelled")}
                          >
                            Cancel
                          </Button>
                          {item.status === "pending" && (
                            <Button
                              variant="outline"
                              size="sm"
                              className="h-8 px-2 text-xs"
                              disabled={busy || !options.includes("in_progress")}
                              data-testid={`${kind}-start-${item.id}`}
                              onClick={() => void setStatus(item, "in_progress")}
                            >
                              Start
                            </Button>
                          )}
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-8 px-2 text-muted-foreground hover:text-blue-300"
                            title="View run history"
                            aria-label={`View run history for ${item.title}`}
                            onClick={() => setHistoryItem(item)}
                          >
                            <History className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-8 px-2 text-muted-foreground hover:text-foreground"
                            title={`Edit ${item.title}`}
                            aria-label={`Edit ${item.title}`}
                            onClick={() => {
                              setEditing(item);
                              setDialogOpen(true);
                            }}
                          >
                            <PencilLine className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-8 px-2 text-muted-foreground hover:text-amber-300"
                            title={item.status === "cancelled" ? "Run anyway" : "Run now"}
                            aria-label={`Run ${item.title} now`}
                            disabled={busy || item.status === "completed" || item.status === "cancelled"}
                            onClick={() => void runNow(item)}
                          >
                            <Zap className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-8 px-2 text-muted-foreground hover:text-red-300"
                            title={`Delete ${item.title}`}
                            aria-label={`Delete ${item.title}`}
                            onClick={() => setPendingDelete(item)}
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
        {showPager && (
          <div className="border-t border-border px-4 py-2 text-xs text-muted-foreground">
            Showing {rows.length} of {total} {plural}. The list is sorted by due
            time, soonest first, with undated items last.
          </div>
        )}
      </div>

      <ReminderTaskDialog
        open={dialogOpen}
        kind={kind}
        item={editing}
        onOpenChange={setDialogOpen}
        onSave={submit}
      />

      <ConfirmDialog
        open={pendingDelete !== null}
        title={`Delete ${pendingDelete?.status === "cancelled" ? "cancelled " : ""}${noun}?`}
        // Same rule as the jobs view: native `window.confirm` silently returns
        // false in the desktop WKWebView, so the guard must be a rendered dialog.
        description={
          pendingDelete && (
            <>
              <span className="font-medium text-foreground break-all">
                {pendingDelete.title}
              </span>{" "}
              will be removed from the list.{" "}
              {pendingDelete.due_at_ms
                ? "It will no longer fire."
                : "It has no due date, so it was never going to fire."}{" "}
              Anything it already delivered stays in the outbox.
            </>
          )
        }
        confirmLabel="Delete"
        pendingLabel="Deleting…"
        onConfirm={async () => {
          if (!pendingDelete) return;
          // A rejected delete propagates so the dialog shows the reason inline
          // and stays open, rather than emptying the row on screen.
          await deleteItem(pendingDelete);
          setPendingDelete(null);
        }}
        onCancel={() => setPendingDelete(null)}
      />

      {historyItem && (
        <CronHistoryPanel
          jobId={api.reminderDeliveryId(kind, historyItem.id)}
          jobName={historyItem.title}
          onClose={() => setHistoryItem(null)}
          fetchRuns={(limit, offset) => api.getReminderItemRuns(kind, historyItem.id, limit, offset)}
        />
      )}
    </section>
  );
}
