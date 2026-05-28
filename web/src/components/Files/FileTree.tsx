import { useState, useEffect } from "react";

interface FileNode {
  name: string;
  path: string;
  is_dir: boolean;
  children?: FileNode[];
}

interface Props {
  onSelect?: (path: string) => void;
}

function TreeNode({ node, depth, onSelect }: { node: FileNode; depth: number; onSelect?: (path: string) => void }) {
  const [expanded, setExpanded] = useState(depth < 2);

  if (node.is_dir) {
    return (
      <div>
        <button
          onClick={() => setExpanded(!expanded)}
          className="w-full text-left px-2 py-0.5 text-sm hover:bg-zinc-800 text-zinc-400"
          style={{ paddingLeft: `${depth * 12 + 8}px` }}
        >
          <span className="text-zinc-600 mr-1">{expanded ? "▼" : "▶"}</span>
          {node.name}
        </button>
        {expanded && node.children?.map((child) => (
          <TreeNode key={child.path} node={child} depth={depth + 1} onSelect={onSelect} />
        ))}
      </div>
    );
  }

  return (
    <button
      onClick={() => onSelect?.(node.path)}
      className="w-full text-left px-2 py-0.5 text-sm hover:bg-zinc-800 text-zinc-300"
      style={{ paddingLeft: `${depth * 12 + 20}px` }}
    >
      {node.name}
    </button>
  );
}

export default function FileTree({ onSelect }: Props) {
  const [tree, setTree] = useState<FileNode | null>(null);

  useEffect(() => {
    fetch("/api/files/tree")
      .then((r) => r.json())
      .then(setTree)
      .catch(console.error);
  }, []);

  if (!tree) return null;

  return (
    <div className="p-3">
      <label className="text-xs text-zinc-500 uppercase tracking-wider">Files</label>
      <div className="mt-1 overflow-y-auto max-h-60">
        {tree.children?.map((child) => (
          <TreeNode key={child.path} node={child} depth={0} onSelect={onSelect} />
        ))}
      </div>
    </div>
  );
}
