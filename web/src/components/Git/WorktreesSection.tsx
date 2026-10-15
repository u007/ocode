import { useEffect, useState } from "react";
import { AlertTriangle, ChevronDown, ChevronRight, GitBranch, Plus, Trash2 } from "lucide-react";
import { api } from "@/api/client";
import type { GitWorktree } from "@/api/types";
import ConfirmDialog from "@/components/common/ConfirmDialog";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

interface Props {
  projectPath: string | undefined;
  projectHost: string | undefined;
  expanded: boolean;
  onToggle: () => void;
  /** Registers the checkout as a project and activates it. Rejects with the
   *  reason when it cannot. Omitted: the list has no Open action. */
  onOpenWorktree?: (path: string) => Promise<void>;
  className: string;
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function shortHead(head: string): string {
  return head.slice(0, 7);
}

/**
 * Worktrees section of the Git tab: the project's checkouts, with open,
 * create and remove. The parent keys this component by project path, so
 * switching projects discards the old list instead of showing it.
 *
 * Local projects only. A remote project shows a notice and never fetches,
 * because the server would otherwise resolve the request to this machine.
 */
export default function WorktreesSection({
  projectPath,
  projectHost,
  expanded,
  onToggle,
  onOpenWorktree,
  className,
}: Props) {
  const [worktrees, setWorktrees] = useState<GitWorktree[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [opError, setOpError] = useState<string | null>(null);
  const [openingPath, setOpeningPath] = useState<string | null>(null);

  const [createOpen, setCreateOpen] = useState(false);
  const [branchInput, setBranchInput] = useState("");
  const [createBusy, setCreateBusy] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);

  const [pendingRemove, setPendingRemove] = useState<GitWorktree | null>(null);

  const isRemote = Boolean(projectHost);
  const shouldLoad = expanded && !isRemote && Boolean(projectPath);

  useEffect(() => {
    if (!shouldLoad || !projectPath) return;
    let cancelled = false;
    api.gitWorktrees(projectPath).then(
      (list) => {
        if (cancelled) return;
        setWorktrees(list);
        setLoadError(null);
      },
      (err: unknown) => {
        console.error("Failed to list worktrees:", err);
        if (!cancelled) setLoadError(errorMessage(err));
      },
    );
    return () => {
      cancelled = true;
    };
  }, [shouldLoad, projectPath]);

  const openWorktree = async (wt: GitWorktree, open: (path: string) => Promise<void>) => {
    setOpError(null);
    setOpeningPath(wt.path);
    try {
      await open(wt.path);
    } catch (err) {
      console.error("Failed to open worktree:", wt.path, err);
      setOpError(`Could not open ${wt.path}: ${errorMessage(err)}`);
    } finally {
      setOpeningPath(null);
    }
  };

  const submitCreate = async () => {
    const name = branchInput.trim();
    if (!name || !projectPath) return;
    setCreateBusy(true);
    setCreateError(null);
    try {
      const list = await api.gitWorktreeCreate(name, projectPath);
      setWorktrees(list);
      setLoadError(null);
      setBranchInput("");
      setCreateOpen(false);
    } catch (err) {
      console.error("Failed to create worktree:", name, err);
      setCreateError(errorMessage(err));
    } finally {
      setCreateBusy(false);
    }
  };

  const confirmRemove = async () => {
    if (!pendingRemove || !projectPath) return;
    const list = await api.gitWorktreeRemove(pendingRemove.path, projectPath);
    setWorktrees(list);
    setPendingRemove(null);
  };

  const count = worktrees ? worktrees.length : 0;

  return (
    <div className={`min-h-0 flex flex-col border-t border-border ${className}`}>
      <div className="px-3 py-1.5 text-xs uppercase tracking-wider text-muted-foreground flex items-center justify-between">
        <button
          onClick={onToggle}
          title={expanded ? "Collapse worktrees" : "Expand worktrees"}
          className="flex items-center gap-1 min-w-0 text-left cursor-pointer hover:text-foreground"
        >
          {expanded ? (
            <ChevronDown className="w-3.5 h-3.5 shrink-0" />
          ) : (
            <ChevronRight className="w-3.5 h-3.5 shrink-0" />
          )}
          <span className="truncate">
            Worktrees{" "}
            {worktrees && <span className="text-foreground/50">({count})</span>}
          </span>
        </button>
        {!isRemote && (
          <button
            onClick={() => {
              setCreateError(null);
              setCreateOpen(true);
            }}
            disabled={!projectPath}
            title="Create a worktree for a branch"
            className="flex items-center gap-0.5 text-[10px] normal-case text-purple-400 hover:text-purple-300 hover:underline disabled:opacity-40 disabled:no-underline"
          >
            <Plus className="w-3 h-3" />
            New
          </button>
        )}
      </div>

      {expanded && (
        <div className="flex-1 overflow-y-auto divide-y divide-border/60">
          {isRemote ? (
            <div className="px-3 py-2 text-xs text-muted-foreground/70 italic">
              Worktrees are available for local projects only.
            </div>
          ) : loadError ? (
            <div role="alert" className="px-3 py-2 text-xs text-red-400 break-words">
              Could not list worktrees: {loadError}
            </div>
          ) : worktrees === null ? (
            <div className="px-3 py-2 text-xs text-muted-foreground/70 italic">Loading…</div>
          ) : (
            worktrees.map((wt) => {
              const label = wt.branch || (wt.detached ? `detached ${shortHead(wt.head)}` : shortHead(wt.head));
              return (
                <div
                  key={wt.path}
                  className="group flex items-center gap-1 pl-2 pr-1.5 py-1.5 hover:bg-muted/50"
                >
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-1.5 text-xs font-mono text-foreground min-w-0">
                      <GitBranch className="w-3 h-3 shrink-0 text-muted-foreground" />
                      <span className="truncate">{label}</span>
                      {wt.is_main && <span className="text-[10px] text-muted-foreground shrink-0">main</span>}
                      {wt.is_current && <span className="text-[10px] text-purple-400 shrink-0">open</span>}
                      {wt.locked && <span className="text-[10px] text-amber-500 shrink-0">locked</span>}
                      {wt.prunable && <span className="text-[10px] text-red-400 shrink-0">missing</span>}
                    </div>
                    <div className="pl-5 text-[11px] text-muted-foreground truncate" title={wt.path}>
                      {wt.path}
                    </div>
                  </div>
                  {onOpenWorktree && (
                    <button
                      onClick={() => openWorktree(wt, onOpenWorktree)}
                      disabled={wt.is_current || openingPath !== null}
                      title={wt.is_current ? "Already the open project" : "Open as project"}
                      className="text-[10px] text-purple-400 hover:text-purple-300 hover:underline disabled:opacity-40 disabled:no-underline px-1"
                    >
                      {openingPath === wt.path ? "Opening…" : "Open"}
                    </button>
                  )}
                  {!wt.is_main && (
                    <button
                      onClick={() => setPendingRemove(wt)}
                      disabled={wt.is_current}
                      title={wt.is_current ? "Switch to another project before removing this worktree" : "Remove worktree"}
                      aria-label={`Remove worktree ${wt.path}`}
                      className="p-1 rounded text-muted-foreground hover:text-red-400 disabled:opacity-40"
                    >
                      <Trash2 className="w-3 h-3" />
                    </button>
                  )}
                </div>
              );
            })
          )}
          {opError && (
            <div role="alert" className="px-3 py-1.5 text-[11px] text-red-400 break-words">
              {opError}
            </div>
          )}
        </div>
      )}

      <Dialog open={createOpen} onOpenChange={(open) => !open && !createBusy && setCreateOpen(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New worktree</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground mt-2">
            Checks out the branch into <span className="font-mono text-foreground">.worktrees/</span>.
            An existing local branch is reused; otherwise a new branch is created from HEAD.
          </p>
          <Input
            autoFocus
            value={branchInput}
            placeholder="feature/my-change"
            onChange={(e) => setBranchInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void submitCreate();
            }}
            className="font-mono text-sm mt-2"
            aria-label="Branch name"
          />
          {createError && (
            <div role="alert" className="text-xs text-red-400 break-words">
              {createError}
            </div>
          )}
          <DialogFooter className="mt-4">
            <Button variant="outline" onClick={() => setCreateOpen(false)} disabled={createBusy}>
              Cancel
            </Button>
            <Button onClick={() => void submitCreate()} disabled={createBusy || !branchInput.trim()}>
              {createBusy ? "Creating…" : "Create"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={pendingRemove !== null}
        title="Remove worktree"
        description={
          pendingRemove ? (
            <>
              <AlertTriangle className="inline w-4 h-4 text-amber-500 mr-1" />
              Remove <span className="font-mono text-foreground break-all">{pendingRemove.path}</span>? Git
              refuses if it has uncommitted or untracked changes, or is locked. The directory is deleted; the
              branch is kept.
            </>
          ) : null
        }
        confirmLabel="Remove"
        pendingLabel="Removing…"
        onConfirm={confirmRemove}
        onCancel={() => setPendingRemove(null)}
      />
    </div>
  );
}
