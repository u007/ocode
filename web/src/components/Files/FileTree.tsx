import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Archive,
  Check,
  ChevronRight,
  Clipboard,
  Copy,
  File,
  FilePlus2,
  Files,
  Folder,
  FolderOpen,
  FolderPlus,
  GitCommit,
  Link2,
  Loader2,
  Lock,
  Pencil,
  Scissors,
  Search,
  Trash2,
  Unlock,
  X,
} from "lucide-react";
import { api, apiPath, authHeaders } from "@/api/client";
import { parseKeywords, matchesKeywords } from "@/lib/keywordFilter";
import SecretActionDialog from "./SecretActionDialog";

// Suppress unused-import errors for in-progress secret/file-tree work (dirty
// working tree from parallel feature). The build is strict (`noUnusedLocals`).
void Lock;
void Unlock;

interface FileNode {
  name: string;
  path: string;
  is_dir: boolean;
  children?: FileNode[];
  git_status?: string;
}

interface FileTreeResponse {
  children: FileNode[];
  truncated: boolean;
  is_git_repo?: boolean;
}

interface FileSearchResult {
  path: string;
  line: number;
  text: string;
}

interface FileSearchResponse {
  results: FileSearchResult[];
  truncated: boolean;
  total: number;
  has_more?: boolean;
  capped?: boolean;
}

type SecretMode = "encrypt" | "decrypt";

// All actions the file-tree context menu can perform, shared by every node so
// the menu can act on either the right-clicked node or the whole multi-selection.
interface FileMenuActions {
  isGitRepo: boolean;
  selectedPaths: Set<string>;
  clipboard: { op: "copy" | "cut"; paths: string[] } | null;
  toggleSelect: (p: string) => void;
  rangeSelect: (paths: string[]) => void;
  setSelection: (paths: string[]) => void;
  open: (p: string) => void;
  copyPath: (p: string) => void;
  copy: (paths: string[]) => void;
  cut: (paths: string[]) => void;
  paste: (destDir: string) => void;
  remove: (paths: string[]) => void;
  rename: (path: string) => void;
  newFile: (dir: string) => void;
  newFolder: (dir: string) => void;
  duplicate: (path: string) => void;
  secret: (path: string, name: string, isDir: boolean, mode: SecretMode) => void;
  gitStage: (paths: string[]) => void;
  gitUnstage: (paths: string[]) => void;
  gitDiscard: (paths: string[]) => void;
  gitStash: (paths: string[]) => void;
  gitCommit: (paths: string[]) => void;
}

interface FileTreeProps {
  onOpenFile: (path: string, projectRoot?: string, line?: number, query?: string) => void;
  projectPath?: string;
}

const langIcons: Record<string, string> = {
  ts: "🔷",
  tsx: "⚛️",
  js: "🟨",
  jsx: "⚛️",
  go: "🐹",
  py: "🐍",
  rs: "🦀",
  json: "📋",
  md: "📝",
  css: "🎨",
  html: "🌐",
  yaml: "⚙️",
  toml: "⚙️",
};

// Tree responses keep paths relative to their selected root so they can be
// opened in the editor with the same projectRoot. Expansion requests are
// different: the server must receive the selected root as well, otherwise a
// relative child path is resolved against its default workDir.
export function treePathForRequest(projectRoot: string | undefined, nodePath: string): string {
  if (!projectRoot) return nodePath;
  const root = projectRoot.replace(/[\\/]+$/, "");
  const relative = nodePath.replace(/^[\\/]+/, "");
  if (!relative || relative === ".") return projectRoot;
  return `${root}/${relative}`;
}

function parentDir(p: string): string {
  const i = p.lastIndexOf("/");
  return i >= 0 ? p.slice(0, i) : p;
}

// Parse the two git-status columns into coarse flags for menu visibility.
function gitFlags(status?: string): { staged: boolean; working: boolean; tracked: boolean } {
  if (!status) return { staged: false, working: false, tracked: true };
  const staged = status[0] !== " " && status[0] !== "?" && status[0] !== "U";
  const working = status[1] !== " " || status === "??";
  const tracked = status !== "??" && status[0] !== "?";
  return { staged, working, tracked };
}

function nodeMatchesKeywords(node: FileNode, keywords: string[]): boolean {
  return matchesKeywords(`${node.name} ${node.path}`, keywords);
}

function filterTreeNodes(nodes: FileNode[], keywords: string[]): FileNode[] {
  const out: FileNode[] = [];
  for (const n of nodes) {
    const childFiltered = n.children ? filterTreeNodes(n.children, keywords) : [];
    const selfMatch = nodeMatchesKeywords(n, keywords);
    if (selfMatch || childFiltered.length > 0) {
      out.push({
        ...n,
        children: childFiltered.length > 0 ? childFiltered : undefined,
      });
    }
  }
  return out;
}

function countTreeNodes(nodes: FileNode[]): number {
  let c = 0;
  for (const n of nodes) {
    c += 1;
    if (n.children) c += countTreeNodes(n.children);
  }
  return c;
}

function FileIcon({ name, isDir, expanded }: { name: string; isDir: boolean; expanded: boolean }) {
  if (isDir) {
    return expanded ? (
      <FolderOpen className="w-4 h-4 text-amber-500 shrink-0" />
    ) : (
      <Folder className="w-4 h-4 text-amber-500 shrink-0" />
    );
  }
  const ext = name.split(".").pop()?.toLowerCase() || "";
  const icon = langIcons[ext];
  if (icon) {
    return <span className="w-4 h-4 text-[10px] shrink-0 leading-none">{icon}</span>;
  }
  return <File className="w-4 h-4 text-blue-400 shrink-0" />;
}

function GitBadge({ status }: { status: string }) {
  const map: Record<string, { label: string; cls: string }> = {
    M: { label: "M", cls: "bg-amber-500/20 text-amber-400 border-amber-500/30" },
    "?": { label: "N", cls: "bg-emerald-500/20 text-emerald-400 border-emerald-500/30" },
    A: { label: "A", cls: "bg-emerald-500/20 text-emerald-400 border-emerald-500/30" },
    D: { label: "D", cls: "bg-red-500/20 text-red-400 border-red-500/30" },
    R: { label: "R", cls: "bg-purple-500/20 text-purple-400 border-purple-500/30" },
  };
  const cfg = map[status] || { label: status, cls: "bg-accent text-accent-foreground border-border" };
  const title =
    status === "?"
      ? "Untracked (new)"
      : status === "M"
        ? "Modified"
        : status === "A"
          ? "Added"
          : status === "D"
            ? "Deleted"
            : status === "R"
              ? "Renamed"
              : status;
  return (
    <span
      title={title}
      className={`inline-flex items-center justify-center w-4 h-4 text-[9px] font-bold rounded border shrink-0 ${cfg.cls}`}
    >
      {cfg.label}
    </span>
  );
}

function highlightKeywords(text: string, keywords: string[]): React.ReactNode {
  if (keywords.length === 0) return text;
  const lower = text.toLowerCase();
  const ranges: [number, number][] = [];
  for (const kw of keywords) {
    const lk = kw.toLowerCase();
    let idx = 0;
    while (true) {
      const pos = lower.indexOf(lk, idx);
      if (pos === -1) break;
      ranges.push([pos, pos + lk.length]);
      idx = pos + lk.length;
    }
  }
  if (ranges.length === 0) return text;
  ranges.sort((a, b) => a[0] - b[0]);
  const merged: [number, number][] = [ranges[0]];
  for (let i = 1; i < ranges.length; i++) {
    const last = merged[merged.length - 1];
    const cur = ranges[i];
    if (cur[0] <= last[1]) last[1] = Math.max(last[1], cur[1]);
    else merged.push(cur);
  }
  const parts: React.ReactNode[] = [];
  let lastIdx = 0;
  for (const [s, e] of merged) {
    if (s > lastIdx) parts.push(text.slice(lastIdx, s));
    parts.push(
      <mark key={`${s}-${e}`} className="bg-yellow-500/30 text-yellow-200 rounded px-0.5">
        {text.slice(s, e)}
      </mark>,
    );
    lastIdx = e;
  }
  if (lastIdx < text.length) parts.push(text.slice(lastIdx));
  return <>{parts}</>;
}

interface TreeNodeProps {
  node: FileNode;
  depth: number;
  selectedPath: string | null;
  siblings: FileNode[];
  lastClickedPath: string | null;
  onPlainClick: (node: FileNode, e: React.MouseEvent) => void;
  projectRoot?: string;
  forceExpanded?: boolean;
  menu: FileMenuActions;
}

function TreeNode({
  node,
  depth,
  selectedPath,
  siblings,
  lastClickedPath,
  onPlainClick,
  projectRoot,
  forceExpanded,
  menu,
}: TreeNodeProps) {
  const [expanded, setExpanded] = useState(!!forceExpanded);
  const [children, setChildren] = useState<FileNode[] | null>(node.children ?? null);
  const [loadingChildren, setLoadingChildren] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (forceExpanded) {
      setExpanded(true);
      if (node.children !== undefined) setChildren(node.children ?? []);
    }
  }, [forceExpanded, node.children]);

  useEffect(() => {
    if (!node.is_dir || !expanded || children !== null || forceExpanded) return;
    const controller = new AbortController();
    abortRef.current = controller;
    setLoadingChildren(true);
    (async () => {
      try {
        const res = await fetch(
          apiPath(
            `/api/files/tree?path=${encodeURIComponent(treePathForRequest(projectRoot, node.path))}&depth=1`,
          ),
          { headers: authHeaders(), signal: controller.signal },
        );
        if (!res.ok) throw new Error("Failed to load directory");
        const data: FileTreeResponse = await res.json();
        if (data.truncated) {
          console.warn(`File tree truncated under ${node.path}; not all entries were loaded`);
        }
        setChildren(data.children);
      } catch (err) {
        if ((err as Error).name !== "AbortError") {
          console.error("File tree children error:", err);
        }
      } finally {
        if (!controller.signal.aborted) setLoadingChildren(false);
      }
    })();
    return () => controller.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [expanded, node.is_dir, node.path]);

  const toggle = () => {
    if (forceExpanded) return;
    if (expanded) {
      abortRef.current?.abort();
      setLoadingChildren(false);
    }
    setExpanded(!expanded);
  };

  const requestPath = treePathForRequest(projectRoot, node.path);
  const selected = menu.selectedPaths.has(node.path);
  const flags = gitFlags(node.git_status);
  const isDir = node.is_dir;
  // Directories have no per-node git_status, but git add/reset/discard on a
  // directory operate on every file beneath it, so enable those actions for
  // folders unconditionally (within a repo).
  const stageEnabled = isDir || flags.working;
  const unstageEnabled = isDir || flags.staged;
  const discardEnabled = isDir || (flags.working && flags.tracked);

  // Effective targets for this menu: the whole selection when the node is part
  // of it, otherwise just this node (and the selection is narrowed to it so
  // the checkbox state matches what the actions will operate on).
  const effectivePaths = menu.selectedPaths.has(node.path)
    ? Array.from(menu.selectedPaths)
    : [node.path];

  const handleRowClick = (e: React.MouseEvent) => {
    if (e.shiftKey && lastClickedPath && siblings.some((s) => s.path === lastClickedPath)) {
      const idxA = siblings.findIndex((s) => s.path === lastClickedPath);
      const idxB = siblings.findIndex((s) => s.path === node.path);
      if (idxA >= 0 && idxB >= 0) {
        const [lo, hi] = idxA < idxB ? [idxA, idxB] : [idxB, idxA];
        menu.rangeSelect(siblings.slice(lo, hi + 1).map((s) => s.path));
        return;
      }
    }
    if (e.metaKey || e.ctrlKey) {
      menu.toggleSelect(node.path);
      return;
    }
    onPlainClick(node, e);
  };

  const CheckBox = (
    <button
      type="button"
      aria-label={`${node.name} — ${selected ? "deselect" : "select"}`}
      onClick={(e) => {
        e.stopPropagation();
        menu.toggleSelect(node.path);
      }}
      className={`shrink-0 w-3.5 h-3.5 rounded-sm border flex items-center justify-center transition-colors ${
        selected ? "bg-primary border-primary text-primary-foreground" : "border-border hover:border-foreground/50"
      }`}
    >
      {selected && <Check className="w-3 h-3" />}
    </button>
  );

  const rowBase = `w-full justify-start h-7 px-2 text-xs gap-1.5 font-normal flex items-center transition-colors ${
    forceExpanded ? "cursor-default" : ""
  }`;
  const rowState = selected
    ? "bg-accent text-accent-foreground"
    : selectedPath === node.path
      ? "bg-muted text-foreground"
      : "text-muted-foreground hover:bg-muted";

  const openMenu = () => {
    if (!menu.selectedPaths.has(node.path)) menu.setSelection([node.path]);
  };

  const dirItems = (
    <>
      {menu.isGitRepo && (
        <>
          <ContextMenuItem onSelect={() => menu.gitStage(effectivePaths)} disabled={!stageEnabled}>
            <GitCommit className="w-3.5 h-3.5 mr-2" /> Stage
          </ContextMenuItem>
          <ContextMenuItem onSelect={() => menu.gitUnstage(effectivePaths)} disabled={!unstageEnabled}>
            <GitCommit className="w-3.5 h-3.5 mr-2 opacity-60" /> Unstage
          </ContextMenuItem>
          <ContextMenuItem onSelect={() => menu.gitDiscard(effectivePaths)} disabled={!discardEnabled}>
            <GitCommit className="w-3.5 h-3.5 mr-2 opacity-60" /> Discard changes
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem onSelect={() => menu.gitStash(effectivePaths)}>
            <Archive className="w-3.5 h-3.5 mr-2" /> Stash…
          </ContextMenuItem>
          <ContextMenuItem onSelect={() => menu.gitCommit(effectivePaths)}>
            <GitCommit className="w-3.5 h-3.5 mr-2" /> Commit…
          </ContextMenuItem>
          <ContextMenuSeparator />
        </>
      )}
      <ContextMenuItem onSelect={() => menu.copy(effectivePaths)}>
        <Copy className="w-3.5 h-3.5 mr-2" /> Copy
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => menu.cut(effectivePaths)}>
        <Scissors className="w-3.5 h-3.5 mr-2" /> Cut
      </ContextMenuItem>
      {menu.clipboard && (
        <ContextMenuItem onSelect={() => menu.paste(isDir ? requestPath : parentDir(requestPath))}>
          <Clipboard className="w-3.5 h-3.5 mr-2" /> Paste
        </ContextMenuItem>
      )}
      <ContextMenuItem onSelect={() => menu.duplicate(node.path)}>
        <Files className="w-3.5 h-3.5 mr-2" /> Duplicate
      </ContextMenuItem>
      <ContextMenuSeparator />
      <ContextMenuItem onSelect={() => menu.newFile(isDir ? requestPath : parentDir(requestPath))}>
        <FilePlus2 className="w-3.5 h-3.5 mr-2" /> New file…
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => menu.newFolder(isDir ? requestPath : parentDir(requestPath))}>
        <FolderPlus className="w-3.5 h-3.5 mr-2" /> New folder…
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => menu.rename(node.path)}>
        <Pencil className="w-3.5 h-3.5 mr-2" /> Rename…
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => menu.copyPath(requestPath)}>
        <Link2 className="w-3.5 h-3.5 mr-2" /> Copy path
      </ContextMenuItem>
      <ContextMenuSeparator />
      <ContextMenuItem onSelect={() => menu.remove(effectivePaths)} className="text-red-400 focus:text-red-300">
        <Trash2 className="w-3.5 h-3.5 mr-2" /> Delete
      </ContextMenuItem>
    </>
  );

  if (isDir) {
    return (
      <div>
        <ContextMenu onOpenChange={(open) => open && openMenu()}>
          <ContextMenuTrigger asChild>
            <div className="flex items-center gap-0.5" style={{ paddingLeft: `${depth * 12 + 4}px` }}>
              {CheckBox}
              <button
                className={`${rowBase} ${rowState} flex-1 min-w-0`}
                onClick={(e) => {
                  handleRowClick(e);
                  if (!e.defaultPrevented) toggle();
                }}
              >
                <ChevronRight
                  className={`w-3 h-3 shrink-0 text-muted-foreground transition-transform ${
                    expanded ? "rotate-90" : ""
                  }`}
                />
                <FileIcon name={node.name} isDir expanded={expanded} />
                <span className="truncate flex-1 min-w-0">{node.name}</span>
                {loadingChildren && (
                  <Loader2 className="w-3 h-3 shrink-0 text-muted-foreground animate-spin" />
                )}
              </button>
            </div>
          </ContextMenuTrigger>
          <ContextMenuContent>{dirItems}</ContextMenuContent>
        </ContextMenu>
        {expanded &&
          children?.map((child) => (
            <TreeNode
              key={child.path}
              node={child}
              siblings={children}
              depth={depth + 1}
              selectedPath={selectedPath}
              lastClickedPath={lastClickedPath}
              onPlainClick={onPlainClick}
              projectRoot={projectRoot}
              forceExpanded={forceExpanded}
              menu={menu}
            />
          ))}
      </div>
    );
  }

  return (
    <ContextMenu onOpenChange={(open) => open && openMenu()}>
      <ContextMenuTrigger asChild>
        <div className="flex items-center gap-0.5" style={{ paddingLeft: `${depth * 12 + 16}px` }}>
          {CheckBox}
          <button className={`${rowBase} ${rowState} flex-1 min-w-0`} onClick={handleRowClick}>
            <FileIcon name={node.name} isDir={false} expanded={false} />
            <span className="truncate flex-1 min-w-0">{node.name}</span>
            {node.git_status && <GitBadge status={node.git_status} />}
          </button>
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent>{dirItems}</ContextMenuContent>
    </ContextMenu>
  );
}

interface PromptState {
  title: string;
  label: string;
  defaultValue: string;
  onConfirm: (value: string) => void;
}

// In-app replacement for window.confirm(): native JS dialogs are not supported
// in the Wails/WKWebView desktop webview (confirm() silently returns false), so
// the delete flow must use a rendered dialog like every other confirmation.
function ConfirmDeleteDialog({
  paths,
  onCancel,
  onConfirm,
}: {
  paths: string[];
  onCancel: () => void;
  onConfirm: () => Promise<void>;
}) {
  const [deleting, setDeleting] = useState(false);
  const shown = paths.slice(0, 5);
  const rest = paths.length - shown.length;
  return (
    <Dialog open onOpenChange={(o) => !o && !deleting && onCancel()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle className="text-sm">
            Delete {paths.length} item{paths.length === 1 ? "" : "s"}?
          </DialogTitle>
        </DialogHeader>
        <p className="text-xs text-muted-foreground">This cannot be undone.</p>
        <ul className="mt-2 space-y-0.5 max-h-36 overflow-y-auto rounded border border-border p-2">
          {shown.map((p) => (
            <li key={p} className="font-mono text-[11px] text-foreground truncate" title={p}>
              {p}
            </li>
          ))}
          {rest > 0 && <li className="text-[11px] text-muted-foreground/70">…and {rest} more</li>}
        </ul>
        <div className="flex justify-end gap-2 mt-3">
          <Button variant="ghost" onClick={onCancel} disabled={deleting}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            disabled={deleting}
            onClick={async () => {
              setDeleting(true);
              try {
                await onConfirm();
              } finally {
                setDeleting(false);
              }
            }}
          >
            {deleting ? "Deleting…" : "Delete"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function PromptDialog({ state, onCancel }: { state: PromptState | null; onCancel: () => void }) {
  const [value, setValue] = useState("");
  useEffect(() => {
    setValue(state?.defaultValue ?? "");
  }, [state]);
  if (!state) return null;
  return (
    <Dialog
      open={!!state}
      onOpenChange={(o) => !o && onCancel()}
    >
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle className="text-sm">{state.title}</DialogTitle>
        </DialogHeader>
        <Input
          autoFocus
          value={value}
          placeholder={state.label}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") state.onConfirm(value.trim());
            if (e.key === "Escape") onCancel();
          }}
        />
        <div className="flex justify-end gap-2 mt-3">
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button
            onClick={() => state.onConfirm(value.trim())}
            disabled={value.trim().length === 0}
          >
            OK
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export default function FileTree({ onOpenFile, projectPath }: FileTreeProps) {
  const [tree, setTree] = useState<FileNode[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set());
  const [lastClickedPath, setLastClickedPath] = useState<string | null>(null);
  const [clipboard, setClipboard] = useState<{ op: "copy" | "cut"; paths: string[] } | null>(null);
  const [isGitRepo, setIsGitRepo] = useState(false);
  const [refreshKey, setRefreshKey] = useState(0);
  const [extraPaths, setExtraPaths] = useState<string[]>([]);
  const [activeRoot, setActiveRoot] = useState<string | undefined>(projectPath);
  const [keyword, setKeyword] = useState("");
  const [searchMode, setSearchMode] = useState<"path" | "content">("path");
  const [contentQuery, setContentQuery] = useState("");
  const [contentResults, setContentResults] = useState<FileSearchResult[] | null>(null);
  const [contentLoading, setContentLoading] = useState(false);
  const [contentLoadingMore, setContentLoadingMore] = useState(false);
  const [contentHasMore, setContentHasMore] = useState(false);
  const [contentTotal, setContentTotal] = useState<number | null>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const scrollViewportRef = useRef<HTMLDivElement | null>(null);
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  const searchAbortRef = useRef<AbortController | null>(null);
  const searchGenRef = useRef(0);
  const fullTreeAbortRef = useRef<AbortController | null>(null);
  const [fullTree, setFullTree] = useState<FileNode[] | null>(null);
  const [fullTreeLoading, setFullTreeLoading] = useState(false);
  const [fullTreeTruncated, setFullTreeTruncated] = useState(false);
  const [secretAction, setSecretAction] = useState<{
    path: string;
    name: string;
    isDir: boolean;
    mode: SecretMode;
  } | null>(null);
  const [prompt, setPrompt] = useState<PromptState | null>(null);
  // Snapshot of paths awaiting delete confirmation. Native window.confirm() is
  // unsupported in the Wails/WKWebView desktop webview, so deletion goes through
  // a rendered dialog (ConfirmDeleteDialog) instead.
  const [pendingDelete, setPendingDelete] = useState<string[] | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const noticeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const keywords = useMemo(() => parseKeywords(keyword), [keyword]);
  const contentKeywords = useMemo(() => parseKeywords(contentQuery), [contentQuery]);

  const showNotice = useCallback((msg: string) => {
    setNotice(msg);
    if (noticeTimer.current) clearTimeout(noticeTimer.current);
    noticeTimer.current = setTimeout(() => setNotice(null), 3500);
  }, []);

  // The active project is the default root; extra allowed paths (configured
  // in Settings) are additional roots the user can switch the tree to.
  useEffect(() => {
    api
      .getPathsConfig()
      .then((cfg) => setExtraPaths(cfg.extra_allowed_paths || []))
      .catch((err) => console.error("Failed to load extra allowed paths:", err));
  }, []);

  useEffect(() => {
    setActiveRoot(projectPath);
  }, [projectPath]);

  const loadRoot = useCallback(
    (root: string) => {
      setLoading(true);
      const controller = new AbortController();
      (async () => {
        try {
          const query = `path=${encodeURIComponent(root)}&depth=1`;
          const res = await fetch(apiPath(`/api/files/tree?${query}`), {
            headers: authHeaders(),
            signal: controller.signal,
          });
          if (!res.ok) throw new Error("Failed to load file tree");
          const data: FileTreeResponse = await res.json();
          if (data.truncated) {
            console.warn("File tree truncated at the root; not all entries were loaded");
          }
          if (!controller.signal.aborted) {
            setTree(data.children);
            setIsGitRepo(!!data.is_git_repo);
          }
        } catch (err) {
          if ((err as Error).name !== "AbortError") console.error("File tree error:", err);
        } finally {
          if (!controller.signal.aborted) setLoading(false);
        }
      })();
      return () => controller.abort();
    },
    [],
  );

  const refresh = useCallback(() => {
    const root = activeRoot ?? projectPath;
    if (!root) {
      setRefreshKey((k) => k + 1);
      return;
    }
    loadRoot(root);
    setRefreshKey((k) => k + 1);
  }, [activeRoot, projectPath, loadRoot]);

  useEffect(() => {
    const root = activeRoot ?? projectPath;
    if (!root) {
      setTree([]);
      setLoading(false);
      setIsGitRepo(false);
      return;
    }
    return loadRoot(root);
  }, [activeRoot, projectPath, loadRoot]);

  useEffect(() => {
    setFullTree(null);
    setFullTreeTruncated(false);
  }, [activeRoot]);

  // Fetch full tree for keyword filtering.
  useEffect(() => {
    const q = keyword.trim();
    if (!q) return;
    const root = activeRoot ?? projectPath;
    if (!root) return;
    if (fullTree !== null) return;
    const controller = new AbortController();
    fullTreeAbortRef.current?.abort();
    fullTreeAbortRef.current = controller;
    setFullTreeLoading(true);
    (async () => {
      try {
        const query = `path=${encodeURIComponent(root)}&depth=0`;
        const res = await fetch(apiPath(`/api/files/tree?${query}`), { headers: authHeaders(), signal: controller.signal });
        if (!res.ok) throw new Error("Failed to load full file tree for filtering");
        const data: FileTreeResponse = await res.json();
        if (data.truncated) {
          console.warn("File tree truncated for filter; not all files are searchable");
          setFullTreeTruncated(true);
        }
        if (data.is_git_repo) setIsGitRepo(true);
        if (controller.signal.aborted) return;
        setFullTree(data.children);
      } catch (err) {
        if ((err as Error).name !== "AbortError") console.error("Full tree error:", err);
      } finally {
        if (!controller.signal.aborted) setFullTreeLoading(false);
      }
    })();
    return () => controller.abort();
  }, [keyword, activeRoot, projectPath, fullTree]);

  const fetchContentPage = useCallback(
    (offset: number, append: boolean) => {
      const root = activeRoot ?? projectPath;
      const q = contentQuery.trim();
      if (!root || q.length < 2) return;
      const gen = ++searchGenRef.current;
      if (append) setContentLoadingMore(true);
      else setContentLoading(true);
      const controller = new AbortController();
      searchAbortRef.current?.abort();
      searchAbortRef.current = controller;
      (async () => {
        try {
          const params = new URLSearchParams({
            path: root,
            query: q,
            offset: String(offset),
            limit: "50",
          });
          const res = await fetch(apiPath(`/api/files/search?${params.toString()}`), {
            headers: authHeaders(),
            signal: controller.signal,
          });
          if (!res.ok) throw new Error("Content search failed");
          const data: FileSearchResponse = await res.json();
          if (controller.signal.aborted || gen !== searchGenRef.current) return;
          setContentResults((prev) =>
            append ? [...(prev ?? []), ...data.results] : data.results,
          );
          setContentHasMore(!!data.has_more);
          setContentTotal(data.total ?? null);
        } catch (err) {
          if ((err as Error).name !== "AbortError") console.error("Content search error:", err);
        } finally {
          if (!controller.signal.aborted) {
            setContentLoading(false);
            setContentLoadingMore(false);
          }
        }
      })();
    },
    [activeRoot, projectPath, contentQuery],
  );

  useEffect(() => {
    if (searchMode !== "content") {
      setContentResults(null);
      setContentLoading(false);
      setContentLoadingMore(false);
      setContentHasMore(false);
      setContentTotal(null);
      searchGenRef.current++;
      searchAbortRef.current?.abort();
      return;
    }
    const q = contentQuery.trim();
    if (q.length < 2) {
      setContentResults(null);
      setContentLoading(false);
      setContentLoadingMore(false);
      setContentHasMore(false);
      setContentTotal(null);
      searchGenRef.current++;
      searchAbortRef.current?.abort();
      return;
    }
    setContentHasMore(false);
    setContentTotal(null);
    const timer = setTimeout(() => {
      fetchContentPage(0, false);
    }, 300);
    return () => {
      clearTimeout(timer);
    };
  }, [searchMode, contentQuery, activeRoot, projectPath, fetchContentPage]);

  useEffect(() => {
    if (searchMode !== "content" || !contentHasMore || contentLoading || contentLoadingMore) return;
    const sentinel = sentinelRef.current;
    const viewport =
      scrollViewportRef.current ||
      (document.querySelector("[data-radix-scroll-area-viewport]") as HTMLDivElement | null);
    if (!sentinel) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && contentHasMore && !contentLoading && !contentLoadingMore) {
          fetchContentPage(contentResults?.length ?? 0, true);
        }
      },
      { root: viewport, rootMargin: "200px", threshold: 0 },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [searchMode, contentHasMore, contentLoading, contentLoadingMore, contentResults?.length, activeRoot, projectPath, contentQuery, fetchContentPage]);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "f" && searchMode === "path") {
        e.preventDefault();
        setSearchMode("content");
        setTimeout(() => searchInputRef.current?.focus(), 0);
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [searchMode]);

  const rootOptions = useMemo(() => {
    const opts: { path: string; label: string }[] = [];
    if (projectPath) opts.push({ path: projectPath, label: projectPath.split("/").pop() || projectPath });
    for (const p of extraPaths) {
      if (p && !opts.some((o) => o.path === p)) {
        opts.push({ path: p, label: p.split("/").pop() || p });
      }
    }
    return opts;
  }, [projectPath, extraPaths]);

  const isFiltering = keywords.length > 0;
  const filteredTree = useMemo(() => {
    if (!isFiltering) return null;
    const source = fullTree ?? tree;
    return filterTreeNodes(source, keywords);
  }, [isFiltering, fullTree, tree, keywords]);
  const filteredCount = useMemo(
    () => (filteredTree ? countTreeNodes(filteredTree) : 0),
    [filteredTree],
  );

  const handleSelect = (path: string) => {
    setSelectedPath(path);
    if (searchMode === "content" && contentQuery.trim()) {
      onOpenFile(path, activeRoot, undefined, contentQuery.trim());
    } else {
      onOpenFile(path, activeRoot);
    }
  };
  void handleSelect;

  const onPlainClick = useCallback(
    (node: FileNode, _e: React.MouseEvent) => {
      setSelectedPath(node.path);
      setSelectedPaths(new Set([node.path]));
      setLastClickedPath(node.path);
      if (!node.is_dir) {
        if (searchMode === "content" && contentQuery.trim()) {
          onOpenFile(node.path, activeRoot, undefined, contentQuery.trim());
        } else {
          onOpenFile(node.path, activeRoot);
        }
      }
    },
    [searchMode, contentQuery, activeRoot, onOpenFile],
  );

  const toggleSelect = useCallback((p: string) => {
    setSelectedPaths((prev) => {
      const next = new Set(prev);
      if (next.has(p)) next.delete(p);
      else next.add(p);
      return next;
    });
    setLastClickedPath(p);
  }, []);

  const rangeSelect = useCallback((paths: string[]) => {
    setSelectedPaths(new Set(paths));
  }, []);

  const runGit = useCallback(
    async (fn: (paths: string[]) => Promise<unknown>, paths: string[]) => {
      try {
        await fn(paths);
        showNotice("Git operation complete");
        refresh();
      } catch (err) {
        showNotice(`Git error: ${(err as Error).message}`);
      }
    },
    [refresh, showNotice],
  );

  const runFs = useCallback(
    async (fn: () => Promise<unknown>): Promise<boolean> => {
      try {
        await fn();
        showNotice("Done");
        refresh();
        return true;
      } catch (err) {
        showNotice(`Error: ${(err as Error).message}`);
        return false;
      }
    },
    [refresh, showNotice],
  );

  const confirmDelete = useCallback(async () => {
    const paths = pendingDelete;
    if (!paths || paths.length === 0) return;
    const ok = await runFs(async () => {
      await api.fsDelete(paths, activeRoot);
      window.dispatchEvent(
        new CustomEvent("ocode:fs-delete", { detail: { paths, projectRoot: activeRoot } }),
      );
    });
    // Close and prune the selection only on success; on failure keep the dialog
    // open so the user can retry after seeing the error notice.
    if (ok) {
      setPendingDelete(null);
      setSelectedPaths((prev) => {
        if (prev.size === 0) return prev;
        const next = new Set(prev);
        for (const p of paths) next.delete(p);
        return next;
      });
    }
  }, [pendingDelete, activeRoot, runFs]);

  const menu: FileMenuActions = {
    isGitRepo,
    selectedPaths,
    clipboard,
    toggleSelect,
    rangeSelect,
    setSelection: (paths) => setSelectedPaths(new Set(paths)),
    open: (p) => onOpenFile(p, activeRoot),
    copyPath: (p) => {
      navigator.clipboard?.writeText(p).then(
        () => showNotice("Path copied"),
        () => showNotice("Copy failed"),
      );
    },
    copy: (paths) => {
      setClipboard({ op: "copy", paths });
      showNotice(`Copied ${paths.length} item(s)`);
    },
    cut: (paths) => {
      setClipboard({ op: "cut", paths });
      showNotice(`Cut ${paths.length} item(s)`);
    },
    paste: (destDir) => {
      if (!clipboard) return;
      const cut = clipboard.op === "cut";
      const paths = [...clipboard.paths];
      runFs(async () => {
        if (cut) {
          await api.fsMove(paths, destDir, activeRoot);
          setClipboard(null);
          window.dispatchEvent(new CustomEvent("ocode:fs-delete", { detail: { paths, projectRoot: activeRoot } }));
        } else {
          await api.fsCopy(paths, destDir, activeRoot);
        }
      });
    },
    remove: (paths) => {
      // Snapshot into dialog state; selection can change while the dialog is open.
      setPendingDelete(paths);
    },
    rename: (path) => {
      const name = path.split("/").pop() || "";
      setPrompt({
        title: "Rename",
        label: "New name",
        defaultValue: name,
        onConfirm: (value) => {
          setPrompt(null);
          if (!value) return;
          runFs(async () => {
            const res = (await api.fsRename(path, value, activeRoot)) as { path?: string };
            const dir = path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "";
            const newPath = res?.path ?? (dir ? `${dir}/${value}` : value);
            window.dispatchEvent(new CustomEvent("ocode:fs-rename", { detail: { oldPath: path, newPath, projectRoot: activeRoot } }));
          });
        },
      });
    },
    newFile: (dir) => {
      setPrompt({
        title: "New file",
        label: "File name",
        defaultValue: "newfile.txt",
        onConfirm: (value) => {
          setPrompt(null);
          if (!value) return;
          runFs(() => api.fsNewFile(`${dir}/${value}`, activeRoot));
        },
      });
    },
    newFolder: (dir) => {
      setPrompt({
        title: "New folder",
        label: "Folder name",
        defaultValue: "newfolder",
        onConfirm: (value) => {
          setPrompt(null);
          if (!value) return;
          runFs(() => api.fsNewFolder(`${dir}/${value}`, activeRoot));
        },
      });
    },
    duplicate: (path) => runFs(() => api.fsDuplicate(path, activeRoot)),
    secret: (path, name, isDir, mode) => setSecretAction({ path, name, isDir, mode }),
    gitStage: (paths) => runGit((p) => api.gitStage(p, activeRoot), paths),
    gitUnstage: (paths) => runGit((p) => api.gitUnstage(p, activeRoot), paths),
    gitDiscard: (paths) => runGit((p) => api.gitDiscard(p, activeRoot), paths),
    gitStash: (paths) =>
      setPrompt({
        title: "Stash changes",
        label: "Stash message (optional)",
        defaultValue: "",
        onConfirm: (value) => {
          setPrompt(null);
          runGit((p) => api.gitStash(value, p, activeRoot), paths);
        },
      }),
    gitCommit: (paths) =>
      setPrompt({
        title: "Commit changes",
        label: "Commit message",
        defaultValue: "",
        onConfirm: (value) => {
          setPrompt(null);
          if (!value) {
            showNotice("Commit message required");
            return;
          }
          runGit((p) => api.gitCommit(value, p, activeRoot), paths);
        },
      }),
  };

  return (
    <div className="flex flex-col h-full">
      {notice && (
        <div className="px-3 py-1 text-[11px] bg-accent text-accent-foreground border-b border-border shrink-0">
          {notice}
        </div>
      )}
      <div className="flex items-center justify-between px-3 h-9 border-b border-border shrink-0 gap-2">
        <h3 className="text-xs font-medium text-muted-foreground shrink-0">Files</h3>
        {selectedPaths.size > 0 && (
          <button
            type="button"
            onClick={() => setSelectedPaths(new Set())}
            className="text-[11px] text-muted-foreground hover:text-foreground shrink-0"
            title="Clear selection"
          >
            {selectedPaths.size} selected ✕
          </button>
        )}
        {rootOptions.length > 1 && (
          <Select value={activeRoot} onValueChange={setActiveRoot}>
            <SelectTrigger className="h-6 w-auto min-w-0 max-w-[60%] text-xs px-2 gap-1 border-none bg-transparent">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {rootOptions.map((o) => (
                <SelectItem key={o.path} value={o.path} className="text-xs">
                  {o.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </div>
      <div className="px-2 py-1.5 border-b border-border shrink-0 space-y-1.5">
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={() => setSearchMode(searchMode === "content" ? "path" : "content")}
            className={`shrink-0 h-7 px-2 text-[11px] font-medium rounded border transition-colors ${
              searchMode === "content"
                ? "bg-accent text-accent-foreground border-border"
                : "bg-transparent text-muted-foreground border-border hover:bg-accent hover:text-accent-foreground"
            }`}
            title={searchMode === "content" ? "Searching file contents (click for path filter)" : "Filtering by path (click for content search)"}
          >
            {searchMode === "content" ? "Content" : "Path"}
          </button>
          <div className="relative flex-1">
            <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground/50 pointer-events-none" />
            <Input
              ref={searchInputRef}
              value={searchMode === "path" ? keyword : contentQuery}
              onChange={(e) => (searchMode === "path" ? setKeyword(e.target.value) : setContentQuery(e.target.value))}
              placeholder={searchMode === "content" ? "Search file contents..." : "Filter by keywords..."}
              className="h-7 pl-7 pr-7 text-xs"
            />
            {(searchMode === "path" ? keyword : contentQuery) && (
              <button
                type="button"
                onClick={() => (searchMode === "path" ? setKeyword("") : setContentQuery(""))}
                className="absolute right-1 top-1/2 -translate-y-1/2 p-1 rounded hover:bg-accent text-muted-foreground hover:text-accent-foreground"
                aria-label="Clear filter"
              >
                <X className="w-3.5 h-3.5" />
              </button>
            )}
          </div>
        </div>
        <div className="text-[11px] text-muted-foreground/70 hidden sm:block">
          {searchMode === "content" ? "Tip: Ctrl+F toggles content search · keywords AND, case-insensitive" : "Tip: checkbox or ⌘/Ctrl-click to multi-select · right-click for actions"}
        </div>
        {isFiltering && searchMode === "path" && (
          <div className="mt-1 text-[11px] text-muted-foreground truncate">
            {fullTreeLoading
              ? "Searching…"
              : `${filteredCount} match${filteredCount === 1 ? "" : "es"}${fullTreeTruncated ? " — results may be incomplete (truncated)" : ""}`}
          </div>
        )}
        {searchMode === "content" && contentQuery.trim().length >= 2 && (
          <div className="mt-1 text-[11px] text-muted-foreground truncate">
            {contentLoading
              ? "Searching contents…"
              : contentResults
                ? `${contentResults.length} match${contentResults.length === 1 ? "" : "es"}${contentTotal !== null ? ` of ${contentTotal}` : ""}${contentHasMore ? " — scroll for more" : ""}${contentLoadingMore ? " (loading…)" : ""}${(contentResults.length >= 5000 ? " (capped at 5000)" : "")}`
                : ""}
          </div>
        )}
      </div>
      <ScrollArea className="flex-1" ref={(el) => {
        if (el) {
          const vp = el.querySelector("[data-radix-scroll-area-viewport]") as HTMLDivElement | null;
          if (vp) scrollViewportRef.current = vp;
        }
      }}>
        {searchMode === "content" && contentQuery.trim().length >= 2 ? (
          contentLoading ? (
            <div className="flex items-center justify-center py-12 text-xs text-muted-foreground gap-2">
              <Loader2 className="w-4 h-4 animate-spin" />
              Searching contents…
            </div>
          ) : !contentResults || contentResults.length === 0 ? (
            <div className="px-4 py-12 text-center text-xs text-muted-foreground">No matching content</div>
          ) : (
            <div className="py-1" key={refreshKey}>
              {contentResults.map((r, idx) => (
                <button
                  key={`${r.path}:${r.line}:${idx}`}
                  onClick={() => {
                    setSelectedPath(r.path);
                    onOpenFile(r.path, activeRoot, r.line, contentQuery.trim());
                  }}
                  className={`w-full text-left px-3 py-1.5 hover:bg-muted transition-colors border-l-2 ${
                    selectedPath === r.path ? "bg-muted border-border" : "border-transparent"
                  }`}
                  title={r.text}
                >
                  <div className="text-[11px] font-mono text-amber-300 truncate">{r.path}:{r.line}</div>
                  <div className="text-xs text-foreground font-mono mt-0.5 whitespace-pre-wrap break-words" style={{ wordBreak: "break-all" }}>
                    {highlightKeywords(r.text, contentKeywords)}
                  </div>
                </button>
              ))}
              <div ref={sentinelRef} className="h-4" />
              {contentLoadingMore && (
                <div className="flex items-center justify-center py-2 text-xs text-muted-foreground gap-2">
                  <Loader2 className="w-3 h-3 animate-spin" /> Loading more…
                </div>
              )}
              {!contentHasMore && contentResults.length > 0 && (
                <div className="text-center py-2 text-[11px] text-muted-foreground/60">— end of results —</div>
              )}
            </div>
          )
        ) : loading ? (
          <div className="flex items-center justify-center py-12 text-xs text-muted-foreground">Loading…</div>
        ) : !(activeRoot ?? projectPath) ? (
          <div className="px-4 py-12 text-center text-xs text-muted-foreground">
            <FolderOpen className="w-8 h-8 mx-auto mb-2 text-muted-foreground/60" />
            <p>No project added yet</p>
            <p className="mt-1">Add a project from the sidebar to browse files</p>
          </div>
        ) : isFiltering ? (
          fullTreeLoading ? (
            <div className="flex items-center justify-center py-12 text-xs text-muted-foreground gap-2">
              <Loader2 className="w-4 h-4 animate-spin" />
              Filtering…
            </div>
          ) : !filteredTree || filteredTree.length === 0 ? (
            <div className="px-4 py-12 text-center text-xs text-muted-foreground">No matching files</div>
          ) : (
            <div className="py-1" key={refreshKey}>
              {filteredTree.map((node) => (
                <TreeNode
                  key={node.path}
                  node={node}
                  siblings={filteredTree}
                  depth={0}
                  selectedPath={selectedPath}
                  lastClickedPath={lastClickedPath}
                  onPlainClick={onPlainClick}
                  projectRoot={activeRoot}
                  forceExpanded
                  menu={menu}
                />
              ))}
            </div>
          )
        ) : tree.length === 0 ? (
          <div className="px-4 py-12 text-center text-xs text-muted-foreground">No files</div>
        ) : (
          <div className="py-1" key={refreshKey}>
            {tree.map((node) => (
              <TreeNode
                key={node.path}
                node={node}
                siblings={tree}
                depth={0}
                selectedPath={selectedPath}
                lastClickedPath={lastClickedPath}
                onPlainClick={onPlainClick}
                projectRoot={activeRoot}
                menu={menu}
              />
            ))}
          </div>
        )}
      </ScrollArea>
      {secretAction && (
        <SecretActionDialog
          open
          onOpenChange={(open) => !open && setSecretAction(null)}
          path={secretAction.path}
          name={secretAction.name}
          isDir={secretAction.isDir}
          mode={secretAction.mode}
        />
      )}
      <PromptDialog state={prompt} onCancel={() => setPrompt(null)} />
      {pendingDelete && (
        <ConfirmDeleteDialog
          paths={pendingDelete}
          onCancel={() => setPendingDelete(null)}
          onConfirm={confirmDelete}
        />
      )}
    </div>
  );
}
