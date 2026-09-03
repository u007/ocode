import { useEffect, useRef, useState } from "react";
import * as pdfjsLib from "pdfjs-dist";
import workerSrc from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import { api } from "../../api/client";
import { SelectionToolbar, usePreviewSelection } from "./SelectionToolbar";

// pdf.js runs page raster + text extraction in a worker; the bundled worker
// URL keeps preview offline-capable (no CDN, same as monaco-setup).
if (typeof window !== "undefined" && pdfjsLib.GlobalWorkerOptions.workerSrc !== workerSrc) {
  pdfjsLib.GlobalWorkerOptions.workerSrc = workerSrc;
}

/**
 * Multi-page PDF viewer (pdf.js, Mozilla — stable/maintained).
 * Each page renders to canvas with an overlaid selectable text layer, so
 * highlights work exactly like a native reader. Pager controls + keyboard
 * (←/→) navigate; the current page feeds the Ask-LLM citation ("p.3").
 */
export default function PdfViewer({
  path,
  projectRoot,
  page,
  onPageChange,
}: {
  path: string;
  projectRoot?: string;
  page: number;
  onPageChange: (page: number) => void;
}) {
  const [total, setTotal] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const textRef = useRef<HTMLDivElement | null>(null);
  const docRef = useRef<pdfjsLib.PDFDocumentProxy | null>(null);
  const { ref, sel, clear } = usePreviewSelection<HTMLDivElement>(() => `p.${page}`);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setTotal(0);
    api
      .fetchFileRaw(path, projectRoot)
      .then((buf) => pdfjsLib.getDocument({ data: buf }).promise)
      .then((doc) => {
        if (cancelled) {
          void doc.cleanup();
          return;
        }
        docRef.current = doc;
        setTotal(doc.numPages);
        setLoading(false);
      })
      .catch((e) => {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : String(e));
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
      const d = docRef.current;
      docRef.current = null;
      if (d) void d.cleanup();
    };
  }, [path, projectRoot]);

  useEffect(() => {
    const doc = docRef.current;
    if (!doc || total === 0) return;
    const safe = Math.min(Math.max(1, page), total);
    let cancelled = false;
    (async () => {
      try {
        const pg = await doc.getPage(safe);
        if (cancelled) return;
        const viewport = pg.getViewport({ scale: 1.5 });
        const canvas = canvasRef.current;
        if (canvas) {
          canvas.width = viewport.width;
          canvas.height = viewport.height;
          await pg.render({ canvas, viewport }).promise;
        }
        if (cancelled) return;
        const tc = await pg.getTextContent();
        const layer = textRef.current;
        if (layer) {
          layer.innerHTML = "";
          layer.style.width = `${viewport.width}px`;
          layer.style.height = `${viewport.height}px`;
          for (const item of tc.items) {
            if (!("str" in item) || !item.str) continue;
            const tx = pdfjsLib.Util.transform(viewport.transform, item.transform);
            const span = document.createElement("span");
            span.textContent = item.str;
            span.style.cssText = `position:absolute;left:${tx[4]}px;top:${tx[5]}px;font-size:${Math.abs(tx[0]) || 10}px;line-height:1;white-space:pre;`;
            layer.appendChild(span);
            layer.appendChild(document.createTextNode(" "));
          }
        }
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [page, total]);

  if (loading) return <div className="p-4 text-xs text-muted-foreground">Loading PDF…</div>;
  if (error) return <div className="p-4 text-xs text-red-400">PDF failed: {error}</div>;

  const safe = Math.min(Math.max(1, page), Math.max(1, total));
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b border-border px-2 py-1 text-xs">
        <button type="button" disabled={safe <= 1} onClick={() => onPageChange(safe - 1)} className="rounded px-1.5 py-0.5 hover:bg-muted disabled:opacity-40" aria-label="Previous page">←</button>
        <span className="text-muted-foreground">{safe} / {total}</span>
        <button type="button" disabled={safe >= total} onClick={() => onPageChange(safe + 1)} className="rounded px-1.5 py-0.5 hover:bg-muted disabled:opacity-40" aria-label="Next page">→</button>
      </div>
      <div ref={ref} className="relative min-h-0 flex-1 overflow-auto bg-muted/20 p-2" onKeyDown={(e) => {
        if (e.key === "ArrowLeft") onPageChange(safe - 1);
        else if (e.key === "ArrowRight") onPageChange(safe + 1);
      }} tabIndex={0}>
        <div className="relative mx-auto w-fit">
          <canvas ref={canvasRef} className="block rounded border border-border bg-white" />
          {/* Selectable text layer over the raster: transparent glyphs, visible selection. */}
          <div ref={textRef} className="pdf-text-layer absolute left-0 top-0 overflow-hidden select-text" aria-hidden={false} />
        </div>
      </div>
      <style>{`.pdf-text-layer span{color:transparent;}.pdf-text-layer span::selection{background:rgba(59,130,246,.35);}`}</style>
      {sel && <SelectionToolbar sel={sel} path={path} label={`p.${safe}`} projectRoot={projectRoot} onDone={clear} />}
    </div>
  );
}
