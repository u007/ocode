import { useEffect, useState } from "react";
import { api } from "../../api/client";

/** Image preview (png/jpg/gif/webp/svg) fetched as authed bytes → blob URL. */
export default function ImageViewer({ path, projectRoot }: { path: string; projectRoot?: string }) {
  const [url, setUrl] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setUrl(null);
    setError(null);
    let objectUrl = "";
    api
      .fetchFileRaw(path, projectRoot)
      .then((buf) => {
        if (cancelled) return;
        objectUrl = URL.createObjectURL(new Blob([buf]));
        setUrl(objectUrl);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [path, projectRoot]);

  if (error) return <div className="p-4 text-xs text-red-400">Image failed: {error}</div>;
  if (!url) return <div className="p-4 text-xs text-muted-foreground">Loading image…</div>;
  return (
    <div className="flex h-full min-h-0 items-center justify-center overflow-auto bg-muted/20 p-2">
      <img src={url} alt={path} className="max-h-full max-w-full rounded border border-border object-contain" />
    </div>
  );
}
