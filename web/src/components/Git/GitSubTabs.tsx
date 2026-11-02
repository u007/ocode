import { FileDiff, GitGraph } from "lucide-react";
import { cn } from "@/lib/utils";

/** The two views inside the Git tab: working-tree changes or commit history. */
export type GitView = "changes" | "history";

const GIT_VIEWS: Array<{ id: GitView; label: string; icon: typeof FileDiff }> = [
  { id: "changes", label: "Changes", icon: FileDiff },
  { id: "history", label: "History", icon: GitGraph },
];

interface Props {
  value: GitView;
  onChange: (next: GitView) => void;
}

/**
 * Segmented control built from plain buttons, not a Radix Tabs: both views are
 * switched by the parent, which keeps the commit box and dialogs mounted, so
 * the same switch shape as CronSubTabs is used.
 */
export default function GitSubTabs({ value, onChange }: Props) {
  return (
    <div role="tablist" aria-label="Git view" className="flex items-center gap-1">
      {GIT_VIEWS.map((view) => {
        const Icon = view.icon;
        const selected = view.id === value;
        return (
          <button
            key={view.id}
            type="button"
            role="tab"
            id={`git-subtab-${view.id}`}
            aria-selected={selected}
            aria-controls={`git-subpanel-${view.id}`}
            data-testid={`git-subtab-${view.id}`}
            onClick={() => onChange(view.id)}
            className={cn(
              "inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs transition-colors",
              selected
                ? "border-blue-700 bg-blue-950/60 text-blue-200"
                : "border-border bg-card text-muted-foreground hover:bg-muted hover:text-foreground",
            )}
          >
            <Icon className="h-3 w-3" />
            {view.label}
          </button>
        );
      })}
    </div>
  );
}
