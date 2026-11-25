import { useProjectWorktrees } from "@/hooks/useProjectWorktrees";
import type { GitWorktree } from "@/api/types";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";

interface Props {
  projectPath?: string;
  value?: string;
  onChange(path: string): void;
  className?: string;
  disabled?: boolean;
}

function shortHead(head: string): string {
  return head.slice(0, 7);
}

/** Branch when attached, otherwise the detached short HEAD. */
function checkoutLabel(wt: GitWorktree): string {
  if (wt.branch) return wt.branch;
  if (wt.detached) return `detached ${shortHead(wt.head)}`;
  return shortHead(wt.head);
}

function unavailableReason(wt: GitWorktree): string {
  if (wt.exists === false) return "missing";
  if (wt.prunable) return "prunable";
  return "";
}

/** Every checkout of the project's repository. Missing/prunable checkouts stay
 *  visible but disabled, and a failed or empty list renders in place: the
 *  selector never falls back to another checkout. */
export default function WorktreeSelect({
  projectPath,
  value,
  onChange,
  className,
  disabled,
}: Props) {
  const { worktrees, loading, error } = useProjectWorktrees(projectPath);

  if (error) {
    return (
      <div
        role="alert"
        className={cn(
          "rounded-md border border-destructive/40 px-3 py-2 text-sm text-destructive",
          className,
        )}
      >
        Could not load checkouts: {error}
      </div>
    );
  }
  if (!loading && worktrees.length === 0) {
    return (
      <div
        className={cn(
          "rounded-md border border-input px-3 py-2 text-sm text-muted-foreground",
          className,
        )}
      >
        {projectPath ? "No Git checkouts found." : "No project selected."}
      </div>
    );
  }

  return (
    <Select value={value} onValueChange={onChange} disabled={disabled || loading}>
      <SelectTrigger className={cn(className)} aria-label="Git checkout">
        <SelectValue placeholder={loading ? "Loading checkouts…" : "Select checkout"} />
      </SelectTrigger>
      <SelectContent>
        {worktrees.map((wt) => {
          const reason = unavailableReason(wt);
          return (
            <SelectItem
              key={wt.path}
              value={wt.path}
              title={wt.path}
              disabled={Boolean(reason)}
            >
              <span className="flex min-w-0 items-center gap-2">
                <span className="truncate font-medium">{checkoutLabel(wt)}</span>
                <span className="truncate font-mono text-xs text-muted-foreground">
                  {wt.path}
                </span>
                {reason ? (
                  <span className="shrink-0 text-xs text-muted-foreground">({reason})</span>
                ) : null}
              </span>
            </SelectItem>
          );
        })}
      </SelectContent>
    </Select>
  );
}
