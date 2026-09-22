import { useEffect, useState } from "react";
import { api } from "../../api/client";
import MermaidViewer from "./MermaidViewer";

/** Plain .mmd file → interactive diagram (same viewer as mermaid fences). */
export default function MmdViewer({
  path,
  projectRoot,
  projectHost,
  onOpenFile,
  revision,
}: {
  path: string;
  projectRoot?: string;
  projectHost?: string;
  onOpenFile: (path: string) => void;
  /** Live-refresh revision from the sidebar PreviewHost; refetched silently
   *  when it changes (AI edited the diagram mid-turn). */
  revision?: number;
}) {
  const [code, setCode] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setCode(null);
    setError(null);
    api
      .fetchFileRaw(path, projectRoot, projectHost)
      .then((buf) => {
        if (!cancelled) setCode(new TextDecoder().decode(buf));
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [path, projectRoot, projectHost]);

  // Live refresh on revision bumps (sidebar live mode only).
  useEffect(() => {
    if (revision === undefined || revision === 0) return;
    let cancelled = false;
    api
      .fetchFileRaw(path, projectRoot, projectHost)
      .then((buf) => {
        if (!cancelled) setCode(new TextDecoder().decode(buf));
      })
      .catch(() => {
        // Keep the last good diagram on a transient refresh failure.
      });
    return () => {
      cancelled = true;
    };
  }, [revision, path, projectRoot, projectHost]);

  if (error) return <div className="p-4 text-xs text-red-400">Diagram failed: {error}</div>;
  if (code === null) return <div className="p-4 text-xs text-muted-foreground">Loading diagram…</div>;
  return <MermaidViewer path={path} code={code} projectRoot={projectRoot} projectHost={projectHost} onOpenFile={onOpenFile} />;
}
