import { useEffect, useState } from "react";
import { api } from "../../api/client";
import MermaidViewer from "./MermaidViewer";

/** Plain .mmd file → interactive diagram (same viewer as mermaid fences). */
export default function MmdViewer({
  path,
  projectRoot,
  onOpenFile,
}: {
  path: string;
  projectRoot?: string;
  onOpenFile: (path: string) => void;
}) {
  const [code, setCode] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setCode(null);
    setError(null);
    api
      .fetchFileRaw(path, projectRoot)
      .then((buf) => {
        if (!cancelled) setCode(new TextDecoder().decode(buf));
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [path, projectRoot]);

  if (error) return <div className="p-4 text-xs text-red-400">Diagram failed: {error}</div>;
  if (code === null) return <div className="p-4 text-xs text-muted-foreground">Loading diagram…</div>;
  return <MermaidViewer path={path} code={code} projectRoot={projectRoot} onOpenFile={onOpenFile} />;
}
