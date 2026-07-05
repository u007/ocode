import { useEffect, useState } from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ChevronRight, File, Folder, FolderOpen } from "lucide-react";
import { apiPath, authHeaders } from "@/api/client";

interface FileNode {
  name: string;
  path: string;
  is_dir: boolean;
  children?: FileNode[];
}

interface FileTreeProps {
  onOpenFile: (path: string) => void;
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

function TreeNode({ node, depth, selectedPath, onSelect }: TreeNodeProps) {
  const [expanded, setExpanded] = useState(depth < 1);

  if (node.is_dir) {
    return (
      <div>
        <button
          className="w-full justify-start h-7 px-2 text-xs gap-1.5 font-normal flex items-center hover:bg-zinc-800 transition-colors"
          style={{ paddingLeft: `${depth * 12 + 8}px` }}
          onClick={() => setExpanded(!expanded)}
        >
          <ChevronRight
            className={`w-3 h-3 shrink-0 text-muted-foreground transition-transform ${
              expanded ? "rotate-90" : ""
            }`}
          />
          <FileIcon name={node.name} isDir expanded={expanded} />
          <span className="truncate text-muted-foreground">{node.name}</span>
        </button>
        {expanded &&
          node.children?.map((child) => (
            <TreeNode
              key={child.path}
              node={child}
              depth={depth + 1}
              selectedPath={selectedPath}
              onSelect={onSelect}
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

export default function FileTree({ onOpenFile }: FileTreeProps) {
  const [tree, setTree] = useState<FileNode[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedPath, setSelectedPath] = useState<string | null>(null);

  // Load tree on mount
  useEffect(() => {
    (async () => {
      try {
        const res = await fetch(apiPath("/api/files/tree"), { headers: authHeaders() });
        if (!res.ok) throw new Error("Failed to load file tree");
        setTree(await res.json());
      } catch (err) {
        console.error("File tree error:", err);
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  const handleSelect = (path: string) => {
    setSelectedPath(path);
    onOpenFile(path);
  };

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between px-3 h-9 border-b border-border shrink-0">
        <h3 className="text-xs font-medium text-muted-foreground">Files</h3>
      </div>
      <ScrollArea className="flex-1">
        {loading ? (
          <div className="flex items-center justify-center py-12 text-xs text-muted-foreground">
            Loading…
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
              />
            ))}
          </div>
        )}
      </ScrollArea>
    </div>
  );
}
