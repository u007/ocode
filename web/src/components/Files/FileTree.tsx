import { useEffect, useMemo, useRef, useState } from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ChevronRight, File, Folder, FolderOpen, Loader2 } from "lucide-react";
import { api, apiPath, authHeaders } from "@/api/client";

interface FileNode {
  name: string;
  path: string;
  is_dir: boolean;
  children?: FileNode[];
}

interface FileTreeResponse {
  children: FileNode[];
  truncated: boolean;
}

interface FileTreeProps {
  onOpenFile: (path: string, projectRoot?: string) => void;
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

interface TreeNodeProps {
  node: FileNode;
  depth: number;
  selectedPath: string | null;
  onSelect: (path: string) => void;
  projectRoot?: string;
}

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

function TreeNode({ node, depth, selectedPath, onSelect, projectRoot }: TreeNodeProps) {
  const [expanded, setExpanded] = useState(false);
  // Lazily fetched immediate children for this directory. null = not fetched
  // yet; the initial tree only carries one level, so every directory below
  // the root fetches its own children on first expand instead of the whole
  // subtree being walked upfront.
  const [children, setChildren] = useState<FileNode[] | null>(node.children ?? null);
  const [loadingChildren, setLoadingChildren] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (!node.is_dir || !expanded || children !== null) return;
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
    if (expanded) {
      // Collapsing cancels any in-flight children fetch for this directory.
      abortRef.current?.abort();
      setLoadingChildren(false);
    }
    setExpanded(!expanded);
  };

  if (node.is_dir) {
    return (
      <div>
        <button
          className="w-full justify-start h-7 px-2 text-xs gap-1.5 font-normal flex items-center hover:bg-zinc-800 transition-colors"
          style={{ paddingLeft: `${depth * 12 + 8}px` }}
          onClick={toggle}
        >
          <ChevronRight
            className={`w-3 h-3 shrink-0 text-muted-foreground transition-transform ${
              expanded ? "rotate-90" : ""
            }`}
          />
          <FileIcon name={node.name} isDir expanded={expanded} />
          <span className="truncate text-muted-foreground">{node.name}</span>
          {loadingChildren && (
            <Loader2 className="w-3 h-3 shrink-0 text-muted-foreground animate-spin ml-auto mr-2" />
          )}
        </button>
        {expanded &&
          children?.map((child) => (
            <TreeNode
              key={child.path}
              node={child}
              depth={depth + 1}
              selectedPath={selectedPath}
              onSelect={onSelect}
              projectRoot={projectRoot}
            />
          ))}
      </div>
    );
  }

  return (
    <button
      className={`w-full justify-start h-7 px-2 text-xs gap-1.5 font-normal flex items-center hover:bg-zinc-800 transition-colors ${
        selectedPath === node.path
          ? "bg-zinc-800 text-zinc-200"
          : "text-muted-foreground"
      }`}
      style={{ paddingLeft: `${depth * 12 + 20}px` }}
      onClick={() => onSelect(node.path)}
    >
      <FileIcon name={node.name} isDir={false} expanded={false} />
      <span className="truncate">{node.name}</span>
    </button>
  );
}

export default function FileTree({ onOpenFile, projectPath }: FileTreeProps) {
  const [tree, setTree] = useState<FileNode[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const [extraPaths, setExtraPaths] = useState<string[]>([]);
  const [activeRoot, setActiveRoot] = useState<string | undefined>(projectPath);

  // The active project is the default root; extra allowed paths (configured
  // in Settings) are additional roots the user can switch the tree to.
  useEffect(() => {
    api
      .getPathsConfig()
      .then((cfg) => setExtraPaths(cfg.extra_allowed_paths || []))
      .catch((err) => console.error("Failed to load extra allowed paths:", err));
  }, []);

  // Switching projects resets the tree back to that project's root.
  useEffect(() => {
    setActiveRoot(projectPath);
  }, [projectPath]);

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

  // Load tree whenever the active root changes. When no project is
  // selected (no active project yet) we do not fall back to the server's
  // anchored workDir — that would list an unrelated directory (e.g. home or
  // "/") and show a confusing empty "No files" state on a fresh install.
  useEffect(() => {
    const root = activeRoot ?? projectPath;
    if (!root) {
      setTree([]);
      setLoading(false);
      return;
    }
    setLoading(true);
    (async () => {
      try {
        const query = `path=${encodeURIComponent(root)}&depth=1`;
        const res = await fetch(apiPath(`/api/files/tree?${query}`), { headers: authHeaders() });
        if (!res.ok) throw new Error("Failed to load file tree");
        const data: FileTreeResponse = await res.json();
        if (data.truncated) {
          console.warn("File tree truncated at the root; not all entries were loaded");
        }
        setTree(data.children);
      } catch (err) {
        console.error("File tree error:", err);
      } finally {
        setLoading(false);
      }
    })();
  }, [activeRoot, projectPath]);

  const handleSelect = (path: string) => {
    setSelectedPath(path);
    onOpenFile(path, activeRoot);
  };

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between px-3 h-9 border-b border-border shrink-0 gap-2">
        <h3 className="text-xs font-medium text-muted-foreground shrink-0">Files</h3>
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
      <ScrollArea className="flex-1">
        {loading ? (
          <div className="flex items-center justify-center py-12 text-xs text-muted-foreground">
            Loading…
          </div>
        ) : !(activeRoot ?? projectPath) ? (
          <div className="px-4 py-12 text-center text-xs text-muted-foreground">
            <FolderOpen className="w-8 h-8 mx-auto mb-2 text-muted-foreground/60" />
            <p>No project added yet</p>
            <p className="mt-1">Add a project from the sidebar to browse files</p>
          </div>
        ) : tree.length === 0 ? (
          <div className="px-4 py-12 text-center text-xs text-muted-foreground">
            No files
          </div>
        ) : (
          <div className="py-1">
            {tree.map((node) => (
              <TreeNode
                key={node.path}
                node={node}
                depth={0}
                selectedPath={selectedPath}
                onSelect={handleSelect}
                projectRoot={activeRoot}
              />
            ))}
          </div>
        )}
      </ScrollArea>
    </div>
  );
}
