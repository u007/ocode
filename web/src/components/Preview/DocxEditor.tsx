import { useCallback, useEffect, useRef, useState } from "react";
import { api as apiClient, authHeaders } from "../../api/client";
import ConfirmDialog from "../common/ConfirmDialog";
import { mountUniverDoc } from "./univerDocMount";
import { docxToUniver, univerToDocx, externalImageHosts, imageRequestHeaders } from "./docxData";
import type { UniverDocHandle } from "./univerDocMount";

async function sha256Hex(bytes: Uint8Array): Promise<string> {
  const buf = await crypto.subtle.digest("SHA-256", bytes as unknown as BufferSource);
  return Array.from(new Uint8Array(buf)).map((b) => b.toString(16).padStart(2, "0")).join("");
}

const confirmedSaveKeys = new Set<string>();

export default function DocxEditor({
  path,
  projectRoot,
  projectHost,
  onDirtyChange,
  onSaveRef,
  onSaveHandlerRegistered,
}: {
  path: string;
  projectRoot?: string;
  projectHost?: string;
  onDirtyChange?: (dirty: boolean) => void;
  onSaveRef?: { current: (() => Promise<boolean>) | null };
  onSaveHandlerRegistered?: () => void;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const handleRef = useRef<UniverDocHandle | null>(null);
  const originalHashRef = useRef<string>("");
  const originalBytesRef = useRef<Uint8Array | null>(null);
  const rawBlocksRef = useRef<{ startIndex: number; xml: string }[]>([]);
  const tableStylesRef = useRef<Record<string, any>>({});
  const imageMetaRef = useRef<Record<string, { xml: string; width?: number; height?: number }>>({});
  const saveKey = `${projectHost ?? ""}::${projectRoot ?? ""}::${path}`;
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [imageCount, setImageCount] = useState(0);
  const [externalHosts, setExternalHosts] = useState<string[]>([]);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const confirmRef = useRef<{ answer: Promise<boolean>; settle: (ok: boolean) => void } | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const params = new URLSearchParams();
        params.set("path", path);
        if (projectRoot) params.set("project_root", projectRoot);
        if (projectHost) params.set("host", projectHost);
        const res = await fetch(`/api/files/raw?${params.toString()}`, { credentials: "include" });
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const buf = await res.arrayBuffer();
        const bytes = new Uint8Array(buf);
        if (cancelled) return;
        const data = await docxToUniver(bytes);
        originalBytesRef.current = bytes;
        rawBlocksRef.current = data.body?.docxRawBlocks ?? [];
        tableStylesRef.current = (data as { __tableStyles?: Record<string, any> }).__tableStyles ?? {};
        imageMetaRef.current = (data as { __imageMeta?: Record<string, { xml: string }> }).__imageMeta ?? {};
        originalHashRef.current = await sha256Hex(bytes);
        if (cancelled) return;
        const container = containerRef.current;
        if (!container) throw new Error("no container");
        const handle = await mountUniverDoc(container, data, false, {
          editable: true,
          onDirty: () => { setDirty(true); onDirtyChange?.(true); },
        });
        const snap = handle.snapshot() as { drawingsOrder?: unknown[] };
        setImageCount(Array.isArray(snap.drawingsOrder) ? snap.drawingsOrder.length : 0);
        if (cancelled) { handle.dispose(); return; }
        handleRef.current = handle;
        setLoading(false);
      } catch (e) {
        if (!cancelled) setError(String(e));
      }
    })();
    return () => {
      cancelled = true;
      confirmRef.current?.settle(false);
      confirmRef.current = null;
      if (handleRef.current) { handleRef.current.dispose(); handleRef.current = null; }
    };
  }, [path, projectRoot, projectHost, onDirtyChange]);

  const doSave = useCallback(async (): Promise<boolean> => {
    const handle = handleRef.current;
    if (!handle) return false;
    setSaving(true);
    try {
      const data = handle.snapshot();
      const imageErrors: string[] = [];
      const embeddedImageMeta: Record<string, { xml: string; width: number; height: number }> = {};
      const bytes = await univerToDocx(data, {
        originalBytes: originalBytesRef.current ?? undefined,
        rawBlocks: rawBlocksRef.current,
        tableStyles: tableStylesRef.current,
        imageMeta: imageMetaRef.current,
        resolveImage: (source, sourceType) => handle.resolveImage(source, sourceType),
        // Same-origin fetches carry the bearer token; external hosts never do.
        imageFetch: (url, init) =>
          fetch(url, {
            ...init,
            headers: imageRequestHeaders(url, window.location.origin, init.headers as Record<string, string> | undefined, authHeaders()),
          }),
        onImageError: (message) => imageErrors.push(message),
        imageMetaOut: embeddedImageMeta,
      });
      const hash = await sha256Hex(bytes);
      const res = await apiClient.saveFileRaw(path, bytes.buffer as ArrayBuffer, {
        projectRoot,
        host: projectHost,
        expectedHash: originalHashRef.current,
      });
      if (!res) { setSaving(false); return false; }
      originalHashRef.current = hash;
      originalBytesRef.current = bytes;
      // The saved package now holds the parts these images point at; keep their
      // meta so the next save reuses them instead of embedding them again.
      imageMetaRef.current = { ...imageMetaRef.current, ...embeddedImageMeta };
      setError(null);
      setNotice(imageErrors.length ? `Saved, but could not embed ${imageErrors.length} image(s): ${imageErrors.join(" ")}` : null);
      setDirty(false);
      onDirtyChange?.(false);
      setSaving(false);
      return true;
    } catch (e) {
      setError(String(e));
      setSaving(false);
      return false;
    }
  }, [path, projectRoot, projectHost, onDirtyChange]);

  const requestSave = useCallback((): Promise<boolean> => {
    if (confirmedSaveKeys.has(saveKey)) return doSave();
    const open = confirmRef.current;
    if (open !== null) return open.answer;
    let settle!: (ok: boolean) => void;
    const answer = new Promise<boolean>((resolve) => { settle = resolve; });
    confirmRef.current = { answer, settle };
    const snapshot = handleRef.current?.snapshot();
    setExternalHosts(snapshot ? externalImageHosts(snapshot, imageMetaRef.current) : []);
    setConfirmOpen(true);
    return answer;
  }, [doSave, saveKey]);

  useEffect(() => {
    if (onSaveRef) onSaveRef.current = requestSave;
    onSaveHandlerRegistered?.();
    return () => { if (onSaveRef) onSaveRef.current = null; };
  }, [requestSave, onSaveRef, onSaveHandlerRegistered]);

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-2 border-b p-2 text-xs">
        <span className="ml-auto">{dirty ? "Unsaved changes" : "Saved"}</span>
        <button onClick={() => void requestSave()} disabled={!dirty || saving} className="rounded border px-2 py-1 disabled:opacity-50">
          {saving ? "Saving…" : "Save"}
        </button>
      </div>
      {error && (
        <div className="border-b border-destructive/30 bg-destructive/10 p-2 text-xs text-destructive">{error}</div>
      )}
      {notice && (
        <div className="border-b border-amber-500/30 bg-amber-500/10 p-2 text-xs text-amber-700 dark:text-amber-400">{notice}</div>
      )}
      {imageCount > 0 && !notice && (
        <div className="border-b border-amber-500/30 bg-amber-500/10 p-2 text-xs text-amber-700 dark:text-amber-400">
          This document contains {imageCount} image{imageCount === 1 ? "" : "s"}. They are preserved and saved, but the editor cannot display them yet.
        </div>
      )}
      <div ref={containerRef} className="flex-1 overflow-auto" />
      {loading && <div className="absolute inset-0 flex items-center justify-center text-sm text-muted-foreground">Loading…</div>}
      <ConfirmDialog
        open={confirmOpen}
        title="Save document?"
        description={`Saving rewrites the document text. Paragraphs, alignment, inline formatting and simple tables are editable; table/cell borders and shading are kept, but formatting inside edited table cells is simplified. Inline images are editable (move/resize); anchored or complex images are preserved unchanged. Images pasted or inserted in the editor are embedded in the file from base64, http(s)/blob URLs, or a resolvable UUID; SVG is not embedded. ${externalHosts.length > 0 ? `Saving will download ${externalHosts.length > 1 ? "images" : "an image"} from ${externalHosts.join(", ")}. ` : ""}Other parts (styles, headers, media) are copied verbatim. The original is backed up as .bak on first save.`}
        confirmLabel="Save"
        confirmVariant="default"
        onConfirm={async () => {
          confirmedSaveKeys.add(saveKey);
          setConfirmOpen(false);
          const open = confirmRef.current;
          if (open) { confirmRef.current = null; open.settle(await doSave()); }
        }}
        onCancel={() => {
          setConfirmOpen(false);
          const open = confirmRef.current;
          if (open) { confirmRef.current = null; open.settle(false); }
        }}
      />
    </div>
  );
}
