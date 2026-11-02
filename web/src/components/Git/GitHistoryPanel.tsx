import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { GitBranch, RefreshCw, Tag, X } from "lucide-react";
import { api } from "@/api/client";
import { cn } from "@/lib/utils";
import { copyTextToClipboard } from "@/lib/clipboard";
import ConfirmDialog from "@/components/common/ConfirmDialog";
import { ContextMenu } from "@/components/Layout/ContextMenu";
import type { ContextMenuItem } from "@/components/Layout/ContextMenu";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type {
  GitCommit,
  GitDiffFile,
  GitRef,
  GitRefAction,
  GitRefActionRequest,
  GitWorkspace,
} from "@/api/types";
import { CommitDiff, timeAgo } from "./GitPanel";
import { GRAPH_PALETTE_SIZE, layoutCommitGraph, type GraphEdge, type GraphRow } from "./commitGraph";

const PAGE_SIZE = 200;
const ROW_HEIGHT = 28;
const GRAPH_LANE_WIDTH = 14;
const TABLE_MIN_WIDTH = 720;
const SCOPE_KEY = "ocode.ui.git-history-scope.v1";
const NOTICE_MS = 5000;

type HistoryScope = "all" | "current";

// Stroke and fill classes per palette slot. Full class names so Tailwind keeps them.
const GRAPH_COLORS: Array<{ stroke: string; fill: string }> = [
  { stroke: "stroke-blue-400", fill: "fill-blue-400" },
  { stroke: "stroke-orange-400", fill: "fill-orange-400" },
  { stroke: "stroke-green-400", fill: "fill-green-400" },
  { stroke: "stroke-purple-400", fill: "fill-purple-400" },
  { stroke: "stroke-pink-400", fill: "fill-pink-400" },
  { stroke: "stroke-teal-400", fill: "fill-teal-400" },
  { stroke: "stroke-yellow-400", fill: "fill-yellow-400" },
  { stroke: "stroke-red-400", fill: "fill-red-400" },
];

const REF_CHIP_CLASS: Record<GitRef["kind"], string> = {
  head: "border-blue-500/50 bg-blue-500/15 text-blue-300",
  remote: "border-orange-500/50 bg-orange-500/15 text-orange-300",
  tag: "border-green-500/50 bg-green-500/15 text-green-300",
};
const REF_CHIP_CURRENT_CLASS = "border-blue-400 bg-blue-500 text-white";

/** Remote HEAD pointers (origin/HEAD) are noise in the graph and the menu. */
function visibleRefs(refs: GitRef[]): GitRef[] {
  return refs.filter((r) => !(r.kind === "remote" && r.name.endsWith("/HEAD")));
}

function loadScope(): HistoryScope {
  try {
    return window.localStorage.getItem(SCOPE_KEY) === "current" ? "current" : "all";
  } catch {
    // intentionally not logged: localStorage is unavailable in private mode; the default scope applies.
    return "all";
  }
}

function saveScope(scope: HistoryScope) {
  try {
    window.localStorage.setItem(SCOPE_KEY, scope);
  } catch {
    // intentionally not logged: localStorage is unavailable in private mode; the choice lasts this session only.
  }
}

function messageOf(e: unknown, fallback: string): string {
  return e instanceof Error && e.message ? e.message : fallback;
}

function sameCommit(a: GitCommit, b: GitCommit): boolean {
  return (
    a.hash === b.hash &&
    a.message === b.message &&
    a.parents.join(" ") === b.parents.join(" ") &&
    JSON.stringify(a.refs) === JSON.stringify(b.refs)
  );
}

interface CommitList {
  commits: GitCommit[];
  hasMore: boolean;
}

/** Fold a freshly fetched first page into the list the panel already shows.
 *  Identical content returns the same object so nothing re-renders. When the
 *  head is unchanged, the commits loaded beyond the first page are kept. */
function mergeHead(prev: CommitList, page: GitCommit[]): CommitList {
  const headUnchanged =
    page.length > 0 &&
    prev.commits.length >= page.length &&
    page.every((c, i) => c.hash === prev.commits[i].hash);
  if (!headUnchanged) {
    return { commits: page, hasMore: page.length === PAGE_SIZE };
  }
  const tail = prev.commits.slice(page.length);
  const sameHead = page.every((c, i) => sameCommit(c, prev.commits[i]));
  if (sameHead) return prev;
  return {
    commits: [...page, ...tail],
    hasMore: tail.length > 0 ? prev.hasMore : page.length === PAGE_SIZE,
  };
}

type PendingConfirm =
  | { kind: "revert"; hash: string }
  | { kind: "reset-hard"; hash: string }
  | { kind: "branch-delete"; name: string };

interface NameDialogState {
  kind: "branch" | "tag";
  hash: string;
}

export interface GitHistoryPanelProps {
  projectPath?: string;
  projectHost?: string;
  /** False while the Git tab is hidden: nothing is fetched. */
  active: boolean;
  workspace: GitWorkspace | null;
  onWorkspaceChanged: (ws: GitWorkspace) => void;
  onShowChanges: () => void;
  /** GitPanel bumps this on each of its own refreshes so an outside commit or push shows up here. */
  refreshToken: number;
}

export default function GitHistoryPanel({
  projectPath,
  projectHost,
  active,
  workspace,
  onWorkspaceChanged,
  onShowChanges,
  refreshToken,
}: GitHistoryPanelProps) {
  const remote = Boolean(projectHost);
  const [scope, setScope] = useState<HistoryScope>(loadScope);
  const [list, setList] = useState<CommitList>({ commits: [], hasMore: false });
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  // Sticky action error (git's own text on a 409). Only the next action or the
  // dismiss button clears it, never a background refresh.
  const [actionError, setActionError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const noticeTimer = useRef<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [selectedHash, setSelectedHash] = useState<string | null>(null);
  const [detail, setDetail] = useState<GitDiffFile[] | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState<string | null>(null);
  const [menu, setMenu] = useState<{ hash: string; position: { x: number; y: number } } | null>(null);
  const [confirm, setConfirm] = useState<PendingConfirm | null>(null);
  const [nameDialog, setNameDialog] = useState<NameDialogState | null>(null);
  const [nameValue, setNameValue] = useState("");
  const [nameCheckout, setNameCheckout] = useState(true);
  const [nameError, setNameError] = useState<string | null>(null);
  // Bumped whenever the commit list is replaced, so a slow response for an
  // older scope, project or head cannot land on the current list.
  const generation = useRef(0);
  // Head fetches and load-more requests in flight. A background poll is skipped
  // while any runs, so a poll never supersedes the load the user is waiting on.
  const inFlight = useRef(0);
  const detailSeq = useRef(0);

  const { commits } = list;
  const rows = useMemo(() => layoutCommitGraph(commits), [commits]);
  const graphWidth = Math.max(1, ...rows.map((r) => r.activeLanes)) * GRAPH_LANE_WIDTH;
  const columns = `${graphWidth}px minmax(0,1fr) 140px 90px 80px`;
  const dirty = workspace ? workspace.staged.length + workspace.unstaged.length > 0 : false;
  const headName =
    commits.flatMap((c) => c.refs).find((r) => r.kind === "head" && r.current)?.name ??
    workspace?.status.branch ??
    "";
  // A detached HEAD reports a bare "HEAD" entry, which is a position, not a branch.
  const currentBranch = headName === "HEAD" ? "" : headName;

  const showNotice = useCallback((message: string) => {
    if (noticeTimer.current !== null) window.clearTimeout(noticeTimer.current);
    setNotice(message);
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

  /** Refetch the first page. `background` never shows a spinner or a load error. */
  const refreshHead = useCallback(
    async (background: boolean) => {
      if (background && inFlight.current > 0) return;
      const gen = ++generation.current;
      inFlight.current++;
      if (!background) {
        setLoading(true);
        setLoadError(null);
      }
      try {
        const page = await api.gitLog(
          projectPath,
          { limit: PAGE_SIZE, all: scope === "all" },
          projectHost,
        );
        if (gen !== generation.current) return;
        setList((prev) => mergeHead(prev, page));
        if (!background) setLoadError(null);
      } catch (e) {
        console.error("Git history: loading the commit log failed", { background, error: e });
        if (gen !== generation.current) return;
        if (!background) setLoadError(messageOf(e, "failed to load history"));
      } finally {
        inFlight.current--;
        if (gen === generation.current) setLoading(false);
      }
    },
    [projectPath, projectHost, scope],
  );

  // A new scope, project or host starts from an empty list, so no commit of the
  // old view is still shown while the new one loads.
  useEffect(() => {
    if (!active) return;
    generation.current++;
    setList({ commits: [], hasMore: false });
    setSelectedHash(null);
    setDetail(null);
    setDetailError(null);
    void refreshHead(false);
    // refreshHead changes with scope/project; `active` re-runs the load on show.
  }, [active, scope, projectPath, projectHost]);

  // GitPanel's own refresh (poll, git_status event, or its own action) bumps
  // refreshToken. Refetch only while shown; the first value is the mount load above.
  const seenToken = useRef(refreshToken);
  useEffect(() => {
    if (refreshToken === seenToken.current) return;
    seenToken.current = refreshToken;
    if (active) void refreshHead(true);
  }, [refreshToken, active, refreshHead]);

  const changeScope = (next: HistoryScope) => {
    saveScope(next);
    setScope(next);
  };

  const loadMore = async () => {
    const gen = generation.current;
    const skip = list.commits.length;
    setLoadingMore(true);
    inFlight.current++;
    try {
      const page = await api.gitLog(
        projectPath,
        { limit: PAGE_SIZE, skip, all: scope === "all" },
        projectHost,
      );
      if (gen !== generation.current) return;
      setList((prev) => ({
        commits: [...prev.commits, ...page],
        hasMore: page.length === PAGE_SIZE,
      }));
    } catch (e) {
      console.error("Git history: loading more commits failed", { skip, error: e });
      if (gen !== generation.current) return;
      setLoadError(messageOf(e, "failed to load more commits"));
    } finally {
      inFlight.current--;
      setLoadingMore(false);
    }
  };

  const selectCommit = async (hash: string) => {
    setSelectedHash(hash);
    setDetail(null);
    setDetailError(null);
    setDetailLoading(true);
    const seq = ++detailSeq.current;
    try {
      const files = await api.gitShow(hash, projectPath, projectHost);
      if (seq === detailSeq.current) setDetail(files);
    } catch (e) {
      console.error("Git history: loading the commit diff failed", { hash, error: e });
      if (seq === detailSeq.current) setDetailError(messageOf(e, "failed to load commit diff"));
    } finally {
      if (seq === detailSeq.current) setDetailLoading(false);
    }
  };

  /** Run a ref action. Errors propagate so the caller shows them where it sits. */
  const runRefAction = async (req: GitRefActionRequest, done: string): Promise<void> => {
    setBusy(true);
    setActionError(null);
    try {
      const res = await api.gitRefAction(req, projectPath, projectHost);
      onWorkspaceChanged(res.workspace);
      showNotice(done);
      void refreshHead(true);
    } catch (e) {
      console.error("Git history: ref action failed", { action: req.action, target: req.target, error: e });
      throw e;
    } finally {
      setBusy(false);
    }
  };

  const fromMenu = (action: GitRefAction, target: string, done: string, name?: string) => () => {
    void runRefAction({ action, target, name }, done).catch((e: unknown) =>
      setActionError(messageOf(e, "git action failed")),
    );
  };

  const copyText = (text: string, label: string) => {
    void copyTextToClipboard(text).then((ok) => {
      if (ok) showNotice(`Copied ${label}`);
      else setActionError(`Could not copy the ${label}`);
    });
  };

  const buildMenuItems = (c: GitCommit): ContextMenuItem[] => {
    // A mutating item is disabled while busy and always for a remote project.
    const mutate = (item: ContextMenuItem): ContextMenuItem => ({
      ...item,
      disabled: busy || remote,
    });
    const refs = visibleRefs(c.refs);
    const branchRefs = refs.filter((r) => r.kind === "head" && !r.current && r.name !== "HEAD");
    const remoteRefs = refs.filter((r) => r.kind === "remote");
    const localNames = new Set(
      commits.flatMap((x) => x.refs.filter((r) => r.kind === "head").map((r) => r.name)),
    );
    const cur = currentBranch || "HEAD";

    const note: ContextMenuItem[] = remote
      ? [{ label: "Not available for remote projects yet", onClick: () => {}, disabled: true }]
      : [];
    const checkout: ContextMenuItem[] = [
      ...branchRefs.map((r) =>
        mutate({
          label: `Checkout ${r.name}`,
          onClick: fromMenu("checkout", r.name, `Checked out ${r.name}`),
        }),
      ),
      // A remote-only branch: check it out as a new local branch that tracks it.
      ...remoteRefs
        .filter((r) => !localNames.has(r.name.slice(r.name.indexOf("/") + 1)))
        .map((r) => {
          const local = r.name.slice(r.name.indexOf("/") + 1);
          return mutate({
            label: `Checkout ${r.name} (track)`,
            onClick: fromMenu("branch-checkout", r.name, `Checked out ${local}`, local),
          });
        }),
      mutate({
        label: `Checkout commit ${c.short} (detached)`,
        onClick: fromMenu("checkout", c.hash, `Checked out ${c.short}`),
      }),
    ];
    const merge: ContextMenuItem[] = currentBranch
      ? [...branchRefs, ...remoteRefs].map((r) =>
          mutate({
            label: `Merge ${r.name} into ${currentBranch}`,
            onClick: fromMenu("merge", r.name, `Merged ${r.name}`),
          }),
        )
      : [];
    const replay: ContextMenuItem[] = [
      mutate({
        label: "Cherry-pick commit",
        onClick: fromMenu("cherry-pick", c.hash, `Cherry-picked ${c.short}`),
      }),
      mutate({
        label: "Revert commit…",
        onClick: () => setConfirm({ kind: "revert", hash: c.hash }),
      }),
    ];
    const reset: ContextMenuItem[] = [
      mutate({
        label: `Reset ${cur} to here (soft)`,
        onClick: fromMenu("reset-soft", c.hash, `Reset ${cur} to ${c.short} (soft)`),
      }),
      mutate({
        label: `Reset ${cur} to here (mixed)`,
        onClick: fromMenu("reset-mixed", c.hash, `Reset ${cur} to ${c.short} (mixed)`),
      }),
      mutate({
        label: `Reset ${cur} to here (hard)`,
        destructive: true,
        onClick: () => setConfirm({ kind: "reset-hard", hash: c.hash }),
      }),
    ];
    const create: ContextMenuItem[] = [
      mutate({
        label: "New branch here…",
        onClick: () => {
          setNameValue("");
          setNameCheckout(true);
          setNameError(null);
          setNameDialog({ kind: "branch", hash: c.hash });
        },
      }),
      mutate({
        label: "New tag here…",
        onClick: () => {
          setNameValue("");
          setNameError(null);
          setNameDialog({ kind: "tag", hash: c.hash });
        },
      }),
    ];
    const del: ContextMenuItem[] = branchRefs.map((r) =>
      mutate({
        label: `Delete branch ${r.name}`,
        destructive: true,
        onClick: () => setConfirm({ kind: "branch-delete", name: r.name }),
      }),
    );
    const copy: ContextMenuItem[] = [
      { label: "Copy SHA", onClick: () => copyText(c.hash, "SHA") },
      { label: "Copy message", onClick: () => copyText(c.message, "message") },
    ];

    const groups = [note, checkout, merge, replay, reset, create, del, copy].filter(
      (g) => g.length > 0,
    );
    const separator: ContextMenuItem = { label: "", onClick: () => {}, separator: true };
    return groups.flatMap((g, i) => (i === 0 ? g : [separator, ...g]));
  };

  const submitName = async () => {
    if (!nameDialog || busy) return;
    const name = nameValue.trim();
    if (!name) return;
    const isTag = nameDialog.kind === "tag";
    const action: GitRefAction = isTag
      ? "tag"
      : nameCheckout
        ? "branch-checkout"
        : "branch";
    setNameError(null);
    try {
      await runRefAction(
        { action, target: nameDialog.hash, name },
        isTag ? `Created tag ${name}` : `Created branch ${name}`,
      );
      setNameDialog(null);
    } catch (e) {
      setNameError(messageOf(e, "git action failed"));
    }
  };

  const confirmTitle = (p: PendingConfirm) => {
    if (p.kind === "revert") return `Revert ${commits.find((c) => c.hash === p.hash)?.short ?? p.hash.slice(0, 7)}?`;
    if (p.kind === "reset-hard") return `Reset ${currentBranch || "HEAD"} to ${commits.find((c) => c.hash === p.hash)?.short ?? p.hash.slice(0, 7)} (hard)?`;
    return `Delete branch ${p.name}?`;
  };

  const runConfirm = async (p: PendingConfirm) => {
    if (p.kind === "revert") {
      await runRefAction({ action: "revert", target: p.hash }, "Reverted commit");
    } else if (p.kind === "reset-hard") {
      await runRefAction({ action: "reset-hard", target: p.hash }, "Reset (hard)");
    } else {
      await runRefAction({ action: "branch-delete", target: p.name }, `Deleted branch ${p.name}`);
    }
    setConfirm(null);
  };

  const selected = selectedHash ? commits.find((c) => c.hash === selectedHash) ?? null : null;
  const menuCommit = menu ? commits.find((c) => c.hash === menu.hash) ?? null : null;

  return (
    <div data-testid="git-history" className="flex flex-1 min-h-0 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-1.5">
        <Select value={scope} onValueChange={(v) => changeScope(v as HistoryScope)}>
          <SelectTrigger aria-label="Branch scope" className="h-7 w-40 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All branches</SelectItem>
            <SelectItem value="current">Current branch</SelectItem>
          </SelectContent>
        </Select>
        <button
          type="button"
          onClick={() => void refreshHead(false)}
          disabled={loading}
          aria-label="Refresh history"
          title="Refresh history"
          className="p-1 rounded hover:bg-muted/60 text-muted-foreground hover:text-foreground disabled:opacity-50"
        >
          <RefreshCw className={cn("w-3.5 h-3.5", loading && "animate-spin")} />
        </button>
        <span className="text-xs text-muted-foreground" data-testid="git-history-count">
          {commits.length} commit{commits.length === 1 ? "" : "s"}
          {list.hasMore ? "+" : ""}
        </span>
      </div>

      {actionError && (
        <div
          role="alert"
          data-testid="git-history-error"
          className="flex items-start gap-2 border-b border-border bg-red-500/10 px-3 py-1.5 text-xs text-red-400"
        >
          <span className="min-w-0 flex-1 whitespace-pre-wrap break-words">{actionError}</span>
          <button
            type="button"
            onClick={() => setActionError(null)}
            aria-label="Dismiss error"
            className="shrink-0 rounded p-0.5 hover:bg-red-500/20"
          >
            <X className="w-3 h-3" />
          </button>
        </div>
      )}
      {loadError && (
        <div role="alert" className="border-b border-border bg-red-500/10 px-3 py-1.5 text-xs text-red-400">
          {loadError}
        </div>
      )}
      {notice && !actionError && (
        <div role="status" data-testid="git-history-notice" className="border-b border-border bg-green-500/10 px-3 py-1.5 text-xs text-green-400">
          {notice}
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-auto">
        {loading && commits.length === 0 ? (
          <div className="px-3 py-6 text-center text-sm text-muted-foreground">Loading history…</div>
        ) : (
          <div role="table" aria-label="Commit history" style={{ minWidth: TABLE_MIN_WIDTH }}>
            <div
              role="row"
              className="sticky top-0 z-10 grid items-center border-b border-border bg-background text-xs text-muted-foreground"
              style={{ gridTemplateColumns: columns, height: ROW_HEIGHT }}
            >
              <div role="columnheader" className="pl-2">Graph</div>
              <div role="columnheader">Description</div>
              <div role="columnheader">Author</div>
              <div role="columnheader">Date</div>
              <div role="columnheader">Commit</div>
            </div>

            {dirty && (
              <div
                role="row"
                data-testid="git-history-uncommitted"
                onClick={onShowChanges}
                className="grid cursor-pointer items-center text-xs italic hover:bg-muted/40"
                style={{ gridTemplateColumns: columns, height: ROW_HEIGHT }}
              >
                <div role="cell" className="h-full">
                  <svg width={graphWidth} height={ROW_HEIGHT} aria-hidden="true">
                    <line
                      x1={GRAPH_LANE_WIDTH / 2}
                      y1={ROW_HEIGHT / 2}
                      x2={GRAPH_LANE_WIDTH / 2}
                      y2={ROW_HEIGHT}
                      strokeDasharray="2 2"
                      strokeWidth={2}
                      className="stroke-muted-foreground"
                    />
                    <circle
                      cx={GRAPH_LANE_WIDTH / 2}
                      cy={ROW_HEIGHT / 2}
                      r={4}
                      strokeWidth={2}
                      strokeDasharray="2 2"
                      className="fill-background stroke-muted-foreground"
                    />
                  </svg>
                </div>
                <div role="cell" className="truncate pr-2 text-muted-foreground">Uncommitted changes</div>
                <div role="cell" />
                <div role="cell" />
                <div role="cell" className="font-mono text-muted-foreground">*</div>
              </div>
            )}

            {commits.map((c, i) => (
              <CommitRow
                key={c.hash}
                commit={c}
                row={rows[i]}
                graphWidth={graphWidth}
                columns={columns}
                selected={c.hash === selectedHash}
                onSelect={() => void selectCommit(c.hash)}
                onContextMenu={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  setMenu({ hash: c.hash, position: { x: e.clientX, y: e.clientY } });
                }}
              />
            ))}

            {!loading && commits.length === 0 && (
              <div className="px-3 py-6 text-center text-sm text-muted-foreground">No commits</div>
            )}
            {list.hasMore && (
              <div className="p-2">
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full"
                  data-testid="git-history-load-more"
                  disabled={loadingMore}
                  onClick={() => void loadMore()}
                >
                  {loadingMore ? "Loading…" : "Load more"}
                </Button>
              </div>
            )}
          </div>
        )}
      </div>

      {selectedHash && (
        <div data-testid="git-history-detail" className="flex h-[40%] min-h-0 shrink-0 flex-col border-t border-border">
          {selected && (
            <div className="space-y-0.5 border-b border-border px-3 py-2 text-xs">
              <div className="break-all font-mono">{selected.hash}</div>
              <div className="text-muted-foreground">
                {selected.author} &lt;{selected.email}&gt;
              </div>
              <div className="text-muted-foreground">{new Date(selected.date).toLocaleString()}</div>
              <pre className="whitespace-pre-wrap break-words font-sans text-sm">{selected.message}</pre>
            </div>
          )}
          <div className="min-h-0 flex-1 overflow-auto">
            {detailLoading && <div className="p-3 text-sm text-muted-foreground">Loading commit diff…</div>}
            {detailError && <div role="alert" className="p-3 text-xs text-red-400">{detailError}</div>}
            {!detailLoading && !detailError && detail && <CommitDiff files={detail} />}
          </div>
        </div>
      )}

      {menu && menuCommit && (
        <ContextMenu
          items={buildMenuItems(menuCommit)}
          open
          position={menu.position}
          onClose={() => setMenu(null)}
        />
      )}

      {confirm && (
        <ConfirmDialog
          open
          title={confirmTitle(confirm)}
          description={
            confirm.kind === "revert"
              ? "Creates a new commit that undoes this commit's changes."
              : confirm.kind === "reset-hard"
                ? "Discards uncommitted changes and moves the branch back, dropping every commit after this one. Git cannot undo this from the panel."
                : `Deletes the local branch ${confirm.name}.`
          }
          confirmLabel={confirm.kind === "revert" ? "Revert" : confirm.kind === "reset-hard" ? "Reset (hard)" : "Delete branch"}
          pendingLabel="Working…"
          confirmVariant={confirm.kind === "revert" ? "default" : "destructive"}
          onConfirm={() => runConfirm(confirm)}
          onCancel={() => setConfirm(null)}
        />
      )}

      <Dialog open={nameDialog !== null} onOpenChange={(open) => !open && setNameDialog(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              {nameDialog?.kind === "tag" ? <Tag className="w-4 h-4 text-green-400" /> : <GitBranch className="w-4 h-4 text-blue-400" />}
              {nameDialog?.kind === "tag" ? "New tag here" : "New branch here"}
            </DialogTitle>
          </DialogHeader>
          <div className="mt-2 space-y-3">
            <Input
              autoFocus
              value={nameValue}
              onChange={(e) => setNameValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") void submitName();
              }}
              placeholder={nameDialog?.kind === "tag" ? "Tag name" : "Branch name"}
              aria-label={nameDialog?.kind === "tag" ? "Tag name" : "Branch name"}
            />
            {nameDialog?.kind === "branch" && (
              <label className="flex cursor-pointer select-none items-center gap-2 text-sm text-muted-foreground">
                <input
                  type="checkbox"
                  checked={nameCheckout}
                  onChange={(e) => setNameCheckout(e.target.checked)}
                />
                Check out
              </label>
            )}
            {nameError && (
              <div role="alert" className="whitespace-pre-wrap break-words text-xs text-red-400">
                {nameError}
              </div>
            )}
          </div>
          <DialogFooter className="mt-4">
            <Button variant="outline" onClick={() => setNameDialog(null)}>
              Cancel
            </Button>
            <Button onClick={() => void submitName()} disabled={busy || !nameValue.trim()}>
              Create
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

interface CommitRowProps {
  commit: GitCommit;
  row: GraphRow;
  graphWidth: number;
  columns: string;
  selected: boolean;
  onSelect: () => void;
  onContextMenu: (e: React.MouseEvent) => void;
}

function CommitRow({ commit, row, graphWidth, columns, selected, onSelect, onContextMenu }: CommitRowProps) {
  const refs = visibleRefs(commit.refs);
  return (
    <div
      role="row"
      data-testid="git-history-row"
      data-hash={commit.hash}
      aria-selected={selected}
      onClick={onSelect}
      onContextMenu={onContextMenu}
      className={cn(
        "grid cursor-default items-center text-xs hover:bg-muted/40",
        selected && "bg-muted/60",
      )}
      style={{ gridTemplateColumns: columns, height: ROW_HEIGHT }}
    >
      <div role="cell" className="h-full">
        <GraphCell row={row} width={graphWidth} isMerge={commit.parents.length > 1} />
      </div>
      <div role="cell" className="flex min-w-0 items-center gap-1.5 pr-2">
        {refs.map((r) => (
          <span
            key={`${r.kind}:${r.name}`}
            data-testid="git-ref-chip"
            data-kind={r.kind}
            className={cn(
              "inline-flex shrink-0 items-center gap-0.5 rounded border px-1 text-[10px] font-medium leading-4",
              r.kind === "head" && r.current ? REF_CHIP_CURRENT_CLASS : REF_CHIP_CLASS[r.kind],
            )}
          >
            {r.kind === "tag" ? <Tag className="w-2.5 h-2.5" /> : <GitBranch className="w-2.5 h-2.5" />}
            {r.name}
          </span>
        ))}
        <span className="truncate">{commit.message}</span>
      </div>
      <div role="cell" className="truncate pr-2 text-muted-foreground">{commit.author}</div>
      <div role="cell" className="truncate pr-2 text-muted-foreground" title={new Date(commit.date).toLocaleString()}>
        {timeAgo(commit.date)}
      </div>
      <div role="cell" className="font-mono text-muted-foreground">{commit.short}</div>
    </div>
  );
}

function GraphCell({ row, width, isMerge }: { row: GraphRow; width: number; isMerge: boolean }) {
  const laneX = (lane: number) => lane * GRAPH_LANE_WIDTH + GRAPH_LANE_WIDTH / 2;
  const mid = ROW_HEIGHT / 2;
  const node = GRAPH_COLORS[row.color % GRAPH_PALETTE_SIZE];
  const extent = (e: GraphEdge): [number, number] => {
    if (e.kind === "incoming") return [0, mid];
    if (e.kind === "parent") return [mid, ROW_HEIGHT];
    return [0, ROW_HEIGHT];
  };
  return (
    <svg width={width} height={ROW_HEIGHT} className="block" aria-hidden="true">
      {row.edges.map((e, i) => {
        const [y1, y2] = extent(e);
        const x1 = laneX(e.fromLane);
        const x2 = laneX(e.toLane);
        const stroke = GRAPH_COLORS[e.color % GRAPH_PALETTE_SIZE].stroke;
        return x1 === x2 ? (
          <line key={i} x1={x1} y1={y1} x2={x2} y2={y2} strokeWidth={2} className={stroke} />
        ) : (
          <path
            key={i}
            d={`M${x1} ${y1} C${x1} ${(y1 + y2) / 2} ${x2} ${(y1 + y2) / 2} ${x2} ${y2}`}
            fill="none"
            strokeWidth={2}
            className={stroke}
          />
        );
      })}
      <circle
        cx={laneX(row.lane)}
        cy={mid}
        r={4}
        strokeWidth={2}
        className={isMerge ? cn("fill-background", node.stroke) : cn(node.fill, node.stroke)}
      />
    </svg>
  );
}
