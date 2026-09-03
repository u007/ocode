import { useEffect, useRef, useState } from "react";
import { renderAsync } from "docx-preview";
import { api } from "../../api/client";
import { SelectionToolbar, usePreviewSelection } from "./SelectionToolbar";

/**
 * Word viewer (docx-preview — stable/maintained, higher fidelity than
 * mammoth: styles, tables, images, headers). Renders paginated sections
 * (multi-page docs) with the document's own layout; text stays selectable
 * for Copy / Ask-LLM.
 */
export default function DocxViewer({ path, projectRoot }: { path: string; projectRoot?: string }) {
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const bodyRef = useRef<HTMLDivElement | null>(null);
  const { ref, sel, clear } = usePreviewSelection<HTMLDivElement>(() => "doc");

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    api
      .fetchFileRaw(path, projectRoot)
      .then((buf) => {
        if (cancelled || !bodyRef.current) return;
        bodyRef.current.innerHTML = "";
        return renderAsync(buf, bodyRef.current, undefined, { inWrapper: true }).catch((e) => {
          throw e instanceof Error ? e : new Error(String(e));
        });
      })
      .then(() => {
        if (!cancelled) setLoading(false);
      })
      .catch((e) => {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : String(e));
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [path, projectRoot]);

  return (
    <div ref={ref} className="h-full min-h-0 overflow-auto bg-muted/20 p-2">
      {loading && <div className="p-4 text-xs text-muted-foreground">Loading Word document…</div>}
      {error && <div className="p-4 text-xs text-red-400">Word preview failed: {error}</div>}
      <div ref={bodyRef} className="docx-body select-text" />
      <style>{`.docx-body .docx-wrapper{background:transparent !important;padding:0 !important;}.docx-body section.docx{box-shadow:none !important;margin:0 auto 12px !important;}`}</style>
      {sel && <SelectionToolbar sel={sel} path={path} label="doc" projectRoot={projectRoot} onDone={clear} />}
    </div>
  );
}
