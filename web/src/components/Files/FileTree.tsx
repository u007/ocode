import { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { X, FileCode } from "lucide-react";
import hljs from "highlight.js";
import "highlight.js/styles/github-dark.css";

interface FileNode {
  name: string;
  path: string;
  is_dir: boolean;
  children?: FileNode[];
}

interface FileContent {
  path: string;
  content: string;
}

interface Props {
  onSelect?: (path: string) => void;
}

function TreeNode({
  node,
  depth,
  onSelect,
  onPreview,
}: {
  node: FileNode;
  depth: number;
  onSelect?: (path: string) => void;
  onPreview?: (path: string) => void;
}) {
  const [expanded, setExpanded] = useState(depth < 2);

  if (node.is_dir) {
    return (
      <div>
        <Button
          type="button"
          variant="ghost"
          onClick={() => setExpanded(!expanded)}
          className="h-7 w-full justify-start rounded-none px-2 text-sm text-zinc-400 hover:bg-zinc-800"
          style={{ paddingLeft: `${depth * 12 + 8}px` }}
        >
          <span className="text-zinc-600 mr-1">{expanded ? "▼" : "▶"}</span>
          {node.name}
        </Button>
        {expanded &&
          node.children?.map((child) => (
            <TreeNode
              key={child.path}
              node={child}
              depth={depth + 1}
              onSelect={onSelect}
              onPreview={onPreview}
            />
          ))}
      </div>
    );
  }

  return (
    <div className="flex items-center">
      <Button
        type="button"
        variant="ghost"
        onClick={() => onSelect?.(node.path)}
        onDoubleClick={() => onPreview?.(node.path)}
        className="h-7 flex-1 justify-start rounded-none px-2 text-sm text-zinc-300 hover:bg-zinc-800"
        style={{ paddingLeft: `${depth * 12 + 20}px` }}
      >
        {node.name}
      </Button>
      <button
        type="button"
        onClick={() => onPreview?.(node.path)}
        className="h-7 px-2 text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800 transition-colors"
        title="Preview file"
      >
        <FileCode className="w-3.5 h-3.5" />
      </button>
    </div>
  );
}

function getLanguage(filename: string): string {
  const ext = filename.split(".").pop()?.toLowerCase();
  const langMap: Record<string, string> = {
    ts: "typescript",
    tsx: "typescript",
    js: "javascript",
    jsx: "javascript",
    py: "python",
    go: "go",
    rs: "rust",
    rb: "ruby",
    java: "java",
    c: "c",
    cpp: "cpp",
    h: "c",
    hpp: "cpp",
    css: "css",
    scss: "scss",
    html: "html",
    json: "json",
    yaml: "yaml",
    yml: "yaml",
    toml: "toml",
    md: "markdown",
    sh: "bash",
    bash: "bash",
    zsh: "bash",
    sql: "sql",
    graphql: "graphql",
    xml: "xml",
    php: "php",
    swift: "swift",
    kt: "kotlin",
    scala: "scala",
    r: "r",
    R: "r",
    lua: "lua",
    dart: "dart",
  };
  return langMap[ext || ""] || "plaintext";
}

function highlightCode(code: string, language: string): string {
  try {
    if (language && hljs.getLanguage(language)) {
      return hljs.highlight(code, { language }).value;
    }
    return hljs.highlightAuto(code).value;
  } catch {
    return code
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }
}

function FilePreview({
  file,
  onClose,
}: {
  file: FileContent;
  onClose: () => void;
}) {
  const language = getLanguage(file.path);
  const lines = file.content.split("\n");

  return (
    <div className="flex flex-col h-full border-l border-zinc-700">
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-zinc-700 bg-zinc-900">
        <div className="flex items-center gap-2 min-w-0">
          <FileCode className="w-4 h-4 text-zinc-500 flex-shrink-0" />
          <span className="text-xs text-zinc-300 font-mono truncate">
            {file.path}
          </span>
        </div>
        <button
          type="button"
          onClick={onClose}
          className="p-1 text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800 rounded"
        >
          <X className="w-4 h-4" />
        </button>
      </div>

      {/* Code */}
      <div className="flex-1 overflow-auto bg-zinc-950">
        <pre className="p-4 text-xs leading-relaxed">
          <code className={`language-${language}`}>
            {lines.map((line, i) => (
              <div key={i} className="flex">
                <span className="w-12 flex-shrink-0 text-right pr-4 text-zinc-600 select-none">
                  {i + 1}
                </span>
                <span
                  className="flex-1"
                  dangerouslySetInnerHTML={{
                    __html: highlightCode(line, language),
                  }}
                />
              </div>
            ))}
          </code>
        </pre>
      </div>
    </div>
  );
}

export default function FileTree({ onSelect }: Props) {
  const [tree, setTree] = useState<FileNode | null>(null);
  const [previewFile, setPreviewFile] = useState<FileContent | null>(null);

  useEffect(() => {
    fetch("/api/files/tree")
      .then((r) => r.json())
      .then(setTree)
      .catch(console.error);
  }, []);

  const handlePreview = async (path: string) => {
    try {
      const res = await fetch(`/api/files/content?path=${encodeURIComponent(path)}`);
      if (res.ok) {
        const data = await res.json();
        setPreviewFile(data);
      }
    } catch (err) {
      console.error("Failed to load file:", err);
    }
  };

  if (!tree) return null;

  return (
    <div className="flex h-full">
      {/* File tree */}
      <div className={`${previewFile ? "w-64" : "w-full"} flex flex-col overflow-hidden border-r border-zinc-700`}>
        <div className="p-3 border-b border-zinc-700">
          <label className="text-xs text-zinc-500 uppercase tracking-wider">
            Files
          </label>
        </div>
        <div className="flex-1 overflow-y-auto">
          {tree.children?.map((child) => (
            <TreeNode
              key={child.path}
              node={child}
              depth={0}
              onSelect={onSelect}
              onPreview={handlePreview}
            />
          ))}
        </div>
      </div>

      {/* File preview */}
      {previewFile && (
        <div className="flex-1 overflow-hidden">
          <FilePreview
            file={previewFile}
            onClose={() => setPreviewFile(null)}
          />
        </div>
      )}
    </div>
  );
}
