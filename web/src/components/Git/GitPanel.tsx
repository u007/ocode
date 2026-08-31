import { useCallback, useEffect, useState } from "react";
import {
  RefreshCw,
  GitBranch,
  ArrowDownToLine,
  ArrowUpToLine,
  Trash2,
  ExternalLink,
  GitCommitVertical,
} from "lucide-react";
import { api } from "@/api/client";
import { eventBus } from "@/lib/eventBus";
import type {
  GitCommit,
  GitDiffFile,
  GitHunkAction,
  GitWorkspace,
} from "@/api/types";

const REFRESH_INTERVAL = 10000;

const STATUS_BADGES: Record<string, { label: string; color: string }> = {
  modified: { label: "M", color: "bg-yellow-500/20 text-yellow-400" },
  added: { label: "A", color: "bg-green-500/20 text-green-400" },
  deleted: { label: "D", color: "bg-red-500/20 text-red-400" },
  renamed: { label: "R", color: "bg-blue-500/20 text-blue-400" },
  untracked: { label: "?", color: "bg-muted/20 text-muted-foreground" },
};

interface Props {
  onOpenFile?: (path: string, projectRoot?: string) => void;
  projectPath?: string;
  /** True while the Git view is frontmost. The panel is force-mounted so its
   *  DOM survives view switches; without this gate it polls and refetches the
   *  whole workspace every 10s forever, even while the user is chatting. */
  active?: boolean;
}

type Selection =
  | { kind: "file"; path: string; staged: boolean }
  | { kind: "commit"; hash: string }
  | null;

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

export default function GitPanel({ onOpenFile, projectPath, active = true }: Props) {
  const [workspace, setWorkspace] = useState<GitWorkspace | null>(null);
  const [commits, setCommits] = useState<GitCommit[]>([]);
  const [commitDiff, setCommitDiff] = useState<GitDiffFile[] | null>(null);
  const [selection, setSelection] = useState<Selection>(null);
  const [commitMessage, setCommitMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const load = useCallback(async () => {
    setRefreshing(true);
    setError(null);
    try {
      const [ws, log] = await Promise.all([
        api.getGitWorkspace(projectPath),
        api.gitLog(projectPath),
      ]);
      setWorkspace(ws);
      setCommits(log);
      // Selected file may have moved between panes or disappeared (discard):
      // re-resolve against the fresh snapshot.
      setSelection((sel) => {
        if (!sel || sel.kind === "commit") return sel;
        const inStaged = ws.staged.some((f) => f.path === sel.path);
        const inUnstaged = ws.unstaged.some((f) => f.path === sel.path);
        if (!inStaged && !inUnstaged) return null;
        return { kind: "file", path: sel.path, staged: inStaged };
      });
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "Failed to load git workspace",
      );
    } finally {
      setRefreshing(false);
    }
  }, [projectPath]);

  useEffect(() => {
    load();
    if (!active) return;
    const interval = setInterval(load, REFRESH_INTERVAL);
    return () => clearInterval(interval);
  }, [load, active]);

  // The server pushes git_status bus events whenever the repo changes (also
  // after the TUI or the file-tree context menu mutate it) — stay fresh.
  useEffect(() => {
    return eventBus.on("git_status", (env) => {
      if (!projectPath || env.project === projectPath) load();
    });
  }, [load, projectPath]);

  const runMutation = useCallback(
    async (fn: () => Promise<unknown>) => {
      setBusy(true);
      setError(null);
      try {
        await fn();
        await load();
      } catch (e) {
        setError(e instanceof Error ? e.message : "git action failed");
      } finally {
        setBusy(false);
      }
    },
    [load],
  );

  const stageFile = (path: string) =>
    runMutation(() => api.gitStage([path], projectPath));
  const unstageFile = (path: string) =>
    runMutation(() => api.gitUnstage([path], projectPath));
  const discardFile = (path: string, untracked: boolean) =>
    untracked
      ? runMutation(() =>
          api.gitHunk(
            { path, hunk_index: 0, action: "discard", staged: false },
            projectPath,
          ),
        )
      : runMutation(() => api.gitDiscard([path], projectPath));
  const stageAll = (paths: string[]) =>
    runMutation(() => api.gitStage(paths, projectPath));
  const unstageAll = (paths: string[]) =>
    runMutation(() => api.gitUnstage(paths, projectPath));

  const hunkAction = useCallback(
    async (file: GitDiffFile, hunkIndex: number, action: GitHunkAction, staged: boolean) => {
      setBusy(true);
      setError(null);
      try {
        const ws = await api.gitHunk(
          { path: file.path, hunk_index: hunkIndex, action, staged },
          projectPath,
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
      } catch (e) {
        setError(e instanceof Error ? e.message : "hunk action failed");
      } finally {
        setBusy(false);
      }
    },
    [projectPath],
  );

  const commit = () => {
    if (!commitMessage.trim()) {
      setError("Commit message is required");
      return;
    }
    runMutation(async () => {
      await api.gitCommit(commitMessage, [], projectPath);
      setCommitMessage("");
      setSelection(null);
      setCommitDiff(null);
    });
  };

  const selectFile = (file: GitDiffFile, staged: boolean) => {
    setSelection({ kind: "file", path: file.path, staged });
    setCommitDiff(null);
  };

  const selectCommit = async (c: GitCommit) => {
    setSelection({ kind: "commit", hash: c.hash });
    try {
      const files = await api.gitShow(c.hash, projectPath);
      setCommitDiff(files);
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to load commit diff");
      setCommitDiff(null);
    }
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

  // Which diff is shown in the right pane?
  let shownFile: GitDiffFile | null = null;
  let shownStaged = false;
  if (selection && selection.kind === "file") {
    shownFile =
      unstagedFiles.find((f) => f.path === selection.path) ??
      stagedFiles.find((f) => f.path === selection.path) ??
      null;
    shownStaged =
      shownFile !== null &&
      stagedFiles.some((f) => f.path === (shownFile as GitDiffFile).path);
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="px-3 py-2 border-b border-border flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <span className="text-xs text-muted-foreground uppercase tracking-wider shrink-0">
            Git
          </span>
          <span className="text-xs text-muted-foreground font-mono truncate flex items-center gap-1">
            <GitBranch className="w-3.5 h-3.5 shrink-0" />
            {status.branch || "no branch"}
          </span>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <span className="text-xs text-muted-foreground">
            {stagedFiles.length} staged · {unstagedFiles.length} unstaged
          </span>
          <button
            onClick={load}
            disabled={refreshing}
            title="Refresh"
            className="p-1 rounded hover:bg-muted/60 text-muted-foreground hover:text-foreground disabled:opacity-50"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${refreshing ? "animate-spin" : ""}`} />
          </button>
        </div>
      </div>

      {error && (
        <div className="px-3 py-1.5 text-xs bg-red-500/10 text-red-400 border-b border-border">
          {error}
        </div>
      )}

      {/* Body */}
      <div className="flex flex-1 min-h-0">
        {/* Left: staged / unstaged / commits */}
        <div className="w-72 md:w-80 shrink-0 border-r border-border flex flex-col min-h-0 bg-muted/10">
          <FileSection
            title="Staged changes"
            stagedPane
            files={stagedFiles}
            selected={selection}
            busy={busy}
            onSelect={(f) => selectFile(f, true)}
            onSectionAction={
              stagedFiles.length > 1
                ? { label: "Unstage all", fn: () => unstageAll(stagedFiles.map((f) => f.path)) }
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
            files={unstagedFiles}
            selected={selection}
            busy={busy}
            onSelect={(f) => selectFile(f, false)}
            onSectionAction={
              unstagedFiles.length > 1
                ? { label: "Stage all", fn: () => stageAll(unstagedFiles.map((f) => f.path)) }
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
          <div className="flex-1 min-h-0 flex flex-col border-t border-border">
            <div className="px-3 py-1.5 text-xs uppercase tracking-wider text-muted-foreground flex items-center justify-between">
              <span>Commits</span>
              <span className="text-muted-foreground/60 normal-case">{commits.length}</span>
            </div>
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
          </div>
        </div>

        {/* Right: diff pane */}
        <div className="flex-1 min-h-0 overflow-y-auto">
          {selection?.kind === "commit" && commitDiff ? (
            <CommitDiff files={commitDiff} />
          ) : shownFile ? (
            <FileDiff
              file={shownFile}
              staged={shownStaged}
              busy={busy}
              onHunk={(i, a) => hunkAction(shownFile, i, a, shownStaged)}
              onOpen={onOpenFile ? () => onOpenFile(shownFile!.path, projectPath) : undefined}
            />
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
          disabled={busy || !status.has_changes || !commitMessage.trim()}
          className="h-9 px-4 rounded-md bg-primary text-primary-foreground text-sm font-medium hover:opacity-90 disabled:opacity-40"
        >
          Commit
        </button>
      </div>
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
  onSelect,
  onSectionAction,
  rowActions,
}: {
  title: string;
  stagedPane: boolean;
  files: GitDiffFile[];
  selected: Selection;
  busy: boolean;
  onSelect: (f: GitDiffFile) => void;
  onSectionAction?: { label: string; fn: () => void };
  rowActions?: (f: GitDiffFile) => React.ReactNode;
}) {
  return (
    <div className="shrink-0 max-h-[34%] min-h-0 flex flex-col">
      <div className="px-3 py-1.5 text-xs uppercase tracking-wider text-muted-foreground flex items-center justify-between">
        <span>
          {title} <span className="text-foreground/50">({files.length})</span>
        </span>
        {onSectionAction && (
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
      <div className="overflow-y-auto min-h-0">
        {files.length === 0 ? (
          <div className="px-3 py-1 text-xs text-muted-foreground/60 italic">
            Nothing to show
          </div>
        ) : (
          <div className="divide-y divide-border/60">
            {files.map((f) => {
              const badge = STATUS_BADGES[f.status] || STATUS_BADGES.modified;
              const isSelected =
                selected?.kind === "file" &&
                selected.path === f.path &&
                selected.staged === stagedPane;
              return (
                <div
                  key={f.path}
                  onClick={() => onSelect(f)}
                  className={`group flex items-center gap-1.5 pl-2 pr-1.5 py-1 text-sm cursor-pointer hover:bg-muted/50 ${
                    isSelected ? "bg-muted" : ""
                  }`}
                >
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
    </div>
  );
}

/** Working-tree diff for one file, with per-hunk stage/unstage/reverse. */
function FileDiff({
  file,
  staged,
  busy,
  onHunk,
  onOpen,
}: {
  file: GitDiffFile;
  staged: boolean;
  busy: boolean;
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
          <div key={i} className="mb-3">
            {/* hunk header + actions */}
            <div className="flex items-center gap-2 mb-0.5">
              <span className="text-blue-400 font-mono text-xs shrink-0">
                {hunk[0]}
              </span>
              <div className="flex-1" />
              {staged ? (
                <button
                  onClick={() => onHunk(i, "unstage")}
                  disabled={busy}
                  className="text-[11px] px-1.5 py-0.5 rounded bg-blue-500/15 text-blue-400 hover:bg-blue-500/25 disabled:opacity-40 shrink-0"
                >
                  Unstage hunk
                </button>
              ) : (
                <>
                  <button
                    onClick={() => onHunk(i, "stage")}
                    disabled={busy}
                    className="text-[11px] px-1.5 py-0.5 rounded bg-green-500/15 text-green-400 hover:bg-green-500/25 disabled:opacity-40 shrink-0"
                  >
                    {isUntracked ? "Stage file" : "Stage hunk"}
                  </button>
                  <button
                    onClick={() => onHunk(i, "discard")}
                    disabled={busy}
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
            <div className="font-mono text-xs whitespace-pre-wrap">
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
                  <div className="text-blue-400 font-mono text-xs">{hunk[0]}</div>
                  <div className="font-mono text-xs whitespace-pre-wrap">
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
      })}
    </div>
  );
}