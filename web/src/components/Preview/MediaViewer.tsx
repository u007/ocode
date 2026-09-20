import { useCallback, useEffect, useRef, useState } from "react";
import { api, apiPath } from "../../api/client";

// Media container → MIME. Blob type matters for the remote fallback (some
// browsers won't demux a typeless blob URL), and the raw endpoint already sets
// the same types server-side.
const MEDIA_MIME: Record<string, string> = {
  mp3: "audio/mpeg",
  m4a: "audio/mp4",
  aac: "audio/aac",
  wav: "audio/wav",
  ogg: "audio/ogg",
  oga: "audio/ogg",
  opus: "audio/ogg",
  flac: "audio/flac",
  mp4: "video/mp4",
  m4v: "video/mp4",
  webm: "video/webm",
  ogv: "video/ogg",
  mov: "video/quicktime",
};

/** Range-capable media URL carrying a single-file capability token. */
function streamUrl(path: string, projectRoot: string | undefined, token: string): string {
  const q =
    `path=${encodeURIComponent(path)}` +
    `${projectRoot ? `&project_root=${encodeURIComponent(projectRoot)}` : ""}` +
    `${token ? `&media_token=${encodeURIComponent(token)}` : ""}`;
  return apiPath(`/api/files/raw?${q}`);
}

/**
 * Native media preview (audio/video).
 *
 * Local files stream straight into the browser's own element via GET
 * /api/files/raw, which now serves audio/video with `http.ServeContent` —
 * range requests, instant seeking, no full-file download, no server-side
 * buffering. Because a media element cannot send an Authorization header, the
 * SPA first exchanges its bearer for a short-lived single-file capability
 * (`POST /api/files/media-token`) and puts that in the URL.
 *
 * Remote (`projectHost`) files keep the fetch→blob path: there is no range
 * transport over the SSH tunnel, so a capability URL would never be used.
 *
 * Failure handling: if issuing a capability fails (e.g. an older server), the
 * viewer degrades to the blob path rather than showing nothing; if the element
 * itself errors while streaming (capability expired, server restarted), it
 * retries once with a fresh capability before surfacing an error.
 */
export default function MediaViewer({
  path,
  projectRoot,
  projectHost,
  kind,
  active = true,
}: {
  path: string;
  projectRoot?: string;
  projectHost?: string;
  kind: "audio" | "video";
  /** False while the owning tab/pane is hidden. display:none does not pause a
   *  media element, and every visited pane stays mounted across tabs/projects,
   *  so a backgrounded video would otherwise keep playing (and keep its
   *  decoder/network active). */
  active?: boolean;
}) {
  const [src, setSrc] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  // True while `src` is a streaming capability URL (vs a blob URL).
  const streamingRef = useRef(false);
  const streamRetriesRef = useRef(0);
  const blobUrlRef = useRef("");
  const cancelledRef = useRef(false);
  const mediaRef = useRef<HTMLMediaElement | null>(null);

  // Pause on hide. The pane is hidden with `display:none` (not unmounted), which
  // does not stop playback; the user re-presses play when they return.
  useEffect(() => {
    if (active) return;
    const el = mediaRef.current;
    if (el && !el.paused) el.pause();
  }, [active]);

  const isRemote = !!projectHost;

  const revokeBlob = () => {
    if (blobUrlRef.current) {
      URL.revokeObjectURL(blobUrlRef.current);
      blobUrlRef.current = "";
    }
  };

  // Remote / degraded path: fetch the whole file and play it from a blob.
  const loadBlob = useCallback(() => {
    if (cancelledRef.current) return;
    streamingRef.current = false;
    api
      .fetchFileRaw(path, projectRoot, projectHost)
      .then((buf) => {
        if (cancelledRef.current) return;
        const ext = path.slice(path.lastIndexOf(".") + 1).toLowerCase();
        const url = URL.createObjectURL(new Blob([buf], { type: MEDIA_MIME[ext] ?? "" }));
        revokeBlob();
        blobUrlRef.current = url;
        setSrc(url);
      })
      .catch((e) => {
        if (!cancelledRef.current) setError(e instanceof Error ? e.message : String(e));
      });
  }, [path, projectRoot, projectHost]);

  // Local path: capability → range-streamed URL. Falls back to the blob path
  // when the capability endpoint is unavailable.
  const loadStream = useCallback(() => {
    if (cancelledRef.current) return;
    streamingRef.current = true;
    api
      .getMediaToken(path, projectRoot)
      .then(({ token }) => {
        if (!cancelledRef.current) setSrc(streamUrl(path, projectRoot, token));
      })
      .catch(() => {
        if (!cancelledRef.current) loadBlob();
      });
  }, [path, projectRoot, loadBlob]);

  useEffect(() => {
    cancelledRef.current = false;
    streamRetriesRef.current = 0;
    setSrc(null);
    setError(null);
    if (isRemote) loadBlob();
    else loadStream();
    return () => {
      cancelledRef.current = true;
      revokeBlob();
    };
  }, [isRemote, loadStream, loadBlob]);

  // An element error while streaming usually means the capability expired (or
  // the server restarted and dropped its in-memory store). Retry once with a
  // fresh one before giving up; a blob error is terminal (unsupported codec).
  const onMediaError = () => {
    if (streamingRef.current && streamRetriesRef.current < 1) {
      streamRetriesRef.current += 1;
      setSrc(null);
      loadStream();
      return;
    }
    setError(kind === "audio" ? "Audio playback failed" : "Video playback failed");
  };

  const label = kind === "audio" ? "Audio" : "Video";
  if (error) return <div className="p-4 text-xs text-red-400">{label} failed: {error}</div>;
  if (!src) return <div className="p-4 text-xs text-muted-foreground">Loading {kind}…</div>;

  return (
    <div className="flex h-full min-h-0 flex-col items-center justify-center gap-3 overflow-auto bg-muted/20 p-4">
      {kind === "video" ? (
        <video
          ref={(el) => {
            mediaRef.current = el;
          }}
          src={src}
          controls
          playsInline
          preload="metadata"
          onError={onMediaError}
          className="max-h-full max-w-full rounded border border-border"
        />
      ) : (
        <audio
          ref={(el) => {
            mediaRef.current = el;
          }}
          src={src}
          controls
          preload="metadata"
          onError={onMediaError}
          className="w-full max-w-xl"
        />
      )}
      <div className="max-w-full truncate font-mono text-[11px] text-muted-foreground" title={path}>
        {path}
      </div>
    </div>
  );
}
