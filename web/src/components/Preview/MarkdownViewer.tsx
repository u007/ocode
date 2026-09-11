import { useEffect, useMemo, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { api } from "../../api/client";
import MermaidViewer from "./MermaidViewer";
import { SelectionToolbar, usePreviewSelection } from "./SelectionToolbar";
import FileEditor from "../Files/FileEditor";

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
  const [isBinary, setIsBinary] = useState(false);
  const [forceEdit, setForceEdit] = useState(false);
  const { ref, sel, clear } = usePreviewSelection<HTMLDivElement>(() => "doc");

  useEffect(() => {
    let cancelled = false;
    setMd(null);
    setError(null);
    setIsBinary(false);
    setForceEdit(false);
    api
      .getFileContent(path, projectRoot)
      .then((c) => {
        if (!cancelled) {
          setIsBinary(c.is_binary);
          setMd(c.content);
        }
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
  if (isBinary && !forceEdit) return (
    <div className="flex h-full flex-col items-center justify-center gap-4 p-8 text-center">
      <div className="text-2xl font-bold text-amber-500">Binary File</div>
      <p className="text-sm text-muted-foreground">This file contains binary data and cannot be previewed as markdown.</p>
      <button type="button" onClick={() => setForceEdit(true)} className="rounded border border-border px-3 py-1.5 text-sm hover:bg-accent">Edit as text</button>
    </div>
  );
  if (md === null && !isBinary) return <div className="p-4 text-xs text-muted-foreground">Loading…</div>;

  if (forceEdit) {
    return (
      <div className="min-h-0 flex-1 overflow-hidden">
        <FileEditor path={path} projectRoot={projectRoot} content={md || ""} language="plaintext" />
      </div>
    );
  }

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
