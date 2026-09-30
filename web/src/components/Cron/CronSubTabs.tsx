import { BellRing, CalendarClock, ListChecks } from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * The three views inside the Cron tab. One switcher rather than three top-level
 * tabs because all three read the same project store, share the outbox panel,
 * and are the same shape of object — splitting them across the top tab bar
 * would triple the top-level chrome for no added clarity.
 */
export type CronSubView = "jobs" | "reminders" | "tasks";

export const CRON_SUB_VIEWS: Array<{
  id: CronSubView;
  label: string;
  icon: typeof CalendarClock;
  blurb: string;
}> = [
  {
    id: "jobs",
    label: "Jobs",
    icon: CalendarClock,
    blurb: "Recurring and one-shot agent jobs that run on a schedule.",
  },
  {
    id: "reminders",
    label: "Reminders",
    icon: BellRing,
    blurb: "One-shot nudges. Each rings once at its due time, then completes.",
  },
  {
    id: "tasks",
    label: "Tasks",
    icon: ListChecks,
    blurb: "Checklist items. A due date and an agent run are both optional.",
  },
];

interface Props {
  value: CronSubView;
  onChange: (next: CronSubView) => void;
  /**
   * Badge counts rendered next to each label. A plain string map, not
   * Record<CronSubView, number>: the callers key the two list views by their
   * kind ("reminder"/"task") and the jobs view by "jobs", so a strict union here
   * would only force one of those spellings to be wrong.
   */
  counts?: Record<string, number | undefined>;
  disabled?: boolean;
}

/**
 * A segmented control built from plain buttons. It is intentionally NOT a Radix
 * Tabs: the three panes are all mounted by the parent (so a poll keeps running
 * and the table keeps its scroll position when you switch away and back), which
 * is the behaviour `TabsContent` unmounts by default. `aria-controls`/
 * `role="tabpanel"` still wire it up for assistive tech.
 */
export default function CronSubTabs({ value, onChange, counts, disabled }: Props) {
  return (
    <div
      role="tablist"
      aria-label="Cron view"
      className="flex flex-wrap items-center gap-1"
    >
      {CRON_SUB_VIEWS.map((view) => {
        const Icon = view.icon;
        const selected = view.id === value;
        const count = counts?.[view.id];
        return (
          <button
            key={view.id}
            type="button"
            role="tab"
            id={`cron-subtab-${view.id}`}
            aria-selected={selected}
            aria-controls={`cron-subpanel-${view.id}`}
            data-testid={`cron-subtab-${view.id}`}
            disabled={disabled}
            title={view.blurb}
            onClick={() => onChange(view.id)}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-sm transition-colors",
              "disabled:cursor-not-allowed disabled:opacity-50",
              selected
                ? "border-blue-700 bg-blue-950/60 text-blue-200"
                : "border-border bg-card text-muted-foreground hover:bg-muted hover:text-foreground",
            )}
          >
            <Icon className="h-3.5 w-3.5" />
            {view.label}
            {count !== undefined && count > 0 && (
              <span
                data-testid={`cron-subtab-count-${view.id}`}
                className={cn(
                  "rounded-full px-1.5 py-0.5 text-[10px] font-medium tabular-nums",
                  selected ? "bg-blue-900 text-blue-100" : "bg-muted text-muted-foreground",
                )}
              >
                {count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
