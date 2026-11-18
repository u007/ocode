import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
} from "react";
import {
  ArrowRightLeft,
  Check,
  ChevronDown,
  ChevronRight,
  GitBranch,
  GitMerge,
  MoreHorizontal,
  Plus,
  RefreshCw,
  Tag,
  Trash2,
  Upload,
} from "lucide-react";
import { api } from "@/api/client";
import { cn } from "@/lib/utils";
import { timeAgo } from "./GitPanel";
import { ContextMenu, type ContextMenuItem } from "@/components/Layout/ContextMenu";
import ConfirmDialog from "@/components/common/ConfirmDialog";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type {
  GitBranch as GitBranchRow,
  GitBranches,
  GitRefActionRequest,
  GitWorkspace,
} from "@/api/types";

const NOTICE_MS = 5000;

/** The ref a new branch starts from when the picker was left alone. */
const START_HEAD = "HEAD";

interface Props {
  projectPath?: string;
  projectHost?: string;
  /** True while the Git view is frontmost; the panel is force-mounted. */
  active: boolean;
  /** Bumped by GitPanel after its own refresh so this list picks up commits,
   *  fetches and checkouts made elsewhere. */
  refreshToken: number;
  /** The checked-out branch as `git status` reports it. */
  currentBranch: string;
  /** True when the working tree has staged, unstaged or conflicted paths. */
  dirty: boolean;
  /** Applies a ref action's refreshed workspace to the parent Git panel. */
  onWorkspaceChanged: (ws: GitWorkspace) => void;
}

function messageOf(e: unknown, fallback: string): string {
  return e instanceof Error && e.message ? e.message : fallback;
}

/** `origin/feature/x` → `feature/x`. Remote names cannot contain a slash. */
function localNameForRemote(name: string): string {
  const i = name.indexOf("/");
  return i >= 0 ? name.slice(i + 1) : name;
}

function remoteNameFor(b: GitBranchRow): string {
  if (b.remote) return b.remote;
  const i = b.name.indexOf("/");
  return i > 0 ? b.name.slice(0, i) : "";
}

function Section({
  title,
  count,
  icon,
  defaultOpen = true,
  children,
}: {
  title: string;
  count: number;
  icon: ReactNode;
  defaultOpen?: boolean;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="border-b border-border">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex w-full items-center gap-1 px-3 py-1.5 text-left text-xs uppercase tracking-wider text-muted-foreground hover:text-foreground"
      >
        {open ? <ChevronDown className="h-3.5 w-3.5 shrink-0" /> : <ChevronRight className="h-3.5 w-3.5 shrink-0" />}
        <span className="flex items-center gap-1">
          {icon}
          {title} <span className="text-foreground/50">({count})</span>
        </span>
      </button>
      {open && <div className="pb-1">{children}</div>}
    </div>
  );
}

/**
 * Branches view of the Git tab: local branches, remote-tracking branches and
 * tags, with the actions SourceTree puts on its branch list — switch, create
 * from any start point, merge, rebase, rename, delete (with a force fallback),
 * push and upstream editing.
 *
 * All mutations run through the server's closed ref-action / push vocabularies;
 * this component never builds a git command.
 */
export default function BranchesPanel({
  projectPath,
  projectHost,
  active,
  refreshToken,
  currentBranch,
  dirty,
  onWorkspaceChanged,
}: Props) {
  const [data, setData] = useState<GitBranches | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [filter, setFilter] = useState("");
  const [menu, setMenu] = useState<{
    items: ContextMenuItem[];
    position: { x: number; y: number };
  } | null>(null);

  const [pendingSwitch, setPendingSwitch] = useState<GitBranchRow | null>(null);
  const [newBranch, setNewBranch] = useState<{
    name: string;
    start: string;
    checkout: boolean;
    error: string | null;
    busy: boolean;
  } | null>(null);
  const [rename, setRename] = useState<{
    branch: GitBranchRow;
    value: string;
    error: string | null;
    busy: boolean;
  } | null>(null);
  const [del, setDel] = useState<{ branch: GitBranchRow; error: string | null; busy: boolean } | null>(null);
  const [delRemote, setDelRemote] = useState<GitBranchRow | null>(null);
  const [tagDelete, setTagDelete] = useState<GitBranchRow | null>(null);
  const [rebase, setRebase] = useState<GitBranchRow | null>(null);
  const [push, setPush] = useState<{
    branch: GitBranchRow;
    remote: string;
    setUpstream: boolean;
    error: string | null;
    busy: boolean;
  } | null>(null);
  const [upstream, setUpstream] = useState<{ branch: GitBranchRow; value: string; error: string | null; busy: boolean } | null>(
    null,
  );

  const generation = useRef(0);
  const noticeTimer = useRef<number | null>(null);

  const showNotice = useCallback((message: string) => {
    setNotice(message);
    if (noticeTimer.current !== null) window.clearTimeout(noticeTimer.current);
    noticeTimer.current = window.setTimeout(() => {
      setNotice(null);
      noticeTimer.current = null;
    }, NOTICE_MS);
  }, []);

  useEffect(() => {
    return () => {
      if (noticeTimer.current !== null) window.clearTimeout(noticeTimer.current);
    };
  }, []);

  const load = useCallback(
    async (spinner: boolean) => {
      if (!projectPath) return;
      const gen = ++generation.current;
      if (spinner) setLoading(true);
      try {
        const res = await api.gitBranches(projectPath, projectHost);
        if (gen !== generation.current) return;
        setData(res);
        setLoadError(null);
      } catch (e) {
        console.error("Branches: loading the ref list failed", e);
        if (gen !== generation.current) return;
        if (spinner) setLoadError(messageOf(e, "failed to load branches"));
      } finally {
        if (gen === generation.current) setLoading(false);
      }
    },
    [projectPath, projectHost],
  );

  // A new project/host discards the old list so one project's branches are
  // never shown under another.
  useEffect(() => {
    setData(null);
    setActionError(null);
  }, [projectPath, projectHost]);

  useEffect(() => {
    if (!active || !projectPath) return;
    void load(true);
  }, [active, projectPath, load]);

  const seenToken = useRef(refreshToken);
  useEffect(() => {
    if (refreshToken === seenToken.current) return;
    seenToken.current = refreshToken;
    if (active && projectPath) void load(false);
  }, [refreshToken, active, projectPath, load]);

  const doRef = useCallback(
    async (req: GitRefActionRequest, done: string) => {
      if (!projectPath) return;
      setBusy(true);
      setActionError(null);
      try {
        const res = await api.gitRefAction(req, projectPath, projectHost);
        onWorkspaceChanged(res.workspace);
        showNotice(done);
        await load(false);
      } finally {
        setBusy(false);
      }
    },
    [projectPath, projectHost, onWorkspaceChanged, showNotice, load],
  );

  const runMenuAction = (req: GitRefActionRequest, done: string) => () => {
    void doRef(req, done).catch((e: unknown) => {
      console.error("Branches: ref action failed", { action: req.action, target: req.target, error: e });
      setActionError(messageOf(e, "git action failed"));
    });
  };

  const switchBranch = useCallback(
    async (b: GitBranchRow) => {
      if (b.kind === "remote") {
        const local = localNameForRemote(b.name);
        const exists = data?.local.some((l) => l.name === local);
        if (exists) {
          await doRef({ action: "checkout", target: local }, `Switched to ${local}`);
        } else {
          await doRef(
            { action: "branch-checkout", target: b.name, name: local },
            `Checked out ${local} from ${b.name}`,
          );
        }
        return;
      }
      await doRef({ action: "checkout", target: b.name }, `Switched to ${b.name}`);
    },
    [data, doRef],
  );

  const requestSwitch = (b: GitBranchRow) => {
    if (b.current) return;
    if (dirty) {
      setPendingSwitch(b);
      return;
    }
    void switchBranch(b).catch((e: unknown) => setActionError(messageOf(e, "could not switch branch")));
  };

  const submitNewBranch = async () => {
    if (!newBranch || !projectPath || newBranch.busy) return;
    const name = newBranch.name.trim();
    if (!name) return;
    setNewBranch({ ...newBranch, busy: true, error: null });
    try {
      await doRef(
        { action: newBranch.checkout ? "branch-checkout" : "branch", target: newBranch.start, name },
        `Created ${name}`,
      );
      setNewBranch(null);
    } catch (e) {
      setNewBranch((cur) => (cur ? { ...cur, busy: false, error: messageOf(e, "could not create the branch") } : cur));
    }
  };

  const submitRename = async () => {
    if (!rename || rename.busy) return;
    const name = rename.value.trim();
    if (!name) return;
    setRename({ ...rename, busy: true, error: null });
    try {
      await doRef({ action: "branch-rename", target: rename.branch.name, name }, `Renamed to ${name}`);
      setRename(null);
    } catch (e) {
      setRename((cur) => (cur ? { ...cur, busy: false, error: messageOf(e, "could not rename the branch") } : cur));
    }
  };

  const submitDelete = async (force: boolean) => {
    if (!del || del.busy) return;
    setDel({ ...del, busy: true, error: null });
    try {
      await doRef(
        { action: force ? "branch-force-delete" : "branch-delete", target: del.branch.name },
        `Deleted ${del.branch.name}`,
      );
      setDel(null);
    } catch (e) {
      setDel((cur) => (cur ? { ...cur, busy: false, error: messageOf(e, "could not delete the branch") } : cur));
    }
  };

  const submitDeleteRemote = async () => {
    if (!delRemote || !projectPath) return;
    const remote = remoteNameFor(delRemote);
    const branch = localNameForRemote(delRemote.name);
    setBusy(true);
    setActionError(null);
    try {
      await api.gitPushBranch({ remote, branch, delete: true }, projectPath, projectHost);
      showNotice(`Deleted ${delRemote.name} on ${remote}`);
      setDelRemote(null);
      await load(false);
    } catch (e) {
      console.error("Branches: deleting the remote branch failed", { remote, branch, error: e });
      throw e;
    } finally {
      setBusy(false);
    }
  };

  const submitPush = async () => {
    if (!push || !projectPath || push.busy) return;
    setPush({ ...push, busy: true, error: null });
    try {
      await api.gitPushBranch(
        { remote: push.remote.trim(), branch: push.branch.name, set_upstream: push.setUpstream },
        projectPath,
        projectHost,
      );
      showNotice(`Pushed ${push.branch.name} to ${push.remote.trim()}`);
      setPush(null);
      await load(false);
    } catch (e) {
      setPush((cur) => (cur ? { ...cur, busy: false, error: messageOf(e, "could not push the branch") } : cur));
    }
  };

  const submitUpstream = async () => {
    if (!upstream || upstream.busy) return;
    setBusy(true);
    setActionError(null);
    try {
      await doRef({ action: "set-upstream", target: upstream.branch.name, name: upstream.value }, `Tracking ${upstream.value}`);
      setUpstream(null);
    } catch (e) {
      setUpstream((cur) => (cur ? { ...cur, busy: false, error: messageOf(e, "could not set the upstream") } : cur));
    } finally {
      setBusy(false);
    }
  };

  const unsetUpstream = (b: GitBranchRow) => {
    void doRef({ action: "unset-upstream", target: b.name }, `Stopped tracking ${b.upstream ?? "the upstream"}`).catch(
      (e: unknown) => setActionError(messageOf(e, "could not unset the upstream")),
    );
  };

  const remotes = useMemo(() => {
    const names = new Set<string>();
    for (const b of data?.remote ?? []) {
      const name = remoteNameFor(b);
      if (name) names.add(name);
    }
    return [...names];
  }, [data]);

  const defaultRemote = (b: GitBranchRow): string => {
    if (b.upstream) return b.upstream.slice(0, Math.max(0, b.upstream.indexOf("/")));
    return remotes[0] ?? "origin";
  };

  const openCreateDialog = (start: string) => {
    setNewBranch({ name: "", start, checkout: true, error: null, busy: false });
  };

  const menuItems = (b: GitBranchRow): ContextMenuItem[] => {
    const items: ContextMenuItem[] = [];
    const disabled = busy;
    if (b.kind === "local") {
      items.push({
        label: b.current ? "Current branch" : `Switch to ${b.name}`,
        onClick: () => requestSwitch(b),
        disabled: disabled || b.current,
      });
      if (currentBranch && !b.current) {
        items.push({
          label: `Merge ${b.name} into ${currentBranch}`,
          onClick: runMenuAction({ action: "merge", target: b.name }, `Merged ${b.name}`),
          disabled,
        });
        items.push({
          label: `Rebase ${currentBranch} onto ${b.name}`,
          onClick: () => setRebase(b),
          disabled,
        });
      }
      items.push({ label: "", onClick: () => {}, separator: true });
      items.push({
        label: "New branch from this…",
        onClick: () => openCreateDialog(b.name),
        disabled,
      });
      items.push({
        label: "Push branch…",
        onClick: () => setPush({ branch: b, remote: defaultRemote(b), setUpstream: !b.upstream, error: null, busy: false }),
        disabled,
      });
      items.push({
        label: b.upstream ? `Set upstream (now ${b.upstream})…` : "Set upstream…",
        onClick: () => setUpstream({ branch: b, value: b.upstream ?? data?.remote[0]?.name ?? "", error: null, busy: false }),
        disabled: disabled || !data || data.remote.length === 0,
      });
      if (b.upstream) {
        items.push({ label: "Stop tracking upstream", onClick: () => unsetUpstream(b), disabled });
      }
      items.push({
        label: "Rename…",
        onClick: () => setRename({ branch: b, value: b.name, error: null, busy: false }),
        disabled,
      });
      items.push({ label: "", onClick: () => {}, separator: true });
      items.push({
        label: `Delete ${b.name}`,
        destructive: true,
        onClick: () => setDel({ branch: b, error: null, busy: false }),
        disabled: disabled || b.current,
      });
    } else if (b.kind === "remote") {
      const local = localNameForRemote(b.name);
      const exists = data?.local.some((l) => l.name === local);
      items.push({
        label: exists ? `Switch to local ${local}` : `Check out ${local} (tracking)`,
        onClick: () => requestSwitch(b),
        disabled,
      });
      if (currentBranch) {
        items.push({
          label: `Merge ${b.name} into ${currentBranch}`,
          onClick: runMenuAction({ action: "merge", target: b.name }, `Merged ${b.name}`),
          disabled,
        });
        items.push({
          label: `Rebase ${currentBranch} onto ${b.name}`,
          onClick: () => setRebase(b),
          disabled,
        });
      }
      items.push({ label: "", onClick: () => {}, separator: true });
      items.push({ label: "New branch from this…", onClick: () => openCreateDialog(b.name), disabled });
      items.push({
        label: `Delete ${b.name} on ${remoteNameFor(b)}`,
        destructive: true,
        onClick: () => setDelRemote(b),
        disabled,
      });
    } else {
      items.push({
        label: `Check out ${b.name} (detached)`,
        onClick: runMenuAction({ action: "checkout", target: b.name }, `Checked out ${b.name}`),
        disabled,
      });
      items.push({ label: "New branch from this tag…", onClick: () => openCreateDialog(b.name), disabled });
      items.push({ label: "", onClick: () => {}, separator: true });
      items.push({
        label: `Delete tag ${b.name}`,
        destructive: true,
        onClick: () => setTagDelete(b),
        disabled,
      });
    }
    return items;
  };

  const openMenuAt = (e: ReactMouseEvent, b: GitBranchRow) => {
    e.preventDefault();
    setMenu({ items: menuItems(b), position: { x: e.clientX, y: e.clientY } });
  };
  const openMenuFromButton = (e: ReactMouseEvent<HTMLButtonElement>, b: GitBranchRow) => {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    setMenu({ items: menuItems(b), position: { x: r.left, y: r.bottom } });
  };

  const lower = filter.trim().toLowerCase();
  const matching = (list: GitBranchRow[]) => (lower ? list.filter((b) => b.name.toLowerCase().includes(lower)) : list);
  const local = matching(data?.local ?? []);
  const remote = matching(data?.remote ?? []);
  const tags = matching(data?.tags ?? []);
  const empty = data !== null && data.local.length + data.remote.length + data.tags.length === 0;

  const startOptions = useMemo(() => {
    const rows = [...(data?.local ?? []), ...(data?.remote ?? []), ...(data?.tags ?? [])];
    return rows.map((b) => ({ value: b.name, label: `${b.name} (${b.kind})` }));
  }, [data]);

  const renderRow = (b: GitBranchRow) => {
    const upstreamGone = b.upstream_gone === true;
    return (
      <div
        key={b.refname}
        role="row"
        data-testid="git-branch-row"
        data-branch-name={b.name}
        data-branch-kind={b.kind}
        onDoubleClick={() => requestSwitch(b)}
        onContextMenu={(e) => openMenuAt(e, b)}
        className={cn(
          "group flex cursor-default items-center gap-2 px-3 py-[3px] text-xs hover:bg-muted/40",
          b.current && "bg-blue-950/30",
        )}
      >
        <span className="flex w-3.5 shrink-0 justify-center">
          {b.current ? (
            <Check className="h-3.5 w-3.5 text-blue-400" aria-label="current branch" />
          ) : b.kind === "tag" ? (
            <Tag className="h-3 w-3 text-green-400/70" />
          ) : (
            <GitBranch className="h-3 w-3 text-muted-foreground/60" />
          )}
        </span>
        <span className={cn("truncate font-mono", b.current ? "text-blue-200" : "text-foreground")}>{b.name}</span>
        {b.kind === "local" && (b.ahead > 0 || b.behind > 0 || upstreamGone) && (
          <span className="flex shrink-0 items-center gap-1 text-[10px] font-semibold text-amber-500">
            {b.ahead > 0 && <span title={`${b.ahead} to push`}>↑{b.ahead}</span>}
            {b.behind > 0 && <span title={`${b.behind} to pull`}>↓{b.behind}</span>}
            {upstreamGone && <span title="The upstream branch no longer exists">gone</span>}
          </span>
        )}
        {b.kind === "local" && b.upstream && !upstreamGone && (
          <span className="shrink-0 truncate text-[10px] text-muted-foreground">{b.upstream}</span>
        )}
        <span className="ml-auto hidden min-w-0 max-w-[45%] truncate text-[11px] text-muted-foreground sm:inline">
          {b.subject}
        </span>
        <span className="w-16 shrink-0 text-right text-[10px] text-muted-foreground">{timeAgo(b.date)}</span>
        <button
          type="button"
          aria-label={`Actions for ${b.name}`}
          data-testid="git-branch-menu"
          onClick={(e) => openMenuFromButton(e, b)}
          className="shrink-0 rounded p-0.5 text-muted-foreground opacity-0 hover:bg-muted hover:text-foreground focus:opacity-100 group-hover:opacity-100"
        >
          <MoreHorizontal className="h-3.5 w-3.5" />
        </button>
      </div>
    );
  };

  const setBranchName = (name: string) =>
    setNewBranch((cur) => (cur ? { ...cur, name, error: null } : cur));

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="git-branches">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
        <input
          type="text"
          placeholder="Filter branches..."
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          aria-label="Filter branches"
          className="h-7 w-40 rounded-md border border-border bg-muted/40 px-2 text-xs focus:outline-none focus:ring-2 focus:ring-ring sm:w-56"
        />
        <span className="hidden text-xs text-muted-foreground sm:inline" data-testid="git-branches-counts">
          {local.length} local · {remote.length} remote · {tags.length} tags
        </span>
        <div className="ml-auto flex items-center gap-1">
          <button
            type="button"
            onClick={() => void load(false)}
            aria-label="Refresh branches"
            title="Refresh branches"
            className="rounded p-1 text-muted-foreground hover:bg-muted/60 hover:text-foreground"
          >
            <RefreshCw className="h-3.5 w-3.5" />
          </button>
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 px-2 text-xs"
            data-testid="git-branches-new"
            onClick={() => openCreateDialog(START_HEAD)}
          >
            <Plus className="h-3.5 w-3.5" />
            New branch
          </Button>
        </div>
      </div>

      {actionError && (
        <div
          role="alert"
          data-testid="git-branches-error"
          className="flex items-start gap-2 border-b border-border bg-red-500/10 px-3 py-1.5 text-xs text-red-400"
        >
          <span className="min-w-0 flex-1 whitespace-pre-wrap break-words">{actionError}</span>
          <button
            type="button"
            onClick={() => setActionError(null)}
            aria-label="Dismiss error"
            className="shrink-0 rounded p-0.5 hover:bg-red-500/20"
          >
            ×
          </button>
        </div>
      )}
      {loadError && (
        <div role="alert" className="border-b border-border bg-red-500/10 px-3 py-1.5 text-xs text-red-400">
          {loadError}
        </div>
      )}
      {notice && !actionError && (
        <div
          role="status"
          data-testid="git-branches-notice"
          className="border-b border-border bg-green-500/10 px-3 py-1.5 text-xs text-green-400"
        >
          {notice}
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-auto">
        {loading && data === null ? (
          <div className="px-3 py-6 text-center text-sm text-muted-foreground">Loading branches…</div>
        ) : empty ? (
          <div className="px-3 py-6 text-center text-sm text-muted-foreground">
            No branches. This project may not be a git repository.
          </div>
        ) : (
          <>
            <Section title="Local branches" count={local.length} icon={<GitBranch className="h-3 w-3" />}>
              {local.length === 0 ? (
                <div className="px-3 py-1 text-xs text-muted-foreground">No matching local branches.</div>
              ) : (
                local.map(renderRow)
              )}
            </Section>
            <Section title="Remote branches" count={remote.length} icon={<ArrowRightLeft className="h-3 w-3" />}>
              {remote.length === 0 ? (
                <div className="px-3 py-1 text-xs text-muted-foreground">
                  No remote branches. Fetch to update them.
                </div>
              ) : (
                remote.map(renderRow)
              )}
            </Section>
            <Section title="Tags" count={tags.length} icon={<Tag className="h-3 w-3" />} defaultOpen={tags.length > 0}>
              {tags.length === 0 ? (
                <div className="px-3 py-1 text-xs text-muted-foreground">No tags.</div>
              ) : (
                tags.map(renderRow)
              )}
            </Section>
          </>
        )}
      </div>

      {menu && (
        <ContextMenu items={menu.items} open position={menu.position} onClose={() => setMenu(null)} />
      )}

      {pendingSwitch && (
        <ConfirmDialog
          open
          title={`Switch to ${pendingSwitch.name}?`}
          description="You have uncommitted changes. git will refuse the switch if they would be overwritten; commit or stash them first to be safe."
          confirmLabel="Switch anyway"
          confirmVariant="default"
          pendingLabel="Switching…"
          onConfirm={async () => {
            await switchBranch(pendingSwitch);
            setPendingSwitch(null);
          }}
          onCancel={() => setPendingSwitch(null)}
        />
      )}

      {/* New branch: name + start point (current HEAD, any branch or tag). */}
      <Dialog open={newBranch !== null} onOpenChange={(open) => !open && !newBranch?.busy && setNewBranch(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <GitBranch className="h-4 w-4 text-blue-400" />
              New branch
            </DialogTitle>
          </DialogHeader>
          <div className="mt-2 space-y-3">
            <Input
              autoFocus
              value={newBranch?.name ?? ""}
              onChange={(e) => setBranchName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") void submitNewBranch();
              }}
              placeholder="Branch name"
              aria-label="Branch name"
            />
            <label className="block text-xs text-muted-foreground">
              Start point
              <Select
                value={newBranch?.start ?? START_HEAD}
                onValueChange={(v) => setNewBranch((cur) => (cur ? { ...cur, start: v, error: null } : cur))}
              >
                <SelectTrigger aria-label="Start point" className="mt-1 h-8">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={START_HEAD}>Current HEAD ({currentBranch || "detached"})</SelectItem>
                  {startOptions.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {o.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </label>
            <label className="flex cursor-pointer select-none items-center gap-2 text-sm text-muted-foreground">
              <input
                type="checkbox"
                checked={newBranch?.checkout ?? true}
                onChange={(e) => setNewBranch((cur) => (cur ? { ...cur, checkout: e.target.checked } : cur))}
              />
              Check out after creating
            </label>
            {newBranch?.error && (
              <div role="alert" className="whitespace-pre-wrap break-words text-xs text-red-400">
                {newBranch.error}
              </div>
            )}
          </div>
          <DialogFooter className="mt-4">
            <Button variant="outline" onClick={() => setNewBranch(null)} disabled={newBranch?.busy}>
              Cancel
            </Button>
            <Button onClick={() => void submitNewBranch()} disabled={newBranch?.busy || !newBranch?.name.trim()}>
              {newBranch?.checkout ? "Create and check out" : "Create"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Rename */}
      <Dialog open={rename !== null} onOpenChange={(open) => !open && !rename?.busy && setRename(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rename branch</DialogTitle>
          </DialogHeader>
          <div className="mt-2 space-y-3">
            <Input
              autoFocus
              value={rename?.value ?? ""}
              onChange={(e) => setRename((cur) => (cur ? { ...cur, value: e.target.value, error: null } : cur))}
              onKeyDown={(e) => {
                if (e.key === "Enter") void submitRename();
              }}
              aria-label="New branch name"
            />
            {rename?.error && (
              <div role="alert" className="whitespace-pre-wrap break-words text-xs text-red-400">
                {rename.error}
              </div>
            )}
          </div>
          <DialogFooter className="mt-4">
            <Button variant="outline" onClick={() => setRename(null)} disabled={rename?.busy}>
              Cancel
            </Button>
            <Button onClick={() => void submitRename()} disabled={rename?.busy || !rename?.value.trim()}>
              Rename
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete local branch: git refuses unmerged branches, and the same
          dialog then offers the force variant. */}
      <Dialog open={del !== null} onOpenChange={(open) => !open && !del?.busy && setDel(null)}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Trash2 className="h-4 w-4 text-red-400" />
              Delete branch {del?.branch.name}?
            </DialogTitle>
          </DialogHeader>
          <div className="mt-2 space-y-3 text-sm text-muted-foreground">
            <p>This removes the local branch. Commits only reachable from it become unreachable.</p>
            {del?.error && (
              <div role="alert" className="whitespace-pre-wrap break-words text-xs text-red-400">
                {del.error}
              </div>
            )}
          </div>
          <DialogFooter className="mt-4 gap-2">
            <Button variant="ghost" onClick={() => setDel(null)} disabled={del?.busy}>
              Cancel
            </Button>
            {del?.error && del.branch && (
              <Button variant="destructive" onClick={() => void submitDelete(true)} disabled={del.busy}>
                Force delete
              </Button>
            )}
            <Button variant="destructive" onClick={() => void submitDelete(false)} disabled={del?.busy}>
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {delRemote && (
        <ConfirmDialog
          open
          title={`Delete ${delRemote.name} on ${remoteNameFor(delRemote)}?`}
          description="The branch is deleted on the remote. Local branches tracking it keep their commits until you delete them too."
          confirmLabel="Delete remote branch"
          pendingLabel="Deleting…"
          onConfirm={async () => {
            await submitDeleteRemote();
          }}
          onCancel={() => setDelRemote(null)}
        />
      )}

      {tagDelete && (
        <ConfirmDialog
          open
          title={`Delete tag ${tagDelete.name}?`}
          description="Deletes the local tag. A tag already pushed to a remote stays there until it is deleted separately."
          confirmLabel="Delete tag"
          pendingLabel="Deleting…"
          onConfirm={async () => {
            await doRef({ action: "tag-delete", target: tagDelete.name }, `Deleted tag ${tagDelete.name}`);
            setTagDelete(null);
          }}
          onCancel={() => setTagDelete(null)}
        />
      )}

      {rebase && (
        <ConfirmDialog
          open
          title={`Rebase ${currentBranch || "the current branch"} onto ${rebase.name}?`}
          description="Local commits are replayed on top of the selected ref. Conflicts stop the rebase in the banner above, where it can be continued or aborted."
          confirmLabel="Rebase"
          confirmVariant="destructive"
          pendingLabel="Rebasing…"
          onConfirm={async () => {
            await doRef({ action: "rebase", target: rebase.name }, `Rebased onto ${rebase.name}`);
            setRebase(null);
          }}
          onCancel={() => setRebase(null)}
        />
      )}

      {/* Push one branch, optionally recording it as its own upstream. */}
      <Dialog open={push !== null} onOpenChange={(open) => !open && !push?.busy && setPush(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Upload className="h-4 w-4 text-blue-400" />
              Push {push?.branch.name}
            </DialogTitle>
          </DialogHeader>
          <div className="mt-2 space-y-3">
            <label className="block text-xs text-muted-foreground">
              Remote
              <Input
                className="mt-1"
                value={push?.remote ?? ""}
                onChange={(e) => setPush((cur) => (cur ? { ...cur, remote: e.target.value, error: null } : cur))}
                aria-label="Remote"
              />
            </label>
            <label className="flex cursor-pointer select-none items-center gap-2 text-sm text-muted-foreground">
              <input
                type="checkbox"
                checked={push?.setUpstream ?? false}
                onChange={(e) => setPush((cur) => (cur ? { ...cur, setUpstream: e.target.checked } : cur))}
              />
              Set this branch as the upstream (-u)
            </label>
            {push?.error && (
              <div role="alert" className="whitespace-pre-wrap break-words text-xs text-red-400">
                {push.error}
              </div>
            )}
          </div>
          <DialogFooter className="mt-4">
            <Button variant="outline" onClick={() => setPush(null)} disabled={push?.busy}>
              Cancel
            </Button>
            <Button onClick={() => void submitPush()} disabled={push?.busy || !push?.remote.trim()}>
              Push
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Upstream picker: only fetched remote-tracking branches are offered,
          matching the server's validation. */}
      <Dialog open={upstream !== null} onOpenChange={(open) => !open && !upstream?.busy && setUpstream(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <GitMerge className="h-4 w-4 text-blue-400" />
              Upstream for {upstream?.branch.name}
            </DialogTitle>
          </DialogHeader>
          <div className="mt-2 space-y-3">
            <Select
              value={upstream?.value ?? ""}
              onValueChange={(v) => setUpstream((cur) => (cur ? { ...cur, value: v, error: null } : cur))}
            >
              <SelectTrigger aria-label="Upstream branch">
                <SelectValue placeholder="Remote branch" />
              </SelectTrigger>
              <SelectContent>
                {(data?.remote ?? []).map((b) => (
                  <SelectItem key={b.refname} value={b.name}>
                    {b.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {upstream?.error && (
              <div role="alert" className="whitespace-pre-wrap break-words text-xs text-red-400">
                {upstream.error}
              </div>
            )}
          </div>
          <DialogFooter className="mt-4">
            <Button variant="outline" onClick={() => setUpstream(null)} disabled={upstream?.busy}>
              Cancel
            </Button>
            <Button onClick={() => void submitUpstream()} disabled={upstream?.busy || !upstream?.value}>
              Set upstream
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
