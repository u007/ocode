import { useEffect, useState } from "react";
import { api } from "../../api/client";

/**
 * Image preview (png/jpg/gif/webp/svg) fetched as authed bytes → blob URL.
 *
 * When `content` is provided (controlled mode from FileTabContent split view),
 * the image is rendered from the live editor string instead of fetching from
 * disk. SVG content is rendered via a Blob URL (never innerHTML) to prevent
 * script execution.
 */
export default function ImageViewer({
  path,
  projectRoot,
  projectHost,
  content,
}: {
  path: string;
  projectRoot?: string;
  projectHost?: string;
  /** Live editor content for split-view controlled mode. */
  content?: string;
}) {
  const [url, setUrl] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setUrl(null);
    setError(null);
    let objectUrl = "";

    // Controlled mode: render from live editor content
    if (content !== undefined) {
      // For SVG, we must use a Blob URL — innerHTML would allow script execution
      const isSvg = path.toLowerCase().endsWith(".svg");
      if (isSvg) {
        objectUrl = URL.createObjectURL(new Blob([content], { type: "image/svg+xml" }));
        if (!cancelled) setUrl(objectUrl);
      } else {
        // For binary formats in controlled mode, we'd need base64 encoding
        // which is not practical here. Fall back to fetching from disk.
        api
          .fetchFileRaw(path, projectRoot, projectHost)
          .then((buf) => {
            if (cancelled) return;
            objectUrl = URL.createObjectURL(new Blob([buf]));
            setUrl(objectUrl);
          })
          .catch((e) => {
            if (!cancelled) setError(e instanceof Error ? e.message : String(e));
          });
      }
      return () => {
        cancelled = true;
        if (objectUrl) URL.revokeObjectURL(objectUrl);
      };
    }

    // Uncontrolled mode: fetch from disk
    api
      .fetchFileRaw(path, projectRoot, projectHost)
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
  }, [path, projectRoot, projectHost, content]);

  if (error) return <div className="p-4 text-xs text-red-400">Image failed: {error}</div>;
  if (!url) return <div className="p-4 text-xs text-muted-foreground">Loading image…</div>;
  return (
    <div className="flex h-full min-h-0 items-center justify-center overflow-auto bg-muted/20 p-2">
      <img src={url} alt={path} className="max-h-full max-w-full rounded border border-border object-contain" />
    </div>
  );
}
