import { useEffect, useMemo, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { api } from "../../api/client";
import MermaidViewer from "./MermaidViewer";
import { SelectionToolbar, usePreviewSelection } from "./SelectionToolbar";

function extractMermaid(md: string): string | null {
  const m = md.match(/```mermaid\s+([\s\S]*?)```/);
  return m ? m[1].trim() : null;
}

/**
 * Markdown viewer: rendered prose (react-markdown + GFM — already a project
 * dep) with selectable text, plus an interactive diagram on top when the
 * file contains a ```mermaid fence (clickable nodes, zoom, branch Ask-AI).
 */
export default function MarkdownViewer({
  path,
  projectRoot,
  onOpenFile,
}: {
  path: string;
  projectRoot?: string;
  onOpenFile: (path: string) => void;
}) {
  const [md, setMd] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const { ref, sel, clear } = usePreviewSelection<HTMLDivElement>(() => "doc");

  useEffect(() => {
    let cancelled = false;
    setMd(null);
    setError(null);
    api
      .getFileContent(path, projectRoot)
      .then((c) => {
        if (!cancelled) setMd(c);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [path, projectRoot]);

  const diagram = useMemo(() => (md ? extractMermaid(md) : null), [md]);

  if (error) return <div className="p-4 text-xs text-red-400">Load failed: {error}</div>;
  if (md === null) return <div className="p-4 text-xs text-muted-foreground">Loading…</div>;

  return (
    <div className="flex h-full min-h-0 flex-col">
      {diagram && (
        <div className="min-h-[220px] shrink-0 border-b border-border">
          <MermaidViewer path={path} code={diagram} projectRoot={projectRoot} onOpenFile={onOpenFile} />
        </div>
      )}
      <div ref={ref} className="prose prose-sm prose-invert min-h-0 flex-1 overflow-auto p-3 select-text">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{md}</ReactMarkdown>
      </div>
      {sel && <SelectionToolbar sel={sel} path={path} label="doc" projectRoot={projectRoot} onDone={clear} />}
    </div>
  );
}
