import { useCallback, useEffect, useRef, useState } from "react";
import {
  RefreshCw,
  GitBranch,
  ArrowDownToLine,
  ArrowUpToLine,
  Trash2,
  ExternalLink,
  GitCommitVertical,
  ChevronDown,
  ChevronRight,
  AlertTriangle,
  Archive,
  Check,
  PanelLeft,
  PanelLeftClose,
} from "lucide-react";
import { api } from "@/api/client";
import { eventBus } from "@/lib/eventBus";
import { cn } from "@/lib/utils";
import { useResizableSidebar } from "@/hooks/useResizableSidebar";
import { useIsMobile } from "@/hooks/useIsMobile";
import { useKeyedLoad, type LoadingEventHandler } from "@/hooks/useKeyedLoad";
import { ContextMenu } from "@/components/Layout/ContextMenu";
import type { ContextMenuItem } from "@/components/Layout/ContextMenu";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import type {
  GitCommit,
  GitDiffFile,
  GitHunkAction,
  GitOperation,
  GitStash,
  GitWorkspace,
} from "@/api/types";

const REFRESH_INTERVAL = 10000;

// Expanded/collapsed state of the Git panel's three left-side sections.
// Persisted to localStorage (versioned key, same pattern as CoworkSidebar's
// "ocode.ui.sidebar.v2") so the layout survives reloads.
const GIT_PANEL_SECTIONS_KEY = "ocode.ui.git-panel.v1";

interface PanelSections {
  staged: boolean;
  unstaged: boolean;
  commits: boolean;
  stashes: boolean;
}

const DEFAULT_SECTIONS: PanelSections = {
  staged: true,
  unstaged: true,
  commits: true,
  stashes: true,
};

/** Height of the file-list pane when the panel is stacked on narrow viewports. */
const MOBILE_FILE_PANE_HEIGHT = "45%";

function loadPanelSections(): PanelSections {
  try {
    const raw = window.localStorage.getItem(GIT_PANEL_SECTIONS_KEY);
    if (!raw) return { ...DEFAULT_SECTIONS };
    const parsed = JSON.parse(raw) as Partial<PanelSections>;
    return {
      staged: typeof parsed.staged === "boolean" ? parsed.staged : true,
      unstaged: typeof parsed.unstaged === "boolean" ? parsed.unstaged : true,
      commits: typeof parsed.commits === "boolean" ? parsed.commits : true,
      stashes: typeof parsed.stashes === "boolean" ? parsed.stashes : true,
    };
  } catch {
    return { ...DEFAULT_SECTIONS };
  }
}

const STATUS_BADGES: Record<string, { label: string; color: string }> = {
  modified: { label: "M", color: "bg-yellow-500/20 text-yellow-400" },
  added: { label: "A", color: "bg-green-500/20 text-green-400" },
  deleted: { label: "D", color: "bg-red-500/20 text-red-400" },
  renamed: { label: "R", color: "bg-blue-500/20 text-blue-400" },
  untracked: { label: "?", color: "bg-muted/20 text-muted-foreground" },
  conflicted: { label: "!", color: "bg-red-500/20 text-red-400" },
};

/** Which actions are valid for an operation kind.
 *
 *  This MUST match the server's `gitOperationCommand` table exactly. Showing a
 *  button the server will refuse (a merge has no "skip") or hiding one it
 *  accepts (a bisect advances via good/bad, never "continue") makes the panel
 *  lie about what the user can do. A merge deliberately has no skip: skipping
 *  has no meaning when there is no sequence of commits to step through.
 *
 *  Typed over GitOperation["kind"] rather than `Record<string, ...>` so the
 *  table is EXHAUSTIVE: adding a kind to the server's vocabulary and forgetting
 *  this file is a compile error, not a runtime banner with no buttons. The
 *  runtime `hasOwnProperty` branch below covers the one case a type cannot —
 *  a server NEWER than this bundle. */
const OPERATION_ACTIONS: Record<
  GitOperation["kind"],
  { action: string; label: string; destructive?: boolean }[]
> = {
  merge: [
    { action: "continue", label: "Continue" },
    { action: "abort", label: "Abort", destructive: true },
  ],
  rebase: [
    { action: "continue", label: "Continue" },
    { action: "abort", label: "Abort", destructive: true },
    { action: "skip", label: "Skip" },
  ],
  "rebase-interactive": [
    { action: "continue", label: "Continue" },
    { action: "abort", label: "Abort", destructive: true },
    { action: "skip", label: "Skip" },
  ],
  am: [
    { action: "continue", label: "Continue" },
    { action: "abort", label: "Abort", destructive: true },
    { action: "skip", label: "Skip" },
  ],
  "cherry-pick": [
    { action: "continue", label: "Continue" },
    { action: "abort", label: "Abort", destructive: true },
    { action: "skip", label: "Skip" },
  ],
  revert: [
    { action: "continue", label: "Continue" },
    { action: "abort", label: "Abort", destructive: true },
    { action: "skip", label: "Skip" },
  ],
  // A bisect advances by classifying commits; there is no "continue". The
  // button is LABELLED "Reset" because that is what git calls it, but the wire
  // action is "abort" — the server's table maps that to `git bisect reset`.
  // Sending "reset" here was a real 400 (caught 2026-09-25).
  bisect: [
    { action: "good", label: "Good" },
    { action: "bad", label: "Bad" },
    { action: "skip", label: "Skip" },
    { action: "abort", label: "Reset", destructive: true },
  ],
};

/** Labels for the two conflict-side buttons.
 *
 *  THE most consequential detail in this feature. During a rebase git's
 *  "ours" is the UPSTREAM branch being replayed onto and "theirs" is the
 *  user's own commit — the exact opposite of the intuitive reading. A user
 *  who reads "Use ours" as "keep my work" would silently discard their commit
 *  while believing they had kept it. So the wording changes, and the tooltip
 *  names the side explicitly.
 *
 *  Only a rebase inverts. `am` and `cherry-pick` do NOT: verified in a scratch
 *  repo with `git am -3`, stage 2 ("ours") is the current HEAD and stage 3
 *  ("theirs") is the incoming patch. Labelling am's sides as upstream/commit
 *  pointed the user at the wrong side of their own history. */
function conflictSideLabels(operationKind?: string) {
  if (operationKind === "rebase" || operationKind === "rebase-interactive") {
    return {
      ours: "Keep upstream",
      theirs: "Keep my commit",
      oursTip: "Keep the upstream version (during a rebase, git calls this 'ours')",
      theirsTip: "Keep your own commit (during a rebase, git calls this 'theirs')",
    };
  }
  return {
    ours: "Use ours",
    theirs: "Use theirs",
    oursTip: "Keep our version of this file",
    theirsTip: "Keep their version of this file",
  };
}

interface Props {
  onOpenFile?: (path: string, projectRoot?: string) => void;
  projectPath?: string;
  /** Registered remote target (SSH/WSL) when this is a remote project;
   *  empty for local projects. Routed through to every git API call. */
  projectHost?: string;
  /** True while the Git view is frontmost. The panel is force-mounted so its
   *  DOM survives view switches; without this gate it polls and refetches the
   *  whole workspace every 10s forever, even while the user is chatting. */
  active?: boolean;
  loadingKey?: string;
  onLoadingEvent?: LoadingEventHandler;
}

type Selection =
  | { kind: "file"; path: string; staged: boolean }
  | { kind: "commit"; hash: string }
  | { kind: "stash"; index: number }
  | null;

/** The two working-tree panes a file row can live in. */
type PaneKey = "staged" | "unstaged";

/** Pane-qualified key for the multi-select set (a path can be in both panes). */
const paneKeyOf = (pane: PaneKey, path: string) => `${pane}:${path}`;

/** Splits a unified patch into per-hunk blocks (each starting at its `@@`
 *  line; the diff preamble is dropped — the header row shows the file). */
function splitHunks(patch: string): string[][] {
  const blocks: string[][] = [];
  let cur: string[] | null = null;
  for (const line of patch.split("\n")) {
    if (line.startsWith("@@")) {
      if (cur) blocks.push(cur);
      cur = [line];
    } else if (cur) {
      cur.push(line);
    }
  }
  if (cur) blocks.push(cur);
  return blocks;
}

function lineColor(line: string): string {
  if (line.startsWith("+") && !line.startsWith("+++")) return "text-green-400";
  if (line.startsWith("-") && !line.startsWith("---")) return "text-red-400";
  if (line.startsWith("@@")) return "text-blue-400";
  return "text-muted-foreground";
}

/** Human-facing label for a stash reflog subject. %gs is either
 *  "WIP on <branch>: <base subject>" or "On <branch>: <message>"; the part
 *  after the first ": " is what the user actually cares about. */
function stashLabel(subject: string): string {
  const idx = subject.indexOf(": ");
  return idx >= 0 ? subject.slice(idx + 2) : subject;
}

function timeAgo(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "";
  const diff = Date.now() - then;
  const min = Math.floor(diff / 60_000);
  if (min < 1) return "just now";
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const day = Math.floor(hr / 24);
  if (day < 30) return `${day}d ago`;
  return new Date(iso).toLocaleDateString();
}

export default function GitPanel({
  onOpenFile,
  projectPath,
  projectHost,
  active = true,
  loadingKey,
  onLoadingEvent,
}: Props) {
  const runKeyedLoad = useKeyedLoad(loadingKey, onLoadingEvent);
  const [workspace, setWorkspace] = useState<GitWorkspace | null>(null);
  const [commits, setCommits] = useState<GitCommit[]>([]);
  const [commitDiff, setCommitDiff] = useState<GitDiffFile[] | null>(null);
  const [stashes, setStashes] = useState<GitStash[]>([]);
  const [stashFiles, setStashFiles] = useState<GitDiffFile[] | null>(null);
  // Paths ticked in the open stash's file list (multi-file restore).
  const [checkedStashFiles, setCheckedStashFiles] = useState<string[]>([]);
  // Multi-select over the two working-tree file lists (the staged/unstaged
  // panes). Keys are pane-qualified — `staged:<path>` / `unstaged:<path>` —
  // because a partially staged file legitimately appears in both panes, and
  // bulk actions must only touch the pane(s) the user picked in.
  // `lastPickedKey` is the shift-click anchor.
  const [pickedPaths, setPickedPaths] = useState<Set<string>>(new Set());
  const [lastPickedKey, setLastPickedKey] = useState<string | null>(null);
  // "Stash changes" dialog: optional message + include-untracked toggle.
  // `stashTargets` is null for "stash everything", or an explicit pathspec list
  // when the dialog is opened from a file row / a multi-selection.
  const [stashDialog, setStashDialog] = useState(false);
  const [stashTargets, setStashTargets] = useState<string[] | null>(null);
  const [stashMessage, setStashMessage] = useState("");
  const [stashIncludeUntracked, setStashIncludeUntracked] = useState(true);
  // Confirmation dialogs for destructive / overwriting stash actions.
  const [pendingDeleteStash, setPendingDeleteStash] = useState<GitStash | null>(null);
  const [pendingRestoreStash, setPendingRestoreStash] = useState<{
    index: number;
    paths: string[];
    overwrites: string[];
  } | null>(null);
  // Snapshot of paths awaiting the destructive discard/delete confirmation.
  // Native window.confirm() silently returns false in the Wails/WKWebView
  // desktop webview, so the flow must use a rendered dialog (same rule as
  // FileTree's ConfirmDeleteDialog).
  const [pendingDiscard, setPendingDiscard] = useState<
    { path: string; untracked: boolean }[] | null
  >(null);
  const [selection, setSelection] = useState<Selection>(null);
  const [commitMessage, setCommitMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Whether the previous load reported an operation in progress. Used only to
  // detect the idle -> operation edge; a ref because the comparison drives a
  // side effect (clearing the now-stale pull error).
  const wasInOperationRef = useRef(false);
  // Transient success notice ("pushed", "committed", …). Auto-clears after 5s,
  // mirroring the TUI's status-bar toast.
  const [notice, setNotice] = useState<string | null>(null);
  const noticeTimer = useRef<number | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [sections, setSections] = useState<PanelSections>(loadPanelSections);
  const [fileFilter, setFileFilter] = useState("");
  const [contextMenu, setContextMenu] = useState<{
    items: ContextMenuItem[];
    position: { x: number; y: number };
  } | null>(null);

  // Left file-list / commits column width. Drag-to-resize, persisted to
  // localStorage so the split survives a reload (same hook as the app sidebar
  // and file tree). Replaces the old fixed `w-72 md:w-80` column.
  const filePane = useResizableSidebar({
    storageKey: "ocode.ui.git-panel.width",
    defaultWidth: 288,
    minWidth: 180,
    maxWidth: 520,
    // Adds a header toggle that collapses the pane to zero and persists it at
    // `ocode.ui.git-panel.width.collapsed`.
    collapsible: true,
  });
  // Below the mobile breakpoint the panel stacks the file list above the diff
  // instead of squeezing a two-column split.
  const isMobile = useIsMobile();

  // showNotice displays a transient success message for 5 seconds. Any prior
  // timer is cleared so rapid actions don't leave a stale message on screen.
  const showNotice = useCallback((message: string) => {
    if (noticeTimer.current !== null) window.clearTimeout(noticeTimer.current);
    setNotice(message);
    noticeTimer.current = window.setTimeout(() => {
      setNotice(null);
      noticeTimer.current = null;
    }, 5000);
  }, []);

  // Clear any pending notice timer on unmount so it can't fire against an
  // unmounted component.
  useEffect(() => {
    return () => {
      if (noticeTimer.current !== null) window.clearTimeout(noticeTimer.current);
    };
  }, []);

  // load refetches the workspace. `background` is true for the periodic poll
  // and eventBus-driven refreshes: those must NOT clear an error or notice
  // left by an explicit user action, otherwise a failed push would vanish
  // within 10 seconds ("git push error disappears by itself").
  const load = useCallback((opts?: { background?: boolean }) => {
    const background = opts?.background ?? false;
    if (!background) setError(null);
    setRefreshing(true);
    return runKeyedLoad(async () => {
      const [ws, log, stashList] = await Promise.all([
        api.getGitWorkspace(projectPath, projectHost),
        api.gitLog(projectPath, 50, projectHost),
        // A failed stash-list probe (older server, transient error) must not
        // take down the whole workspace refresh.
        api.gitStashList(projectPath, projectHost).catch(() => [] as GitStash[]),
      ]);
      return { ws, log, stashList };
    }, {
      retry: () => {
        void load(opts);
      },
    }).then((result) => {
      if (result.status === "success" || result.status === "empty") {
        const { ws, log, stashList } = result.value;
        setWorkspace(ws);
        setCommits(log);
        setStashes(stashList);
        if (!background) setError(null);
        // A pull that stopped on a conflict leaves a sticky error reading
        // "git pull failed: CONFLICT ...". Once the operation banner is up,
        // that message is stale and actively misleading — the banner IS the
        // explanation now.
        //
        // This is deliberately the NARROW transition idle -> operation, and it
        // applies to background loads too. Nothing else may clear an error
        // here: a failed push must stay visible across polls, which is the
        // whole point of the sticky-error contract.
        //
        // A ref, not a state updater: the comparison is a side effect, and a
        // state updater runs during the render phase (twice under StrictMode).
        const nowInOperation = !!ws.status.operation;
        if (!wasInOperationRef.current && nowInOperation) setError(null);
        wasInOperationRef.current = nowInOperation;
        // Selection may no longer exist after a mutation/drop: re-resolve it
        // against the fresh snapshots. A selected file can move between panes;
        // a selected stash can be dropped (possibly by index shift).
        setSelection((sel) => {
          if (!sel) return sel;
          if (sel.kind === "commit") return sel;
          if (sel.kind === "stash") {
            return stashList.some((s) => s.index === sel.index) ? sel : null;
          }
          const inStaged = ws.staged.some((f) => f.path === sel.path);
          const inUnstaged = ws.unstaged.some((f) => f.path === sel.path);
          if (!inStaged && !inUnstaged) return null;
          return {
            kind: "file",
            path: sel.path,
            staged: sel.staged ? inStaged : !inUnstaged ? inStaged : false,
          };
        });
      } else if (result.status === "error") {
        // A background refresh failing is not the user's action — don't let a
        // transient poll error overwrite the outcome of what they just did.
        console.error("Git workspace load failed:", result.error);
        if (!background) setError(result.message);
      }
      if (result.status !== "stale" && result.status !== "aborted") setRefreshing(false);
    });
  }, [projectPath, projectHost, runKeyedLoad]);

  useEffect(() => {
    load();
    if (!active) return;
    const interval = setInterval(() => load({ background: true }), REFRESH_INTERVAL);
    return () => clearInterval(interval);
  }, [load, active]);

  // Persist the section expanded/collapsed layout across reloads.
  useEffect(() => {
    try {
      window.localStorage.setItem(GIT_PANEL_SECTIONS_KEY, JSON.stringify(sections));
    } catch {
      // localStorage unavailable (private mode etc.) — collapse still works
      // for the current session.
    }
  }, [sections]);

  const toggleSection = (key: keyof PanelSections) =>
    setSections((s) => ({ ...s, [key]: !s[key] }));

  // The server pushes git_status bus events whenever the repo changes (also
  // after the TUI or the file-tree context menu mutate it) — stay fresh.
  // These are background refreshes so they never clear a user-visible error.
  useEffect(() => {
    return eventBus.on("git_status", (env) => {
      if (!projectPath || env.project === projectPath) load({ background: true });
    });
  }, [load, projectPath, projectHost]);

  // Drop picked paths that no longer exist (staged/stashed/discarded elsewhere),
  // so a stale selection can't target a file that has already left the repo
  // state — and so the section header's "N selected" count stays honest.
  useEffect(() => {
    if (!workspace) return;
    const valid = new Set<string>();
    for (const f of workspace.staged ?? []) valid.add(paneKeyOf("staged", f.path));
    for (const f of workspace.unstaged ?? []) valid.add(paneKeyOf("unstaged", f.path));
    setPickedPaths((prev) => {
      if (prev.size === 0) return prev;
      let changed = false;
      const next = new Set<string>();
      for (const k of prev) {
        if (valid.has(k)) next.add(k);
        else changed = true;
      }
      return changed ? next : prev;
    });
  }, [workspace]);

  const runMutation = useCallback(
    async (fn: () => Promise<unknown>, successMessage?: string) => {
      setBusy(true);
      setError(null);
      try {
        await fn();
        if (successMessage) showNotice(successMessage);
        await load();
      } catch (e) {
        setError(e instanceof Error ? e.message : "git action failed");
      } finally {
        setBusy(false);
      }
    },
    [load, showNotice],
  );

  // Conflict resolution + operation recovery. Both go through runMutation so
  // they share the panel's ONE error/notice contract: a failed action's error
  // stays until the next user action, and a background reload never clears it.
  const resolveConflict = (path: string, resolution: "ours" | "theirs" | "mark") =>
    runMutation(
      () => api.gitResolveConflict({ path, resolution }, projectPath, projectHost),
      resolution === "mark" ? "marked " + path + " resolved" : "resolved " + path,
    );

  const runOperation = (action: string, kind: string) =>
    runMutation(async () => {
      // The server returns the git command's combined output; commit hooks
      // run during continue/skip, so that output is shown rather than dropped.
      const res = (await api.gitOperation(
        { action: action as "continue", kind: kind as "rebase" },
        projectPath,
        projectHost,
      )) as { output?: string } | undefined;
      const out = res?.output?.trim();
      if (out) showNotice(out);
    }, `${action}d`);

  // Destructive operation actions (abort, and a bisect's reset) discard the
  // in-progress work, so they go through the same confirmation-dialog pattern
  // as the existing discard/force-push flows rather than firing directly.
  const [pendingOperation, setPendingOperation] = useState<{ action: string; kind: string; label: string } | null>(null);
  const requestOperation = (action: string, kind: string, destructive: boolean, label: string) => {
    if (destructive) setPendingOperation({ action, kind, label });
    else void runOperation(action, kind);
  };
  const doPendingOperation = () => {
    if (!pendingOperation) return;
    const p = pendingOperation;
    setPendingOperation(null);
    void runOperation(p.action, p.kind);
  };

  const stageFile = (path: string) =>
    runMutation(() => api.gitStage([path], projectPath, projectHost), "staged " + path);
  const unstageFile = (path: string) =>
    runMutation(() => api.gitUnstage([path], projectPath, projectHost), "unstaged " + path);
  const discardTargets = (targets: { path: string; untracked: boolean }[]) => {
    const tracked = targets.filter((t) => !t.untracked).map((t) => t.path);
    const untracked = targets.filter((t) => t.untracked).map((t) => t.path);
    const describe =
      targets.length === 1
        ? (targets[0].untracked ? "deleted " : "discarded ") + targets[0].path
        : targets.every((t) => t.untracked)
          ? `deleted ${targets.length} files`
          : `discarded ${targets.length} files`;
    return runMutation(async () => {
      if (tracked.length > 0) await api.gitDiscard(tracked, projectPath, projectHost);
      for (const path of untracked) {
        await api.gitHunk(
          { path, hunk_index: 0, action: "discard", staged: false },
          projectPath,
          projectHost,
        );
      }
    }, describe);
  };
  // Gate the destructive discard/delete behind a rendered confirmation dialog
  // (native window.confirm() is a no-op in the desktop webview).
  const requestDiscard = useCallback(
    (targets: { path: string; untracked: boolean }[]) => {
      if (targets.length === 0) return;
      setPendingDiscard(targets);
    },
    [],
  );
  const doDiscard = () => {
    if (!pendingDiscard) return;
    const targets = pendingDiscard;
    setPendingDiscard(null);
    void discardTargets(targets);
  };
  const discardFile = (path: string, untracked: boolean) =>
    requestDiscard([{ path, untracked }]);
  const stageAll = (paths: string[]) =>
    runMutation(() => api.gitStage(paths, projectPath, projectHost), `staged ${paths.length} file(s)`);
  const unstageAll = (paths: string[]) =>
    runMutation(() => api.gitUnstage(paths, projectPath, projectHost), `unstaged ${paths.length} file(s)`);

  // Network actions. Each surfaces a 5-second success notice on completion,
  // mirroring the TUI's status-bar toast.
  const doFetch = () =>
    runMutation(() => api.gitFetch(projectPath, projectHost), "fetched");
  const doPull = () =>
    runMutation(() => api.gitPull(projectPath, projectHost), "pulled");
  const doPush = () =>
    runMutation(() => api.gitPush(projectPath, false, projectHost), "pushed");

  // Force-push confirmation flow
  const [pendingForcePush, setPendingForcePush] = useState(false);
  const [pendingResetRemote, setPendingResetRemote] = useState(false);
  const doForcePush = () => {
    setPendingForcePush(false);
    runMutation(() => api.gitPush(projectPath, true, projectHost), "force pushed");
  };
  const doResetRemote = () => {
    setPendingResetRemote(false);
    runMutation(() => api.gitResetRemote(projectPath, projectHost), "reset to remote");
  };

  // Opens the stash dialog. With no paths it stashes the whole working tree;
  // with paths it targets just those pathspecs (`git stash push -- <paths>`).
  const openStashDialog = useCallback((paths?: string[]) => {
    setStashTargets(paths && paths.length > 0 ? paths : null);
    setStashMessage("");
    setStashDialog(true);
  }, []);

  // Right-click menu per file row. Actions mirror the row's hover buttons
  // (plus "Open in editor"); the set depends on which pane was clicked —
  // a partially staged file's menu follows the clicked pane. `targets` is the
  // effective selection: the whole pane selection when the row is part of it,
  // otherwise just this row. Each target carries its own untracked flag so a
  // bulk discard routes tracked paths to git restore and untracked ones to the
  // delete hunk.
  const fileMenuItems = useCallback(
    (
      f: GitDiffFile,
      stagedPane: boolean,
      targets: { path: string; untracked: boolean }[],
    ): ContextMenuItem[] => {
      const openItem: ContextMenuItem | null = onOpenFile
        ? {
            label: "Open in editor",
            icon: <ExternalLink className="w-3.5 h-3.5" />,
            onClick: () => onOpenFile(f.path, projectPath),
          }
        : null;
      const paths = targets.map((t) => t.path);
      const n = paths.length;
      if (stagedPane) {
        return [
          {
            label: n > 1 ? `Unstage ${n} files` : "Unstage file",
            icon: <ArrowDownToLine className="w-3.5 h-3.5" />,
            onClick: () => unstageAll(paths),
          },
          {
            label: n > 1 ? `Stash ${n} files` : "Stash file",
            icon: <Archive className="w-3.5 h-3.5" />,
            onClick: () => openStashDialog(paths),
          },
          ...(openItem ? [openItem] : []),
        ];
      }
      const untracked = f.status === "untracked";
      const discardLabel =
        n > 1
          ? targets.every((t) => t.untracked)
            ? `Delete ${n} files`
            : `Discard ${n} files`
          : untracked
            ? "Delete untracked file"
            : "Discard changes";
      return [
        {
          label: n > 1 ? `Stage ${n} files` : "Stage file",
          icon: <ArrowUpToLine className="w-3.5 h-3.5" />,
          onClick: () => stageAll(paths),
        },
        {
          label: discardLabel,
          icon: <Trash2 className="w-3.5 h-3.5" />,
          destructive: true,
          onClick: () => requestDiscard(targets),
        },
        {
          label: n > 1 ? `Stash ${n} files` : "Stash file",
          icon: <Archive className="w-3.5 h-3.5" />,
          onClick: () => openStashDialog(paths),
        },
        ...(openItem ? [openItem] : []),
      ];
    },
    [onOpenFile, projectPath, stageAll, unstageAll, requestDiscard, openStashDialog],
  );

  const hunkAction = useCallback(
    async (file: GitDiffFile, hunkIndex: number, action: GitHunkAction, staged: boolean) => {
      setBusy(true);
      setError(null);
      try {
        const ws = await api.gitHunk(
          { path: file.path, hunk_index: hunkIndex, action, staged },
          projectPath,
          projectHost,
        );
        setWorkspace(ws);
        setSelection((sel) =>
          sel && sel.kind === "file" && sel.path === file.path
            ? {
                kind: "file",
                path: file.path,
                staged: ws.staged.some((f) => f.path === file.path),
              }
            : sel,
        );
        showNotice(action === "discard" ? "discarded hunk" : action + "d hunk");
      } catch (e) {
        setError(e instanceof Error ? e.message : "hunk action failed");
      } finally {
        setBusy(false);
      }
    },
    [projectPath, projectHost, showNotice],
  );

  const commit = () => {
    if (!commitMessage.trim()) {
      setError("Commit message is required");
      return;
    }
    runMutation(async () => {
      await api.gitCommit(commitMessage, [], projectPath, projectHost);
      setCommitMessage("");
      setSelection(null);
      setCommitDiff(null);
    }, "committed");
  };

  // Commit staged changes and push to the remote in one action. The awaits are
  // chained so a failed commit never triggers a push (and the push error, if
  // any, surfaces through the same sticky error banner as a plain push).
  const commitAndPush = () => {
    if (!commitMessage.trim()) {
      setError("Commit message is required");
      return;
    }
    runMutation(async () => {
      await api.gitCommit(commitMessage, [], projectPath, projectHost);
      setCommitMessage("");
      setSelection(null);
      setCommitDiff(null);
      await api.gitPush(projectPath, false, projectHost);
    }, "committed and pushed");
  };

  const selectFile = (file: GitDiffFile, staged: boolean) => {
    setSelection({ kind: "file", path: file.path, staged });
    setCommitDiff(null);
  };

  const selectCommit = async (c: GitCommit) => {
    setSelection({ kind: "commit", hash: c.hash });
    try {
      const files = await api.gitShow(c.hash, projectPath, projectHost);
      setCommitDiff(files);
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to load commit diff");
      setCommitDiff(null);
    }
  };

  const selectStash = async (s: GitStash) => {
    setSelection({ kind: "stash", index: s.index });
    setStashFiles(null);
    setCheckedStashFiles([]);
    try {
      const files = await api.gitStashShow(s.index, projectPath, projectHost);
      setStashFiles(files);
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to load stash files");
      setStashFiles([]);
    }
  };

  const toggleStashFile = (path: string) =>
    setCheckedStashFiles((prev) =>
      prev.includes(path) ? prev.filter((p) => p !== path) : [...prev, path],
    );

  const toggleAllStashFiles = (paths: string[]) =>
    setCheckedStashFiles((prev) =>
      paths.length > 0 && paths.every((p) => prev.includes(p)) ? [] : [...paths],
    );

  // restoreStashFiles applies the selected files from the open stash into the
  // working tree. The stash entry is kept — only the working tree changes.
  const restoreStashFiles = async (index: number, paths: string[]) => {
    setBusy(true);
    setError(null);
    try {
      const ws = await api.gitStashApply(index, paths, projectPath, projectHost);
      setWorkspace(ws);
      setCheckedStashFiles([]);
      showNotice(`restored ${paths.length} file${paths.length === 1 ? "" : "s"} from stash`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to restore stash files");
    } finally {
      setBusy(false);
    }
  };

  // requestRestore gates on local changes: `git restore --source` overwrites
  // the working-tree copy silently, so a selected file that is also locally
  // modified goes through a confirmation dialog first.
  const requestRestore = (index: number, paths: string[]) => {
    if (paths.length === 0) return;
    const dirty = new Set(
      [...(workspace?.staged ?? []), ...(workspace?.unstaged ?? [])].map((f) => f.path),
    );
    const overwrites = paths.filter((p) => dirty.has(p));
    if (overwrites.length > 0) {
      setPendingRestoreStash({ index, paths, overwrites });
      return;
    }
    restoreStashFiles(index, paths);
  };

  const doRestoreStash = () => {
    if (!pendingRestoreStash) return;
    const { index, paths } = pendingRestoreStash;
    setPendingRestoreStash(null);
    restoreStashFiles(index, paths);
  };

  const doStash = () => {
    const message = stashMessage;
    const includeUntracked = stashIncludeUntracked;
    const paths = stashTargets ?? [];
    setStashDialog(false);
    setStashMessage("");
    setStashTargets(null);
    runMutation(
      () => api.gitStash(message, paths, projectPath, projectHost, includeUntracked),
      paths.length > 0 ? `stashed ${paths.length} file(s)` : "stashed changes",
    );
  };

  const doDropStash = () => {
    if (!pendingDeleteStash) return;
    const stash = pendingDeleteStash;
    setPendingDeleteStash(null);
    runMutation(async () => {
      await api.gitStashDrop(stash.index, projectPath, projectHost);
      // The open stash no longer exists; clear its detail pane.
      setSelection((sel) =>
        sel?.kind === "stash" && sel.index === stash.index ? null : sel,
      );
      setStashFiles(null);
      setCheckedStashFiles([]);
    }, `dropped ${stash.ref}`);
  };

  if (!workspace) {
    return (
      <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
        Loading git workspace…
      </div>
    );
  }

  const stagedFiles = workspace.staged ?? [];
  const unstagedFiles = workspace.unstaged ?? [];
  const status = workspace.status;
  // `conflicts` is guaranteed by the Go struct, but a server OLDER than
  // this feature omits it from the JSON. It is read during render, so an
  // undefined there would crash the entire panel — normalize once here
  // rather than guarding every read site.
  const conflicts = status.conflicts ?? [];
  const operation = status.operation ?? null;

  // While an operation is in progress, the controls that would either be
  // meaningless or actively dangerous are disabled: an in-progress merge or
  // rebase is exactly the state where committing, pulling, pushing or
  // resetting produces a confusing git error instead of a clear message.
  // Stash is included for that reason even though this feature never uses it.
  // Resolution and the operation buttons themselves stay enabled — they are
  // the way OUT.
  const opInProgress = !!operation;

  // Wording for the discard/delete confirmation. A pure-untracked selection is
  // a real delete; anything containing tracked files is a revert to HEAD.
  const pendingDiscardTargets = pendingDiscard ?? [];
  const discardCount = pendingDiscardTargets.length;
  const discardAllUntracked =
    discardCount > 0 && pendingDiscardTargets.every((t) => t.untracked);
  const discardDialogTitle = discardAllUntracked
    ? discardCount > 1
      ? `Delete ${discardCount} files?`
      : "Delete untracked file?"
    : discardCount > 1
      ? `Discard changes to ${discardCount} files?`
      : "Discard changes?";

  /** The picked paths belonging to one pane, in no particular order. */
  const pickedPathsInPane = (pane: PaneKey): string[] => {
    const prefix = pane + ":";
    return Array.from(pickedPaths)
      .filter((k) => k.startsWith(prefix))
      .map((k) => k.slice(prefix.length));
  };

  const pickedCountInPane = (pane: PaneKey): number => {
    const prefix = pane + ":";
    let n = 0;
    for (const k of pickedPaths) if (k.startsWith(prefix)) n++;
    return n;
  };

  /** Clear the multi-selection (used by the section header's "N selected ✕"). */
  const clearPicked = () => {
    setPickedPaths(new Set());
    setLastPickedKey(null);
  };

  /**
   * Row click: plain selects one file (and shows its diff), Cmd/Ctrl-click
   * toggles a file in the selection, Shift-click selects — or, when both ends
   * are already selected, clears — the contiguous block between the anchor and
   * the clicked row.
   */
  const handleFileRowClick = (f: GitDiffFile, pane: PaneKey, e: React.MouseEvent) => {
    const key = paneKeyOf(pane, f.path);
    const list = pane === "staged" ? stagedFiles : unstagedFiles;
    if (e.shiftKey && lastPickedKey && lastPickedKey.startsWith(pane + ":")) {
      const ai = list.findIndex((x) => paneKeyOf(pane, x.path) === lastPickedKey);
      const bi = list.findIndex((x) => x.path === f.path);
      if (ai >= 0 && bi >= 0) {
        const [lo, hi] = ai < bi ? [ai, bi] : [bi, ai];
        const range = list.slice(lo, hi + 1).map((x) => paneKeyOf(pane, x.path));
        setPickedPaths((prev) => {
          const next = new Set(prev);
          const removing = next.has(key) && next.has(lastPickedKey);
          for (const k of range) {
            if (removing) next.delete(k);
            else next.add(k);
          }
          return next;
        });
        return;
      }
    }
    if (e.metaKey || e.ctrlKey) {
      setPickedPaths((prev) => {
        const next = new Set(prev);
        if (next.has(key)) next.delete(key);
        else next.add(key);
        return next;
      });
      setLastPickedKey(key);
      return;
    }
    setPickedPaths(new Set([key]));
    setLastPickedKey(key);
    selectFile(f, pane === "staged");
  };

  /** Toggle one row's membership in the multi-selection (its checkbox). */
  const handleTogglePicked = (f: GitDiffFile, pane: PaneKey) => {
    const key = paneKeyOf(pane, f.path);
    setPickedPaths((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
    setLastPickedKey(key);
  };

  /**
   * Right-click a row: narrowing the selection to it when it isn't already
   * picked, then opening the menu whose actions target the effective set.
   */
  const handleFileRowContextMenu = (f: GitDiffFile, pane: PaneKey, e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const key = paneKeyOf(pane, f.path);
    const list = pane === "staged" ? stagedFiles : unstagedFiles;
    let targets: { path: string; untracked: boolean }[];
    if (pickedPaths.has(key)) {
      targets = pickedPathsInPane(pane).map((p) => ({
        path: p,
        untracked: list.find((x) => x.path === p)?.status === "untracked",
      }));
    } else {
      targets = [{ path: f.path, untracked: f.status === "untracked" }];
      setPickedPaths(new Set([key]));
      setLastPickedKey(key);
    }
    setContextMenu({
      items: fileMenuItems(f, pane === "staged", targets),
      position: { x: e.clientX, y: e.clientY },
    });
  };
  const filterLower = fileFilter.trim().toLowerCase();
  const filteredStaged = filterLower
    ? stagedFiles.filter((f) => f.path.toLowerCase().includes(filterLower))
    : stagedFiles;
  const filteredUnstaged = filterLower
    ? unstagedFiles.filter((f) => f.path.toLowerCase().includes(filterLower))
    : unstagedFiles;

  // Which diff is shown in the right pane? Resolution is pane-aware: a
  // partially staged file appears in both lists, and clicking the row in the
  // Staged pane should show the staged diff (not always default to unstaged).
  // If the file vanished from the clicked pane (e.g. just unstaged/restaged),
  // fall back to the other list so the selection does not disappear.
  let shownFile: GitDiffFile | null = null;
  let shownStaged = false;
  if (selection && selection.kind === "file") {
    const primary = selection.staged ? stagedFiles : unstagedFiles;
    const fallback = selection.staged ? unstagedFiles : stagedFiles;
    const primaryHas = primary.some((f) => f.path === selection.path);
    const source = primaryHas ? primary : fallback;
    shownFile = source.find((f) => f.path === selection.path) ?? null;
    // "Staged" describes which version is displayed, not merely that the file
    // exists in the index — a partially staged file is in both lists.
    shownStaged = selection.staged === primaryHas;
  }
  // The file exists in both the index and the working tree — offer a toggle
  // between its two diffs.
  const showBoth =
    selection?.kind === "file" &&
    stagedFiles.some((f) => f.path === selection.path) &&
    unstagedFiles.some((f) => f.path === selection.path);

  return (
      <div className="flex flex-col h-full">
      {/* Header */}
      <div className="px-3 py-2 border-b border-border flex flex-wrap items-center justify-between gap-x-2 gap-y-1">
        <div className="flex items-center gap-2 min-w-0">
          <span className="text-xs text-muted-foreground uppercase tracking-wider shrink-0">
            Git
          </span>
          <span className="text-xs text-muted-foreground font-mono truncate flex items-center gap-1">
            <GitBranch className="w-3.5 h-3.5 shrink-0" />
            {status.branch || "no branch"}
            {status.has_upstream && (
              <span className="inline-flex items-center gap-0.5 text-xs font-semibold leading-none text-amber-500 ml-1">
                {status.ahead > 0 && <span title={`${status.ahead} to push`}>↑{status.ahead}</span>}
                {status.behind > 0 && <span title={`${status.behind} to pull`}>↓{status.behind}</span>}
              </span>
            )}
          </span>
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2 min-w-0">
          <input
            type="text"
            placeholder="Filter file..."
            value={fileFilter}
            onChange={(e) => setFileFilter(e.target.value)}
            className="h-7 px-2 rounded-md bg-muted/40 border border-border text-xs focus:outline-none focus:ring-2 focus:ring-ring w-28 sm:w-36 md:w-48"
          />
          <span
            className="hidden sm:inline text-xs text-muted-foreground"
            data-testid="git-summary-counts"
          >
            {conflicts.length > 0 && (
              <span className="text-red-400">{conflicts.length} conflicted · </span>
            )}
            {filteredStaged.length} staged · {filteredUnstaged.length} unstaged
          </span>
          <button
            onClick={doFetch}
            disabled={busy || opInProgress}
            aria-label="Fetch all remotes"
            title={opInProgress ? "Finish or abort the in-progress git operation first" : "Fetch all remotes"}
            className="p-1 rounded hover:bg-muted/60 text-muted-foreground hover:text-foreground disabled:opacity-40"
          >
            <RefreshCw className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={doPull}
            disabled={busy || opInProgress}
            aria-label="Pull from remote"
            title={opInProgress ? "Finish or abort the in-progress git operation first" : "Pull from remote"}
            className="p-1 rounded hover:bg-muted/60 text-muted-foreground hover:text-foreground disabled:opacity-40"
          >
            <ArrowDownToLine className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={doPush}
            disabled={busy || opInProgress}
            aria-label="Push to remote"
            title={opInProgress ? "Finish or abort the in-progress git operation first" : "Push to remote"}
            className="p-1 rounded hover:bg-muted/60 text-muted-foreground hover:text-foreground disabled:opacity-40"
          >
            <ArrowUpToLine className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={() => setPendingForcePush(true)}
            disabled={busy || opInProgress}
            aria-label="Force push with lease"
            title={opInProgress ? "Finish or abort the in-progress git operation first" : "Force push with lease"}
            className="p-1 rounded hover:bg-muted/60 text-amber-500 hover:text-amber-400 disabled:opacity-40"
          >
            <AlertTriangle className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={() => setPendingResetRemote(true)}
            disabled={busy || opInProgress}
            aria-label="Reset to remote"
            title={opInProgress ? "Finish or abort the in-progress git operation first" : "Reset to remote (discard local commits and changes)"}
            className="p-1 rounded hover:bg-muted/60 text-red-400 hover:text-red-300 disabled:opacity-40"
          >
            <Trash2 className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={() => load()}
            disabled={refreshing}
            title="Refresh"
            className="p-1 rounded hover:bg-muted/60 text-muted-foreground hover:text-foreground disabled:opacity-50"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${refreshing ? "animate-spin" : ""}`} />
          </button>
          <button
            onClick={filePane.toggleCollapsed}
            aria-label={filePane.collapsed ? "Show file list" : "Hide file list"}
            aria-expanded={!filePane.collapsed}
            title={filePane.collapsed ? "Show file list" : "Hide file list"}
            className="p-1 rounded hover:bg-muted/60 text-muted-foreground hover:text-foreground"
          >
            {filePane.collapsed ? (
              <PanelLeft className="w-3.5 h-3.5" />
            ) : (
              <PanelLeftClose className="w-3.5 h-3.5" />
            )}
          </button>
        </div>
      </div>

      {/* Operation banner — shown whenever git has stopped mid-operation.
          Sits directly under the header, above the conflicts section, because
          it is the reason the conflicts exist and it holds the way out. */}
      {operation && (() => {
        // A kind this bundle has no entry for means the SERVER is newer than
        // this build. It is not a reason to render an empty action row: that
        // tells the user a rebase is in progress and hands them no way out, so
        // the only honest move is to name the operation, refuse to drive it,
        // and point at the terminal. Logged because it is a real version skew
        // someone must fix, not a condition to swallow.
        const actions = Object.prototype.hasOwnProperty.call(
          OPERATION_ACTIONS,
          operation.kind,
        )
          ? OPERATION_ACTIONS[operation.kind]
          : undefined;
        if (!actions) {
          console.warn(
            "[git] unknown operation kind from server; cannot drive it from this build",
            { kind: operation.kind, label: operation.label },
          );
        }
        return (
        <div
          data-testid="git-operation-banner"
          className="px-3 py-2 border-b border-border bg-amber-500/10 flex flex-wrap items-center gap-2"
        >
          <AlertTriangle className="w-4 h-4 text-amber-500 shrink-0" />
          <span className="text-xs font-medium text-amber-400 mr-auto">
            {operation.label}
          </span>
          {actions ? (
          <div className="flex flex-wrap items-center gap-1">
            {actions.map((a) => {
              // Continue is blocked while any conflict is unresolved. Git
              // refuses anyway; a disabled button with this tooltip explains
              // why instead of relaying "you have unmerged files" afterwards.
              const blockedByConflicts =
                a.action === "continue" && conflicts.length > 0;
              return (
                <button
                  key={a.action}
                  onClick={() =>
                    requestOperation(a.action, operation.kind, !!a.destructive, a.label)
                  }
                  disabled={busy || blockedByConflicts}
                  title={
                    blockedByConflicts
                      ? "Resolve the remaining conflicts before continuing"
                      : undefined
                  }
                  className="px-2 py-1 rounded text-xs border border-border hover:bg-muted/60 disabled:opacity-40 disabled:cursor-not-allowed"
                >
                  {a.label}
                </button>
              );
            })}
          </div>
          ) : (
            // No action row at all, and no pretend buttons: this build cannot
            // drive an operation it does not know. The terminal can.
            <span className="text-xs text-amber-400/90">
              This operation can&apos;t be continued here — finish it in the
              terminal.
            </span>
          )}
        </div>
        );
      })()}

      {/* Conflicts section — only rendered when something is conflicted, so a
          clean repository's layout is unchanged. Above staged, because a
          conflict must be dealt with before anything can be committed. */}
      {conflicts.length > 0 && (
        <div data-testid="git-conflicts" className="border-b border-border">
          <div className="px-3 py-1.5 text-xs uppercase tracking-wider text-muted-foreground flex items-center gap-2">
            <span>Conflicts</span>
            <span className="text-red-400">{conflicts.length}</span>
          </div>
          {conflicts.map((c) => {
            const labels = conflictSideLabels(operation?.kind);
            return (
              <div
                key={c.path}
                data-testid="git-conflict-row"
                className="px-3 py-1.5 flex flex-wrap items-center gap-2 hover:bg-muted/30"
              >
                <span
                  className={`w-4 h-4 rounded text-[10px] flex items-center justify-center shrink-0 ${STATUS_BADGES.conflicted.color}`}
                  title={c.code}
                >
                  {STATUS_BADGES.conflicted.label}
                </span>
                <button
                  onClick={() => onOpenFile?.(c.path, projectPath)}
                  className="text-xs font-mono truncate text-left hover:underline min-w-0 flex-1"
                  title={c.path}
                >
                  {c.path}
                </button>
                <div className="flex items-center gap-1 shrink-0">
                  <button
                    onClick={() => resolveConflict(c.path, "ours")}
                    disabled={busy}
                    title={labels.oursTip}
                    className="px-1.5 py-0.5 rounded text-[11px] border border-border hover:bg-muted/60 disabled:opacity-40"
                  >
                    {labels.ours}
                  </button>
                  <button
                    onClick={() => resolveConflict(c.path, "theirs")}
                    disabled={busy}
                    title={labels.theirsTip}
                    className="px-1.5 py-0.5 rounded text-[11px] border border-border hover:bg-muted/60 disabled:opacity-40"
                  >
                    {labels.theirs}
                  </button>
                  <button
                    onClick={() => resolveConflict(c.path, "mark")}
                    disabled={busy}
                    title="Stage this file as you edited it (refused while conflict markers remain)"
                    className="px-1.5 py-0.5 rounded text-[11px] border border-border hover:bg-muted/60 disabled:opacity-40"
                  >
                    Mark resolved
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {error && (
        <div className="px-3 py-1.5 text-xs bg-red-500/10 text-red-400 border-b border-border">
          {error}
        </div>
      )}

      {notice && !error && (
        <div
          role="status"
          data-testid="git-notice"
          className="px-3 py-1.5 text-xs bg-green-500/10 text-green-400 border-b border-border"
        >
          {notice}
        </div>
      )}

      {/* Body — side-by-side from the md breakpoint up, stacked (file list
          above the diff) on narrower viewports. */}
      <div
        data-testid="git-body"
        className={cn("flex flex-1 min-h-0", isMobile && "flex-col")}
      >
        {/* Left: staged / unstaged / commits — width is drag-resizable and
            persisted (see `filePane`). The header toggle collapses it to zero;
            `overflow-hidden` clips the content while the width/height animates. */}
        <div
          data-testid="git-file-pane"
          className={cn(
            "shrink-0 flex flex-col min-h-0 bg-muted/10 overflow-hidden transition-[width,height] duration-100",
            isMobile && "w-full",
            isMobile && !filePane.collapsed && "border-b border-border",
          )}
          style={
            isMobile
              ? { height: filePane.collapsed ? 0 : MOBILE_FILE_PANE_HEIGHT }
              : { width: filePane.collapsed ? 0 : filePane.width }
          }
        >
          <FileSection
            title="Staged changes"
            stagedPane
            files={filteredStaged}
            selected={selection}
            busy={busy}
            onRowClick={(f, e) => handleFileRowClick(f, "staged", e)}
            onTogglePicked={(f) => handleTogglePicked(f, "staged")}
            isPicked={(f) => pickedPaths.has(paneKeyOf("staged", f.path))}
            pickedCount={pickedCountInPane("staged")}
            onClearPicked={clearPicked}
            collapsed={!sections.staged}
            onToggle={() => toggleSection("staged")}
            onRowContextMenu={(f, e) => handleFileRowContextMenu(f, "staged", e)}
            onSectionAction={
              filteredStaged.length > 1
                ? { label: "Unstage all", fn: () => unstageAll(filteredStaged.map((f) => f.path)) }
                : undefined
            }
            rowActions={(f) => (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  unstageFile(f.path);
                }}
                disabled={busy}
                title="Unstage file"
                className="opacity-0 group-hover:opacity-100 p-0.5 rounded text-blue-400 hover:text-blue-300 disabled:opacity-40"
              >
                <ArrowDownToLine className="w-3.5 h-3.5" />
              </button>
            )}
          />

          <FileSection
            title="Unstaged changes"
            stagedPane={false}
            files={filteredUnstaged}
            selected={selection}
            busy={busy}
            onRowClick={(f, e) => handleFileRowClick(f, "unstaged", e)}
            onTogglePicked={(f) => handleTogglePicked(f, "unstaged")}
            isPicked={(f) => pickedPaths.has(paneKeyOf("unstaged", f.path))}
            pickedCount={pickedCountInPane("unstaged")}
            onClearPicked={clearPicked}
            collapsed={!sections.unstaged}
            onToggle={() => toggleSection("unstaged")}
            onRowContextMenu={(f, e) => handleFileRowContextMenu(f, "unstaged", e)}
            onSectionAction={
              filteredUnstaged.length > 1
                ? { label: "Stage all", fn: () => stageAll(filteredUnstaged.map((f) => f.path)) }
                : undefined
            }
            rowActions={(f) => (
              <span className="opacity-0 group-hover:opacity-100 flex items-center gap-0.5">
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    stageFile(f.path);
                  }}
                  disabled={busy}
                  title="Stage file"
                  className="p-0.5 rounded text-green-400 hover:text-green-300 disabled:opacity-40"
                >
                  <ArrowUpToLine className="w-3.5 h-3.5" />
                </button>
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    discardFile(f.path, f.status === "untracked");
                  }}
                  disabled={busy}
                  title={f.status === "untracked" ? "Delete file" : "Discard changes"}
                  className="p-0.5 rounded text-red-400 hover:text-red-300 disabled:opacity-40"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              </span>
            )}
          />

          {/* Commits */}
          <div
            className={`min-h-0 flex flex-col border-t border-border ${
              sections.commits ? "flex-1" : "shrink-0"
            }`}
          >
            <button
              onClick={() => toggleSection("commits")}
              title={sections.commits ? "Collapse commits" : "Expand commits"}
              className="px-3 py-1.5 text-xs uppercase tracking-wider text-muted-foreground flex items-center justify-between hover:bg-muted/40 hover:text-foreground cursor-pointer text-left"
            >
              <span className="flex items-center gap-1">
                {sections.commits ? (
                  <ChevronDown className="w-3.5 h-3.5 shrink-0" />
                ) : (
                  <ChevronRight className="w-3.5 h-3.5 shrink-0" />
                )}
                Commits
              </span>
              <span className="text-muted-foreground/60 normal-case">{commits.length}</span>
            </button>
            {sections.commits && (
              <div className="flex-1 overflow-y-auto divide-y divide-border">
                {commits.length === 0 ? (
                  <div className="px-3 py-2 text-xs text-muted-foreground/70 italic">
                    No commits yet
                  </div>
                ) : (
                  commits.map((c) => {
                    const isSelected =
                      selection?.kind === "commit" && selection.hash === c.hash;
                    return (
                      <button
                        key={c.hash}
                        onClick={() => selectCommit(c)}
                        className={`w-full text-left px-3 py-2 hover:bg-muted/50 ${
                          isSelected ? "bg-muted" : ""
                        }`}
                      >
                        <div className="flex items-center gap-1.5 text-xs font-mono text-foreground truncate">
                          <GitCommitVertical className="w-3 h-3 shrink-0 text-muted-foreground" />
                          <span className="text-blue-400 shrink-0">{c.short}</span>
                          <span className="truncate">{c.message}</span>
                        </div>
                        <div className="pl-5 text-[11px] text-muted-foreground truncate">
                          {c.author} · {timeAgo(c.date)}
                        </div>
                      </button>
                    );
                  })
                )}
              </div>
            )}
          </div>

          {/* Stashes */}
          <div
            className={`min-h-0 flex flex-col border-t border-border ${
              sections.stashes ? "flex-1" : "shrink-0"
            }`}
          >
            <div className="px-3 py-1.5 text-xs uppercase tracking-wider text-muted-foreground flex items-center justify-between">
              <button
                onClick={() => toggleSection("stashes")}
                title={sections.stashes ? "Collapse stashes" : "Expand stashes"}
                className="flex items-center gap-1 min-w-0 text-left cursor-pointer hover:text-foreground"
              >
                {sections.stashes ? (
                  <ChevronDown className="w-3.5 h-3.5 shrink-0" />
                ) : (
                  <ChevronRight className="w-3.5 h-3.5 shrink-0" />
                )}
                <span className="truncate">
                  Stashes <span className="text-foreground/50">({stashes.length})</span>
                </span>
              </button>
              <button
                onClick={() => openStashDialog()}
                disabled={busy || opInProgress || !status.has_changes}
                title={
                  status.has_changes
                    ? "Stash all changes"
                    : "Nothing to stash — the working tree is clean"
                }
                className="text-[10px] normal-case text-purple-400 hover:text-purple-300 hover:underline disabled:opacity-40 disabled:no-underline"
              >
                Stash all
              </button>
            </div>
            {sections.stashes && (
              <div className="flex-1 overflow-y-auto divide-y divide-border/60">
                {stashes.length === 0 ? (
                  <div className="px-3 py-2 text-xs text-muted-foreground/70 italic">
                    No stashes
                  </div>
                ) : (
                  stashes.map((s) => {
                    const isSelected =
                      selection?.kind === "stash" && selection.index === s.index;
                    return (
                      <div
                        key={s.ref}
                        className={`group flex items-center gap-1 pl-2 pr-1.5 py-1.5 hover:bg-muted/50 ${
                          isSelected ? "bg-muted" : ""
                        }`}
                      >
                        <button
                          onClick={() => selectStash(s)}
                          title={`${s.ref}: ${s.message}`}
                          className="flex-1 min-w-0 text-left"
                        >
                          <div className="flex items-center gap-1.5 text-xs font-mono text-foreground truncate">
                            <Archive className="w-3 h-3 shrink-0 text-muted-foreground" />
                            <span className="text-purple-400 shrink-0">{s.ref}</span>
                            <span className="truncate">{stashLabel(s.message)}</span>
                          </div>
                          <div className="pl-5 text-[11px] text-muted-foreground truncate">
                            {s.author} · {timeAgo(s.date)}
                          </div>
                        </button>
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            setPendingDeleteStash(s);
                          }}
                          disabled={busy}
                          title={`Delete ${s.ref}`}
                          aria-label={`Delete ${s.ref}`}
                          className="opacity-0 group-hover:opacity-100 p-0.5 rounded text-red-400 hover:text-red-300 disabled:opacity-40"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                    );
                  })
                )}
              </div>
            )}
          </div>
        </div>

        {/* Drag handle between the file list and the diff pane. Double-click
            restores the default width. Hidden when the pane is collapsed, or
            when the panel is stacked (a column resize is meaningless there). */}
        {!filePane.collapsed && !isMobile && (
          <div
            ref={filePane.handleRef}
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize file list and diff"
            title="Drag to resize · double-click to reset"
            onPointerDown={filePane.onPointerDown}
            onDoubleClick={filePane.resetToDefault}
            className="w-1 shrink-0 cursor-col-resize bg-border hover:bg-accent active:bg-accent"
          />
        )}

        {/* Right: diff pane */}
        <div className="flex-1 min-h-0 flex flex-col">
          {selection?.kind === "commit" && commitDiff ? (
            <div className="flex-1 min-h-0 overflow-y-auto">
              <CommitDiff files={commitDiff} />
            </div>
          ) : selection?.kind === "stash" ? (
            <StashDiff
              stash={stashes.find((s) => s.index === selection.index) ?? null}
              files={stashFiles}
              checked={checkedStashFiles}
              busy={busy}
              onToggle={toggleStashFile}
              onToggleAll={toggleAllStashFiles}
              onRestore={() => requestRestore(selection.index, checkedStashFiles)}
            />
          ) : shownFile ? (
            <>
              {showBoth && (
                <div className="px-3 py-1.5 border-b border-border flex items-center gap-1 shrink-0">
                  <span className="text-[11px] uppercase tracking-wider text-muted-foreground mr-1">
                    Diff
                  </span>
                  <button
                    onClick={() =>
                      setSelection({ kind: "file", path: shownFile!.path, staged: false })
                    }
                    className={`px-2 py-0.5 rounded text-xs ${
                      shownStaged
                        ? "text-muted-foreground hover:bg-muted/60"
                        : "bg-primary text-primary-foreground"
                    }`}
                  >
                    Working tree
                  </button>
                  <button
                    onClick={() =>
                      setSelection({ kind: "file", path: shownFile!.path, staged: true })
                    }
                    className={`px-2 py-0.5 rounded text-xs ${
                      shownStaged
                        ? "bg-primary text-primary-foreground"
                        : "text-muted-foreground hover:bg-muted/60"
                    }`}
                  >
                    Staged
                  </button>
                </div>
              )}
              <div className="flex-1 min-h-0 overflow-y-auto">
                <FileDiff
                  file={shownFile}
                  staged={shownStaged}
                  busy={busy}
                  opInProgress={opInProgress}
                  onHunk={(i, a) => hunkAction(shownFile, i, a, shownStaged)}
                  onOpen={onOpenFile ? () => onOpenFile(shownFile!.path, projectPath) : undefined}
                />
              </div>
            </>
          ) : (
            <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
              {selection?.kind === "commit"
                ? "Loading commit diff…"
                : "Select a file to view its diff"}
            </div>
          )}
        </div>
      </div>

      {/* Commit box */}
      <div className="p-3 border-t border-border flex items-center gap-2">
        <input
          value={commitMessage}
          onChange={(e) => setCommitMessage(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) commit();
          }}
          placeholder="Commit message for staged changes…"
          className="flex-1 h-9 px-3 rounded-md bg-muted/40 border border-border text-sm focus:outline-none focus:ring-2 focus:ring-ring"
        />
        <button
          onClick={commit}
          disabled={busy || opInProgress || !status.has_changes || !commitMessage.trim()}
          title={opInProgress ? "Finish or abort the in-progress git operation first" : undefined}
          className="h-9 px-4 rounded-md bg-primary text-primary-foreground text-sm font-medium hover:opacity-90 disabled:opacity-40"
        >
          Commit
        </button>
        <button
          onClick={commitAndPush}
          disabled={busy || opInProgress || !status.has_changes || !commitMessage.trim()}
          title={opInProgress ? "Finish or abort the in-progress git operation first" : "Commit staged changes, then push to the remote"}
          className="h-9 px-3 shrink-0 whitespace-nowrap rounded-md border border-border bg-muted/40 text-sm font-medium hover:bg-muted/60 disabled:opacity-40"
        >
          Commit &amp; Push
        </button>
      </div>

      {/* Force-push confirmation dialog */}
      <Dialog open={pendingForcePush} onOpenChange={(open) => !open && setPendingForcePush(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <AlertTriangle className="w-4 h-4 text-amber-500" />
              Force push
            </DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground mt-2">
            This may overwrite remote history with your local commits. The lease protects remote
            changes that appeared after your last fetch by rejecting the push instead of overwriting
            them. Are you sure you want to continue?
          </p>
          <DialogFooter className="mt-4">
            <Button
              variant="outline"
              onClick={() => setPendingForcePush(false)}
              data-dialog-default-action
            >
              Cancel
            </Button>
            <Button variant="destructive" onClick={doForcePush}>
              Force Push
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={pendingResetRemote} onOpenChange={(open) => !open && setPendingResetRemote(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Trash2 className="w-4 h-4 text-red-400" />
              Reset to remote
            </DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground mt-2">
            This fetches the upstream branch and hard-resets the current branch to it. All local
            commits and tracked working-tree changes not on the remote will be permanently discarded.
          </p>
          <DialogFooter className="mt-4">
            <Button
              variant="outline"
              onClick={() => setPendingResetRemote(false)}
              data-dialog-default-action
            >
              Cancel
            </Button>
            <Button variant="destructive" onClick={doResetRemote}>
              Reset to Remote
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Operation-abort confirmation. Aborting discards the in-progress
          operation, so it follows the same rendered-dialog rule as discard
          (the desktop webview's native confirm() silently returns false). */}
      <Dialog
        open={pendingOperation !== null}
        onOpenChange={(open) => !open && setPendingOperation(null)}
      >
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-sm">
              <AlertTriangle className="w-4 h-4 text-red-400" />
              {pendingOperation?.label} this operation?
            </DialogTitle>
          </DialogHeader>
          <p className="text-xs text-muted-foreground mt-1">
            This discards the in-progress git operation and restores the repository
            to the state before it started.
          </p>
          <div className="flex justify-end gap-2 mt-3">
            <button
              onClick={() => setPendingOperation(null)}
              className="px-2 py-1 rounded text-xs border border-border hover:bg-muted/60"
            >
              Cancel
            </button>
            <button
              onClick={doPendingOperation}
              data-dialog-default-action
              className="px-2 py-1 rounded text-xs bg-red-500/20 text-red-400 border border-red-500/40 hover:bg-red-500/30"
            >
              {pendingOperation?.label}
            </button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Discard/delete confirmation. The desktop webview's native confirm()
          silently returns false, so destructive file actions must use a
          rendered dialog (same rule as FileTree's ConfirmDeleteDialog). */}
      <Dialog
        open={pendingDiscard !== null}
        onOpenChange={(open) => !open && setPendingDiscard(null)}
      >
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-sm">
              <AlertTriangle className="w-4 h-4 text-red-400" />
              {discardDialogTitle}
            </DialogTitle>
          </DialogHeader>
          <p className="text-xs text-muted-foreground mt-1">
            {discardAllUntracked
              ? "This permanently deletes the file(s) from disk and cannot be undone."
              : "Local changes are reverted to HEAD and cannot be undone."}
          </p>
          <ul className="mt-2 space-y-0.5 max-h-36 overflow-y-auto rounded border border-border p-2">
            {pendingDiscardTargets.slice(0, 5).map((t) => (
              <li
                key={t.path}
                className="font-mono text-[11px] text-foreground truncate"
                title={t.path}
              >
                {t.path}
              </li>
            ))}
            {discardCount > 5 && (
              <li className="text-[11px] text-muted-foreground/70">
                …and {discardCount - 5} more
              </li>
            )}
          </ul>
          <DialogFooter className="mt-4">
            <Button
              variant="ghost"
              onClick={() => setPendingDiscard(null)}
              data-dialog-default-action
            >
              Cancel
            </Button>
            <Button variant="destructive" onClick={doDiscard} disabled={busy}>
              {discardAllUntracked ? "Delete" : "Discard"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Stash dialog — "all changes" from the section header, or the picked
          files when opened from a row / multi-selection. */}
      <Dialog open={stashDialog} onOpenChange={setStashDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Archive className="w-4 h-4 text-purple-400" />
              {stashTargets
                ? `Stash ${stashTargets.length} file${stashTargets.length === 1 ? "" : "s"}`
                : "Stash all changes"}
            </DialogTitle>
          </DialogHeader>
          <div className="mt-2 space-y-3">
            <input
              value={stashMessage}
              onChange={(e) => setStashMessage(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") doStash();
              }}
              placeholder="Stash message (optional)"
              className="w-full h-9 px-3 rounded-md bg-muted/40 border border-border text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
            <label className="flex items-center gap-2 text-sm text-muted-foreground cursor-pointer select-none">
              <input
                type="checkbox"
                checked={stashIncludeUntracked}
                onChange={(e) => setStashIncludeUntracked(e.target.checked)}
              />
              Include untracked files
            </label>
            <p className="text-xs text-muted-foreground">
              {stashTargets
                ? "The selected files are reverted to HEAD and saved in a stash entry you can restore or delete later from the Stashes list."
                : "Tracked changes are reverted to HEAD and saved in a stash entry you can restore or delete later from the Stashes list."}
            </p>
            {stashTargets && (
              <ul className="max-h-32 overflow-y-auto rounded-md border border-border bg-muted/20 p-2 text-xs font-mono text-muted-foreground space-y-0.5">
                {stashTargets.map((p) => (
                  <li key={p} className="truncate">
                    {p}
                  </li>
                ))}
              </ul>
            )}
          </div>
          <DialogFooter className="mt-4">
            <Button
              variant="outline"
              onClick={() => setStashDialog(false)}
              data-dialog-default-action
            >
              Cancel
            </Button>
            <Button onClick={doStash}>Stash</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete-stash confirmation */}
      <Dialog
        open={pendingDeleteStash !== null}
        onOpenChange={(open) => !open && setPendingDeleteStash(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <AlertTriangle className="w-4 h-4 text-amber-500" />
              Delete stash
            </DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground mt-2">
            Delete <span className="font-mono text-foreground">{pendingDeleteStash?.ref}</span>
            {pendingDeleteStash ? ` (${stashLabel(pendingDeleteStash.message)})` : ""}? The stashed
            changes will be permanently discarded. This cannot be undone.
          </p>
          <DialogFooter className="mt-4">
            <Button
              variant="outline"
              onClick={() => setPendingDeleteStash(null)}
              data-dialog-default-action
            >
              Cancel
            </Button>
            <Button variant="destructive" onClick={doDropStash}>
              Delete stash
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Restore-overwrite confirmation */}
      <Dialog
        open={pendingRestoreStash !== null}
        onOpenChange={(open) => !open && setPendingRestoreStash(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <AlertTriangle className="w-4 h-4 text-amber-500" />
              Overwrite local changes?
            </DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground mt-2">
            Restoring from the stash will overwrite local changes in{" "}
            {pendingRestoreStash?.overwrites.length ?? 0} selected file(s):
          </p>
          <ul className="mt-2 max-h-32 overflow-y-auto text-xs font-mono text-foreground space-y-0.5">
            {pendingRestoreStash?.overwrites.map((p) => (
              <li key={p} className="truncate">
                {p}
              </li>
            ))}
          </ul>
          <DialogFooter className="mt-4">
            <Button
              variant="outline"
              onClick={() => setPendingRestoreStash(null)}
              data-dialog-default-action
            >
              Cancel
            </Button>
            <Button variant="destructive" onClick={doRestoreStash}>
              Overwrite and restore
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {contextMenu && (
        <ContextMenu
          items={contextMenu.items}
          open
          position={contextMenu.position}
          onClose={() => setContextMenu(null)}
        />
      )}
    </div>
  );
}

/* ------------------------------------------------------------------ */

function FileSection({
  title,
  stagedPane,
  files,
  selected,
  busy,
  onRowClick,
  onTogglePicked,
  isPicked,
  pickedCount,
  onClearPicked,
  onSectionAction,
  rowActions,
  collapsed,
  onToggle,
  onRowContextMenu,
}: {
  title: string;
  stagedPane: boolean;
  files: GitDiffFile[];
  selected: Selection;
  busy: boolean;
  /** Plain / Cmd-Ctrl / Shift-click handled by the parent (multi-select). */
  onRowClick: (f: GitDiffFile, e: React.MouseEvent) => void;
  /** Toggle one row's membership in the multi-selection (checkbox). */
  onTogglePicked: (f: GitDiffFile) => void;
  /** Whether a given row is part of the multi-selection. */
  isPicked: (f: GitDiffFile) => boolean;
  pickedCount: number;
  onClearPicked: () => void;
  onSectionAction?: { label: string; fn: () => void };
  rowActions?: (f: GitDiffFile) => React.ReactNode;
  collapsed?: boolean;
  onToggle?: () => void;
  onRowContextMenu: (f: GitDiffFile, e: React.MouseEvent) => void;
}) {
  return (
    <div className="shrink-0 max-h-[34%] min-h-0 flex flex-col">
      <div className="px-3 py-1.5 text-xs uppercase tracking-wider text-muted-foreground flex items-center gap-2">
        {onToggle ? (
          <button
            onClick={onToggle}
            title={collapsed ? `Expand ${title}` : `Collapse ${title}`}
            className="flex items-center gap-1 min-w-0 text-left cursor-pointer hover:text-foreground"
          >
            {collapsed ? (
              <ChevronRight className="w-3.5 h-3.5 shrink-0" />
            ) : (
              <ChevronDown className="w-3.5 h-3.5 shrink-0" />
            )}
            <span className="truncate">
              {title} <span className="text-foreground/50">({files.length})</span>
            </span>
          </button>
        ) : (
          <span>
            {title} <span className="text-foreground/50">({files.length})</span>
          </span>
        )}
        {!collapsed && pickedCount > 0 && (
          <button
            onClick={onClearPicked}
            title="Clear selection"
            className="text-[10px] normal-case text-muted-foreground hover:text-foreground shrink-0"
          >
            {pickedCount} selected ✕
          </button>
        )}
        <div className="flex-1" />
        {!collapsed && onSectionAction && (
          <button
            onClick={onSectionAction.fn}
            disabled={busy}
            title={onSectionAction.label}
            className="text-[10px] normal-case text-blue-400 hover:text-blue-300 hover:underline disabled:opacity-40"
          >
            {onSectionAction.label}
          </button>
        )}
      </div>
      {!collapsed && (
        <div className="overflow-y-auto min-h-0">
          {files.length === 0 ? (
            <div className="px-3 py-1 text-xs text-muted-foreground/60 italic">
              Nothing to show
            </div>
          ) : (
            <div className="divide-y divide-border/60">
              {files.map((f) => {
                const badge = STATUS_BADGES[f.status] || STATUS_BADGES.modified;
                const picked = isPicked(f);
                const isSelected =
                  selected?.kind === "file" &&
                  selected.path === f.path &&
                  selected.staged === stagedPane;
                const rowState = picked
                  ? "bg-accent text-accent-foreground"
                  : isSelected
                    ? "bg-muted"
                    : "hover:bg-muted/50";
                return (
                  <div
                    key={f.path}
                    onClick={(e) => onRowClick(f, e)}
                    onContextMenu={(e) => onRowContextMenu(f, e)}
                    className={`group flex items-center gap-1.5 pl-1.5 pr-1.5 py-1 text-sm cursor-pointer ${rowState}`}
                  >
                    <button
                      type="button"
                      aria-label={`${picked ? "Deselect" : "Select"} ${f.path}`}
                      aria-pressed={picked}
                      onClick={(e) => {
                        e.stopPropagation();
                        onTogglePicked(f);
                      }}
                      className={`shrink-0 w-3.5 h-3.5 rounded-sm border flex items-center justify-center transition-colors ${
                        picked
                          ? "bg-primary border-primary text-primary-foreground"
                          : "border-border opacity-0 group-hover:opacity-100 hover:border-foreground/50"
                      }`}
                    >
                      {picked && <Check className="w-3 h-3" />}
                    </button>
                    <span
                      className={`inline-flex items-center justify-center w-5 h-5 shrink-0 rounded text-[10px] font-bold ${badge.color}`}
                    >
                      {badge.label}
                    </span>
                    <span className="font-mono text-foreground truncate flex-1">
                      {f.path}
                    </span>
                    {rowActions?.(f)}
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

/** Working-tree diff for one file, with per-hunk stage/unstage/reverse. */
function FileDiff({
  file,
  staged,
  busy,
  opInProgress,
  onHunk,
  onOpen,
}: {
  file: GitDiffFile;
  staged: boolean;
  busy: boolean;
  /** An operation is in progress: per-hunk stage/unstage/discard are
   *  disabled, because touching the index mid-merge is how a conflict gets
   *  silently mangled. The conflict resolver is the way through. */
  opInProgress: boolean;
  onHunk: (hunkIndex: number, action: GitHunkAction) => void;
  onOpen?: () => void;
}) {
  const hunks = splitHunks(file.patch);
  const isUntracked = file.status === "untracked";
  const wholeFile = isUntracked || file.status === "added" || file.status === "deleted";

  return (
    <div className="p-3">
      {/* file header */}
      <div className="flex items-center justify-between gap-2 mb-2 pb-2 border-b border-border">
        <span className="font-mono text-xs text-foreground truncate flex-1">
          {file.path}
          <span className="ml-2 text-[10px] uppercase tracking-wider text-muted-foreground">
            {staged ? "staged" : file.status}
            {wholeFile ? " (whole file)" : ""}
          </span>
        </span>
        {onOpen && (
          <button
            onClick={onOpen}
            className="flex items-center gap-1 text-xs text-blue-400 hover:text-blue-300 shrink-0"
            title="Open file in editor tab"
          >
            <ExternalLink className="w-3 h-3" />
            Open
          </button>
        )}
      </div>

      {hunks.length === 0 ? (
        <pre className="text-xs font-mono whitespace-pre-wrap text-muted-foreground">
          {file.patch || "(no textual diff — binary file)"}
        </pre>
      ) : (
        hunks.map((hunk, i) => (
          <div key={i} className="mb-3 border border-border rounded-md overflow-hidden">
            {/* hunk header + actions */}
            <div className="flex items-center gap-2 px-2 py-1.5 bg-muted/30 border-b border-border">
              <span className="text-blue-400 font-mono text-xs shrink-0">
                {hunk[0]}
              </span>
              <div className="flex-1" />
              {staged ? (
                <button
                  onClick={() => onHunk(i, "unstage")}
                  disabled={busy || opInProgress}
                  className="text-[11px] px-1.5 py-0.5 rounded bg-blue-500/15 text-blue-400 hover:bg-blue-500/25 disabled:opacity-40 shrink-0"
                >
                  Unstage hunk
                </button>
              ) : (
                <>
                  <button
                    onClick={() => onHunk(i, "stage")}
                    disabled={busy || opInProgress}
                    className="text-[11px] px-1.5 py-0.5 rounded bg-green-500/15 text-green-400 hover:bg-green-500/25 disabled:opacity-40 shrink-0"
                  >
                    {isUntracked ? "Stage file" : "Stage hunk"}
                  </button>
                  <button
                    onClick={() => onHunk(i, "discard")}
                    disabled={busy || opInProgress}
                    title={
                      isUntracked
                        ? "Delete this untracked file"
                        : "Reverse this hunk in the working tree"
                    }
                    className="text-[11px] px-1.5 py-0.5 rounded bg-red-500/15 text-red-400 hover:bg-red-500/25 disabled:opacity-40 shrink-0"
                  >
                    {isUntracked ? "Delete" : "Reverse hunk"}
                  </button>
                </>
              )}
            </div>
            {/* hunk body */}
            <div className="font-mono text-xs whitespace-pre-wrap px-2 py-1.5">
              {hunk.slice(1).map((line, j) => (
                <div key={j} className={lineColor(line)}>
                  {line || " "}
                </div>
              ))}
            </div>
          </div>
        ))
      )}
    </div>
  );
}

/** Read-only diff of a commit (from git show). */
function CommitDiff({ files }: { files: GitDiffFile[] }) {
  if (files.length === 0) {
    return (
      <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
        No file changes in this commit
      </div>
    );
  }
  return (
    <div className="p-3">
      {files.map((file) => {
        const badge = STATUS_BADGES[file.status] || STATUS_BADGES.modified;
        const hunks = splitHunks(file.patch);
        return (
          <div key={file.path} className="mb-4">
            <div className="flex items-center gap-2 mb-1">
              <span
                className={`inline-flex items-center justify-center w-5 h-5 rounded text-[10px] font-bold ${badge.color}`}
              >
                {badge.label}
              </span>
              <span className="font-mono text-xs text-foreground truncate">
                {file.path}
              </span>
            </div>
            {hunks.length === 0 ? (
              <pre className="text-xs font-mono whitespace-pre-wrap text-muted-foreground pl-7">
                {file.patch || "(binary file)"}
              </pre>
            ) : (
              hunks.map((hunk, i) => (
                <div key={i} className="pl-7">
                  <div className="border border-border rounded-md overflow-hidden">
                    <div className="text-blue-400 font-mono text-xs px-2 py-1.5 bg-muted/30 border-b border-border">{hunk[0]}</div>
                    <div className="font-mono text-xs whitespace-pre-wrap px-2 py-1.5">
                      {hunk.slice(1).map((line, j) => (
                        <div key={j} className={lineColor(line)}>
                          {line || " "}
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              ))
            )}
          </div>
        );
      })}
    </div>
  );
}

/** Right-pane view of one stash entry: per-file checkboxes plus a restore
 *  action for the checked files. Mirrors CommitDiff's rendering, but each
 *  file is selectable instead of read-only. */
function StashDiff({
  stash,
  files,
  checked,
  busy,
  onToggle,
  onToggleAll,
  onRestore,
}: {
  stash: GitStash | null;
  files: GitDiffFile[] | null;
  checked: string[];
  busy: boolean;
  onToggle: (path: string) => void;
  onToggleAll: (paths: string[]) => void;
  onRestore: () => void;
}) {
  if (files === null) {
    return (
      <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
        Loading stash files…
      </div>
    );
  }
  const paths = files.map((f) => f.path);
  const allChecked = paths.length > 0 && paths.every((p) => checked.includes(p));
  return (
    <div className="flex flex-col h-full min-h-0">
      <div className="px-3 py-2 border-b border-border flex items-center justify-between gap-2 shrink-0">
        <div className="min-w-0">
          <div className="text-xs font-mono text-purple-400">{stash?.ref ?? "stash"}</div>
          <div className="text-[11px] text-muted-foreground truncate">
            {stash ? stashLabel(stash.message) : ""}
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {files.length > 0 && (
            <label className="flex items-center gap-1 text-[11px] text-muted-foreground cursor-pointer select-none">
              <input
                type="checkbox"
                checked={allChecked}
                onChange={() => onToggleAll(paths)}
                aria-label="Select all stash files"
              />
              All
            </label>
          )}
          <button
            onClick={onRestore}
            disabled={busy || checked.length === 0}
            className="text-[11px] px-2 py-1 rounded bg-purple-500/15 text-purple-300 hover:bg-purple-500/25 disabled:opacity-40"
          >
            Restore selected ({checked.length})
          </button>
        </div>
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto">
        {files.length === 0 ? (
          <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
            No file changes in this stash
          </div>
        ) : (
          <div className="p-3">
            {files.map((file) => {
              const badge = STATUS_BADGES[file.status] || STATUS_BADGES.modified;
              const hunks = splitHunks(file.patch);
              return (
                <div key={file.path} className="mb-4">
                  <label className="flex items-center gap-2 mb-1 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={checked.includes(file.path)}
                      onChange={() => onToggle(file.path)}
                      aria-label={`Select ${file.path}`}
                    />
                    <span
                      className={`inline-flex items-center justify-center w-5 h-5 rounded text-[10px] font-bold ${badge.color}`}
                    >
                      {badge.label}
                    </span>
                    <span className="font-mono text-xs text-foreground truncate">
                      {file.path}
                    </span>
                  </label>
                  {hunks.length === 0 ? (
                    <pre className="text-xs font-mono whitespace-pre-wrap text-muted-foreground pl-7">
                      {file.patch || "(binary file)"}
                    </pre>
                  ) : (
                    hunks.map((hunk, i) => (
                      <div key={i} className="pl-7">
                        <div className="border border-border rounded-md overflow-hidden">
                          <div className="text-blue-400 font-mono text-xs px-2 py-1.5 bg-muted/30 border-b border-border">
                            {hunk[0]}
                          </div>
                          <div className="font-mono text-xs whitespace-pre-wrap px-2 py-1.5">
                            {hunk.slice(1).map((line, j) => (
                              <div key={j} className={lineColor(line)}>
                                {line || " "}
                              </div>
                            ))}
                          </div>
                        </div>
                      </div>
                    ))
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
