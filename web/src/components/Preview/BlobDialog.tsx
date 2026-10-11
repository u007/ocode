import { useCallback, useEffect, useRef, useState } from "react";
import { Download, Loader2, Upload } from "lucide-react";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { api, type DBCell } from "../../api/client";
import {
  blobFilename,
  describeBlobCell,
  downloadBytes,
  fileToBytes,
  formatHexDump,
  isInlineSafeMediaType,
} from "./blobPreview";

/**
 * View, download and replace one BLOB cell.
 *
 * The grid only carries the first 8 KB of a blob, so this dialog FETCHES the
 * real value before it can show or save it — the `{bytes, truncated}` it is
 * given is used only for the "N MB — only a preview is loaded" notice, never
 * presented as the value.
 *
 * Rendering is an allowlist decision: raster images go into an `<img>` via an
 * object URL, everything else gets a hex dump. SVG and HTML execute script, so
 * "the browser could display it" is not the test.
 */
export interface BlobDialogProps {
  open: boolean;
  path: string;
  table: string;
  column: string;
  rowKey: Record<string, DBCell>;
  /** Size/truncation the grid already knows, for the pre-fetch notice. */
  cell: { bytes: number; truncated: boolean };
  editable: boolean;
  projectRoot?: string;
  projectHost?: string;
  onClose: () => void;
  /** Called after a successful replace so the grid can refetch. */
  onReplaced?: () => void;
}

type LoadState =
  | { kind: "loading" }
  | { kind: "null" }
  | { kind: "ready"; data: ArrayBuffer; mediaType: string }
  | { kind: "error"; message: string };

export default function BlobDialog({
  open,
  path,
  table,
  column,
  rowKey,
  cell,
  editable,
  projectRoot,
  projectHost,
  onClose,
  onReplaced,
}: BlobDialogProps) {
  const [state, setState] = useState<LoadState>({ kind: "loading" });
  const [objectUrl, setObjectUrl] = useState<string | null>(null);
  const [uploadBusy, setUploadBusy] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement | null>(null);

  const keyJson = JSON.stringify(rowKey);

  const load = useCallback(() => {
    let cancelled = false;
    setState({ kind: "loading" });
    api
      .dbBlobDownload(path, table, column, rowKey, { projectRoot, host: projectHost })
      .then((res) => {
        if (cancelled) return;
        if (res.isNull || !res.data) {
          setState({ kind: "null" });
          return;
        }
        setState({ kind: "ready", data: res.data, mediaType: res.mediaType });
      })
      .catch((e: unknown) => {
        if (!cancelled) setState({ kind: "error", message: e instanceof Error ? e.message : String(e) });
      });
    return () => {
      cancelled = true;
    };
    // rowKey is rebuilt on every render; keyJson is its stable identity.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, table, column, keyJson, projectRoot, projectHost]);

  useEffect(() => {
    if (!open) return undefined;
    return load();
  }, [open, load]);

  // One object URL per loaded value, revoked on replace/close so a dialog that
  // is opened repeatedly does not leak blobs for the session's lifetime.
  useEffect(() => {
    if (state.kind !== "ready" || !isInlineSafeMediaType(state.mediaType)) {
      setObjectUrl(null);
      return undefined;
    }
    const url = URL.createObjectURL(new Blob([state.data], { type: state.mediaType }));
    setObjectUrl(url);
    return () => URL.revokeObjectURL(url);
  }, [state]);

  const doDownload = () => {
    if (state.kind !== "ready") return;
    downloadBytes(blobFilename(column, state.mediaType), state.data, state.mediaType);
  };

  const doUpload = async (file: File) => {
    setUploadBusy(true);
    setUploadError(null);
    try {
      const bytes = await fileToBytes(file);
      await api.dbBlobUpload(path, table, column, rowKey, bytes, {
        projectRoot,
        host: projectHost,
      });
      // Refetch rather than assuming the new bytes: the server may have
      // normalized them, and seeing the stored value is the point of the view.
      load();
      onReplaced?.();
    } catch (e) {
      setUploadError(e instanceof Error ? e.message : String(e));
    } finally {
      setUploadBusy(false);
      if (fileRef.current) fileRef.current.value = "";
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Blob · {column}</DialogTitle>
        </DialogHeader>

        <p className="text-xs text-muted-foreground" data-testid="blob-size">
          {describeBlobCell(cell)}
        </p>

        <div className="flex max-h-[50vh] min-h-[8rem] flex-col overflow-auto rounded border border-border bg-muted/20 p-2">
          {state.kind === "loading" && (
            <div className="flex flex-1 items-center justify-center gap-2 text-xs text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" /> Reading blob…
            </div>
          )}
          {state.kind === "null" && (
            <div className="flex flex-1 items-center justify-center text-xs text-muted-foreground" data-testid="blob-null">
              This cell holds NULL — no value is stored.
            </div>
          )}
          {state.kind === "error" && (
            <div className="flex flex-1 items-center justify-center p-2 text-center text-xs text-destructive" data-testid="blob-error">
              {state.message}
            </div>
          )}
          {state.kind === "ready" && isInlineSafeMediaType(state.mediaType) && objectUrl && (
            <img src={objectUrl} alt={`Blob content of ${column}`} className="mx-auto max-h-96 object-contain" />
          )}
          {state.kind === "ready" && !isInlineSafeMediaType(state.mediaType) && (
            <pre
              className="whitespace-pre font-mono text-[11px] leading-tight"
              data-testid="blob-hex"
            >
              {formatHexDump(new Uint8Array(state.data))}
            </pre>
          )}
        </div>

        {uploadError ? (
          <p className="text-xs text-destructive" role="alert" data-testid="blob-upload-error">
            {uploadError}
          </p>
        ) : null}

        <DialogFooter className="flex items-center gap-2">
          {editable ? (
            <>
              <input
                ref={fileRef}
                type="file"
                aria-label={`Replace ${column}`}
                className="hidden"
                onChange={(e) => {
                  const f = e.target.files?.[0];
                  if (f) void doUpload(f);
                }}
              />
              <button
                type="button"
                disabled={uploadBusy}
                data-testid="blob-upload"
                onClick={() => fileRef.current?.click()}
                className="inline-flex items-center gap-1 rounded border border-border px-2 py-0.5 text-xs hover:bg-muted disabled:opacity-50"
              >
                {uploadBusy ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <Upload className="h-3 w-3" />
                )}
                Replace…
              </button>
            </>
          ) : null}
          <button
            type="button"
            disabled={state.kind !== "ready"}
            data-testid="blob-download"
            onClick={doDownload}
            className="inline-flex items-center gap-1 rounded bg-primary px-2 py-0.5 text-xs text-primary-foreground hover:opacity-90 disabled:opacity-50"
          >
            <Download className="h-3 w-3" /> Download
          </button>
          <button
            type="button"
            onClick={onClose}
            className="rounded border border-border px-2 py-0.5 text-xs hover:bg-muted"
          >
            Close
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}