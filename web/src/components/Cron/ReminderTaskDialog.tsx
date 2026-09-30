import { useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type {
  CronPermissionMode,
  ReminderItem,
  ReminderItemAction,
  ReminderItemKind,
  ReminderItemWriteRequest,
} from "@/api/types";
import { datetimeLocalFromMs, msFromDatetimeLocal } from "./cronFormat";

interface Props {
  open: boolean;
  /** The collection this dialog writes to; fixes the copy and the due-date rule. */
  kind: ReminderItemKind;
  item: ReminderItem | null;
  onOpenChange: (open: boolean) => void;
  onSave: (item: ReminderItemWriteRequest) => Promise<void>;
}

type FormState = {
  title: string;
  message: string;
  notes: string;
  action: ReminderItemAction;
  autoComplete: boolean;
  permMode: CronPermissionMode;
  dueValue: string;
  /** Tasks may have no due date at all; a reminder may not. */
  hasDue: boolean;
};

const PERM_MODES: Array<{ value: CronPermissionMode; label: string }> = [
  { value: "normal", label: "Normal — ask before anything destructive" },
  { value: "sandbox", label: "Sandbox — no prompts, writes confined" },
  { value: "locked", label: "Locked — read only" },
  { value: "yolo", label: "YOLO — auto-approve everything" },
];

function defaultState(kind: ReminderItemKind, item: ReminderItem | null): FormState {
  if (!item) {
    // A reminder gets a sensible future due time pre-filled; a task starts with
    // no deadline, because "a task I have not committed to a date for" is the
    // common case and typing one in should be a deliberate act.
    const oneHourOut = datetimeLocalFromMs(Date.now() + 60 * 60 * 1000);
    return {
      title: "",
      message: "",
      notes: "",
      action: "notify",
      autoComplete: false,
      permMode: "normal",
      dueValue: kind === "reminder" ? oneHourOut : "",
      hasDue: kind === "reminder",
    };
  }
  return {
    title: item.title ?? "",
    message: item.message ?? "",
    notes: item.notes ?? "",
    action: item.action ?? "notify",
    autoComplete: item.auto_complete ?? false,
    permMode: item.perm_mode ?? "normal",
    dueValue: datetimeLocalFromMs(item.due_at_ms),
    hasDue: Boolean(item.due_at_ms),
  };
}

export default function ReminderTaskDialog({ open, kind, item, onOpenChange, onSave }: Props) {
  const [state, setState] = useState<FormState>(() => defaultState(kind, item));
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (open) {
      setState(defaultState(kind, item));
      setError(null);
      setSaving(false);
    }
  }, [open, kind, item]);

  const isReminder = kind === "reminder";
  const noun = isReminder ? "reminder" : "task";
  // auto_complete only means anything for a task whose firing runs an agent; the
  // server silently clears it elsewhere, so the UI must not offer it there.
  const autoCompleteApplies = kind === "task" && state.action === "agent";
  const permModeApplies = state.action === "agent";

  const submit = async () => {
    setError(null);
    const title = state.title.trim();
    if (!title) {
      setError(isReminder ? "What should this reminder say?" : "Title is required");
      return;
    }
    let dueAtMs = 0;
    if (state.hasDue) {
      dueAtMs = msFromDatetimeLocal(state.dueValue);
      if (!dueAtMs) {
        setError("Pick a valid date and time");
        return;
      }
      if (dueAtMs <= Date.now()) {
        setError("That time is already in the past — it would fire immediately");
        return;
      }
    } else if (isReminder) {
      setError("A reminder needs a time to fire at");
      return;
    }

    setSaving(true);
    try {
      await onSave({
        title,
        message: state.message.trim() || undefined,
        notes: state.notes.trim() || undefined,
        action: state.action,
        auto_complete: autoCompleteApplies ? state.autoComplete : false,
        perm_mode: permModeApplies ? state.permMode : undefined,
        due_at_ms: state.hasDue ? dueAtMs : 0,
      });
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to save ${noun}`);
    } finally {
      setSaving(false);
    }
  };

  const consequences = useMemo(() => {
    const parts: string[] = [];
    if (state.action === "agent") {
      parts.push(
        `Runs an agent turn with ${state.message.trim() ? "the message" : "the title"} as the prompt when it fires.`,
      );
      if (autoCompleteApplies && state.autoComplete) {
        parts.push(
          "Marks the task completed if the turn finishes without an error — a clean turn is not proof the work was done, so you can always unmark it.",
        );
      }
    } else {
      parts.push("Sends a notification. No model runs, so there is no token cost.");
    }
    if (isReminder) {
      parts.push("Rings once. It then shows as completed until you reopen it.");
    } else if (state.hasDue) {
      parts.push("Stays pending after firing unless auto-complete is on — you decide when it is done.");
    } else {
      parts.push("Has no due date, so it never fires on its own. It is a checklist entry.");
    }
    return parts;
  }, [state, isReminder, autoCompleteApplies]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto bg-card border-border text-foreground">
        <DialogHeader>
          <DialogTitle>
            {item ? `Edit ${noun}` : `Add ${noun}`}
          </DialogTitle>
          <DialogDescription className="text-muted-foreground">
            {isReminder
              ? "A one-shot nudge. It fires once at the time you pick, then completes."
              : "A checklist item. Give it a due date and an agent run if you want, or neither."}
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4">
          <label className="grid gap-2 text-sm">
            <span className="text-muted-foreground">
              {isReminder ? "Reminder" : "Title"}
            </span>
            <Input
              autoFocus
              value={state.title}
              onChange={(e) => setState((prev) => ({ ...prev, title: e.target.value }))}
              placeholder={isReminder ? "Stand-up in five minutes" : "Write the quarterly report"}
            />
          </label>

          <label className="grid gap-2 text-sm">
            <span className="text-muted-foreground">
              {state.action === "agent" ? "Prompt" : "Message"}
              <span className="ml-2 text-xs text-muted-foreground/70">
                {state.action === "agent"
                  ? "Sent to the agent when it fires. Defaults to the title."
                  : "Shown in the notification. Defaults to the title."}
              </span>
            </span>
            <textarea
              value={state.message}
              onChange={(e) => setState((prev) => ({ ...prev, message: e.target.value }))}
              rows={3}
              className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
              placeholder={isReminder ? "Anything you want to be reminded about" : "Optional detail"}
            />
          </label>

          <div className="grid gap-4 sm:grid-cols-2">
            <label className="grid gap-2 text-sm">
              <span className="text-muted-foreground">When it fires</span>
              <Select
                value={state.action}
                onValueChange={(v) =>
                  setState((prev) => ({ ...prev, action: v as ReminderItemAction }))
                }
              >
                <SelectTrigger data-testid="reminder-action-select">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="notify">Notify me</SelectItem>
                  <SelectItem value="agent">Run an agent</SelectItem>
                </SelectContent>
              </Select>
            </label>

            <label className="grid gap-2 text-sm">
              <span className="text-muted-foreground">Permission mode</span>
              <Select
                value={state.permMode}
                disabled={!permModeApplies}
                onValueChange={(v) =>
                  setState((prev) => ({ ...prev, permMode: v as CronPermissionMode }))
                }
              >
                <SelectTrigger data-testid="reminder-perm-select" disabled={!permModeApplies}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PERM_MODES.map((mode) => (
                    <SelectItem key={mode.value} value={mode.value}>
                      {mode.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </label>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            {kind === "task" && (
              <label className="flex items-start gap-2 text-sm">
                <input
                  type="checkbox"
                  className="mt-0.5 h-4 w-4"
                  checked={state.hasDue}
                  data-testid="reminder-has-due"
                  onChange={(e) =>
                    setState((prev) => ({ ...prev, hasDue: e.target.checked }))
                  }
                />
                <span>
                  <span className="text-foreground">Give it a due date</span>
                  <span className="block text-xs text-muted-foreground">
                    Without one it is a plain checklist entry and never fires.
                  </span>
                </span>
              </label>
            )}

            {autoCompleteApplies && (
              <label className="flex items-start gap-2 text-sm">
                <input
                  type="checkbox"
                  className="mt-0.5 h-4 w-4"
                  checked={state.autoComplete}
                  data-testid="reminder-auto-complete"
                  onChange={(e) =>
                    setState((prev) => ({ ...prev, autoComplete: e.target.checked }))
                  }
                />
                <span>
                  <span className="text-foreground">Mark done after the agent runs</span>
                  <span className="block text-xs text-muted-foreground">
                    Only when the turn finishes without an error.
                  </span>
                </span>
              </label>
            )}
          </div>

          {state.hasDue && (
            <label className="grid gap-2 text-sm">
              <span className="text-muted-foreground">
                {isReminder ? "Fires at" : "Due"}
              </span>
              <Input
                type="datetime-local"
                data-testid="reminder-due-input"
                value={state.dueValue}
                onChange={(e) => setState((prev) => ({ ...prev, dueValue: e.target.value }))}
              />
            </label>
          )}

          <label className="grid gap-2 text-sm">
            <span className="text-muted-foreground">Notes</span>
            <Input
              value={state.notes}
              onChange={(e) => setState((prev) => ({ ...prev, notes: e.target.value }))}
              placeholder="Optional context, never shown in a notification"
            />
          </label>

          <div className="rounded-md border border-border bg-background/60 px-3 py-2 text-xs text-muted-foreground">
            <div className="font-medium text-foreground">What will happen</div>
            <ul className="mt-1 list-disc space-y-0.5 pl-4">
              {consequences.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
          </div>

          {error && (
            <div
              role="alert"
              className="rounded-md border border-red-900 bg-red-950/60 px-3 py-2 text-sm text-red-200"
            >
              {error}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            data-dialog-default-action
          >
            Cancel
          </Button>
          <Button onClick={() => void submit()} disabled={saving}>
            {saving ? "Saving…" : item ? "Save changes" : `Add ${noun}`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
