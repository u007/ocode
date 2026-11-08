import { useCallback, useEffect, useRef, useState } from "react";
import { ensureReadableStreamAsyncIterator } from "../../lib/readableStreamAsyncIterator";
import * as pdfjsLib from "pdfjs-dist";
import workerSrc from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import { PDFDocument, degrees } from "pdf-lib";
import { addHighlight, addTextNote, canvasToPdfRect } from "./pdfAnnotate";
import { api as apiClient } from "../../api/client";

ensureReadableStreamAsyncIterator();

if (typeof window !== "undefined" && pdfjsLib.GlobalWorkerOptions.workerSrc !== workerSrc) {
  pdfjsLib.GlobalWorkerOptions.workerSrc = workerSrc;
}

async function sha256Hex(bytes: Uint8Array): Promise<string> {
  const buf = await crypto.subtle.digest("SHA-256", bytes as unknown as BufferSource);
  return Array.from(new Uint8Array(buf)).map((b) => b.toString(16).padStart(2, "0")).join("");
}

export default function PdfEditor({
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
  const [pdfBytes, setPdfBytes] = useState<Uint8Array | null>(null);
  const [doc, setDoc] = useState<PDFDocument | null>(null);
  const [pageCount, setPageCount] = useState(0);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState(0);
  const [sel, setSel] = useState<{ page: number; x: number; y: number; width: number; height: number } | null>(null);
  const dragRef = useRef<{ page: number; startX: number; startY: number } | null>(null);
  const viewportRefs = useRef<any[]>([]);
  const renderedBytesRef = useRef<Uint8Array | null>(null);
  const [rendering, setRendering] = useState(true);
  const canvasRefs = useRef<HTMLCanvasElement[]>([]);
  const originalHashRef = useRef<string>("");

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
        const pdfDoc = await PDFDocument.load(bytes);
        setPdfBytes(bytes);
        setDoc(pdfDoc);
        setPageCount(pdfDoc.getPageCount());
        originalHashRef.current = await sha256Hex(bytes);
        setDirty(false);
      } catch (e) {
        if (!cancelled) setError(String(e));
      }
    })();
    return () => { cancelled = true; };
  }, [path, projectRoot, projectHost]);

  useEffect(() => {
    if (!pdfBytes || !doc) return;
    let cancelled = false;
    setRendering(true);
    (async () => {
      try {
        const loadingTask = pdfjsLib.getDocument({ data: pdfBytes.slice() });
        const pdf = await loadingTask.promise;
        for (let i = 0; i < pdf.numPages; i++) {
          if (cancelled) return;
          const page = await pdf.getPage(i + 1);
          const vp = page.getViewport({ scale: 0.3 });
          viewportRefs.current[i] = vp;
          const canvas = canvasRefs.current[i];
          if (!canvas) continue;
          canvas.width = vp.width;
          canvas.height = vp.height;
          const ctx = canvas.getContext("2d");
          if (!ctx) continue;
          await page.render({ canvas, canvasContext: ctx, viewport: vp } as any).promise;
        }
        if (!cancelled) renderedBytesRef.current = pdfBytes;
      } finally {
        if (!cancelled) setRendering(false);
      }
    })();
    return () => { cancelled = true; };
  }, [pdfBytes, pageCount, doc]);

  const markDirty = useCallback(() => {
    setDirty(true);
    onDirtyChange?.(true);
  }, [onDirtyChange]);

  const rotatePage = useCallback((idx: number, deg: number) => {
    if (!doc) return;
    const p = doc.getPage(idx);
    p.setRotation(degrees((p.getRotation().angle + deg) % 360));
    setDoc(doc);
    setPageCount(doc.getPageCount());
    markDirty();
  }, [doc, markDirty]);

  const deletePage = useCallback((idx: number) => {
    if (!doc || doc.getPageCount() <= 1) return;
    doc.removePage(idx);
    setDoc(doc);
    setPageCount(doc.getPageCount());
    setSelected(Math.max(0, Math.min(selected, doc.getPageCount() - 1)));
    markDirty();
  }, [doc, selected, markDirty]);

  const movePage = useCallback((idx: number, dir: -1 | 1) => {
    if (!doc) return;
    const target = idx + dir;
    if (target < 0 || target >= doc.getPageCount()) return;
    const page = doc.getPages()[idx];
    doc.removePage(idx);
    doc.insertPage(target, page);
    setDoc(doc);
    setPageCount(doc.getPageCount());
    setSelected(target);
    markDirty();
  }, [doc, markDirty]);

  const mergePdf = useCallback(async () => {
    if (!doc) return;
    const input = document.createElement("input");
    input.type = "file";
    input.accept = "application/pdf";
    input.onchange = async () => {
      const file = input.files?.[0];
      if (!file) return;
      try {
        const buf = await file.arrayBuffer();
        const src = await PDFDocument.load(new Uint8Array(buf));
        const copied = await doc.copyPages(src, src.getPageIndices());
        copied.forEach((p) => doc.addPage(p));
        setDoc(doc);
        setPageCount(doc.getPageCount());
        markDirty();
      } catch (e) {
        setError(String(e));
      }
    };
    input.click();
  }, [doc, markDirty]);

  const relPoint = (e: React.MouseEvent, canvas: HTMLCanvasElement) => {
    const r = canvas.getBoundingClientRect();
    const sx = canvas.width / r.width;
    const sy = canvas.height / r.height;
    return { x: (e.clientX - r.left) * sx, y: (e.clientY - r.top) * sy };
  };
  const onPageMouseDown = (page: number, e: React.MouseEvent) => {
    setSelected(page);
    const canvas = canvasRefs.current[page];
    if (!canvas) return;
    const p = relPoint(e, canvas);
    dragRef.current = { page, startX: p.x, startY: p.y };
    setSel({ page, x: p.x, y: p.y, width: 0, height: 0 });
  };
  const onPageMouseMove = (e: React.MouseEvent) => {
    const drag = dragRef.current;
    if (!drag) return;
    const canvas = canvasRefs.current[drag.page];
    if (!canvas) return;
    const p = relPoint(e, canvas);
    setSel({
      page: drag.page,
      x: Math.min(drag.startX, p.x),
      y: Math.min(drag.startY, p.y),
      width: Math.abs(p.x - drag.startX),
      height: Math.abs(p.y - drag.startY),
    });
  };
  const onPageMouseUp = () => {
    dragRef.current = null;
  };

  const applyHighlight = useCallback(async () => {
    if (!doc) return;
    if (renderedBytesRef.current !== pdfBytes) return;
    try {
      const pageSize = doc.getPage(selected).getSize();
      const rect =
        sel && sel.page === selected && sel.width > 1 && sel.height > 1
          ? canvasToPdfRect(sel, {
              scale: 0.3,
              pageHeight: pageSize.height,
              transform: viewportRefs.current[selected]?.transform,
            })
          : { x: 40, y: 40, width: 300, height: 24 };
      const bytes = await addHighlight(await doc.save(), selected, rect);
      const next = await PDFDocument.load(bytes);
      setPdfBytes(bytes);
      setDoc(next);
      markDirty();
    } catch (e) {
      setError(String(e));
    }
  }, [doc, selected, sel, pdfBytes, markDirty]);

  const applyNote = useCallback(async () => {
    if (!doc) return;
    if (renderedBytesRef.current !== pdfBytes) return;
    const text = window.prompt("Note text");
    if (!text) return;
    try {
      const pageSize = doc.getPage(selected).getSize();
      const pos =
        sel && sel.page === selected
          ? canvasToPdfRect(
              { ...sel, height: 0 },
              {
                scale: 0.3,
                pageHeight: pageSize.height,
                transform: viewportRefs.current[selected]?.transform,
              },
            )
          : { x: 40, y: 80 };
      const bytes = await addTextNote(await doc.save(), selected, pos.x, pos.y, text);
      const next = await PDFDocument.load(bytes);
      setPdfBytes(bytes);
      setDoc(next);
      markDirty();
    } catch (e) {
      setError(String(e));
    }
  }, [doc, selected, sel, pdfBytes, markDirty]);

  const save = useCallback(async (): Promise<boolean> => {
    if (!doc) return false;
    setSaving(true);
    try {
      const bytes = await doc.save();
      const u8 = new Uint8Array(bytes);
      const hash = await sha256Hex(u8);
      const res = await apiClient.saveFileRaw(path, u8.buffer as ArrayBuffer, {
        projectRoot,
        host: projectHost,
        expectedHash: originalHashRef.current,
      });
      if (!res) { setSaving(false); return false; }
      originalHashRef.current = hash;
      setDirty(false);
      onDirtyChange?.(false);
      setSaving(false);
      return true;
    } catch (e) {
      setError(String(e));
      setSaving(false);
      return false;
    }
  }, [doc, path, projectRoot, projectHost, onDirtyChange]);

  useEffect(() => {
    if (onSaveRef) onSaveRef.current = save;
    onSaveHandlerRegistered?.();
    return () => { if (onSaveRef) onSaveRef.current = null; };
  }, [save, onSaveRef, onSaveHandlerRegistered]);

  if (error) return <div className="p-4 text-sm text-destructive">{error}</div>;
  if (!doc) return <div className="p-4 text-sm text-muted-foreground">Loading…</div>;

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-2 border-b p-2 text-xs">
        <button onClick={() => rotatePage(selected, 90)} className="rounded border px-2 py-1">Rotate 90°</button>
        <button onClick={() => deletePage(selected)} className="rounded border px-2 py-1">Delete page</button>
        <button onClick={() => movePage(selected, -1)} className="rounded border px-2 py-1">Move up</button>
        <button onClick={() => movePage(selected, 1)} className="rounded border px-2 py-1">Move down</button>
        <button onClick={mergePdf} className="rounded border px-2 py-1">Merge PDF…</button>
        <button onClick={() => void applyHighlight()} disabled={rendering} className="rounded border px-2 py-1 disabled:opacity-50">Highlight</button>
        <button onClick={() => void applyNote()} disabled={rendering} className="rounded border px-2 py-1 disabled:opacity-50">Add note…</button>
        <span className="ml-auto">{dirty ? "Unsaved changes" : "Saved"}</span>
        <button onClick={save} disabled={!dirty || saving} className="rounded border px-2 py-1 disabled:opacity-50">
          {saving ? "Saving…" : "Save"}
        </button>
      </div>
      <div className="flex-1 overflow-auto p-2">
        <div className="flex flex-wrap gap-2">
          {Array.from({ length: pageCount }).map((_, i) => (
            <div
              key={i}
              onMouseDown={(e) => onPageMouseDown(i, e)}
              onMouseMove={onPageMouseMove}
              onMouseUp={onPageMouseUp}
              onMouseLeave={onPageMouseUp}
              className={`cursor-crosshair rounded border p-1 ${selected === i ? "ring-2 ring-primary" : ""}`}
            >
              <canvas ref={(el) => { if (el) canvasRefs.current[i] = el; }} />
              <div className="text-center text-xs">{i + 1}</div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
