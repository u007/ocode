import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { renderAsync } from "docx-preview";
import { api } from "../../api/client";
import { SelectionToolbar, usePreviewSelection } from "./SelectionToolbar";
import { FindBar } from "./FindBar";
import { FindHighlights } from "./FindHighlights";
import { isEditableTarget, previewWantsFindShortcut } from "./findDom";
import { useDomFind } from "./useDomFind";
import { domViewerController, useFindStateReport } from "./viewerControlDom";
import { useViewerController, useViewerReport, VIEWER_VISIBLE_MAX } from "../../lib/viewerContext";
import { collectVisibleText, throttleTrailing } from "../../lib/viewerDom";

/**
 * Word viewer (docx-preview — stable/maintained, higher fidelity than
 * mammoth: styles, tables, images, headers). Renders paginated sections
 * (multi-page docs) with the document's own layout; text stays selectable
 * for Copy / Ask-LLM. Find searches the rendered text nodes, so it matches
 * what the reader sees (including tables and headers) rather than the raw
 * OOXML.
 */
export default function DocxViewer({ path, projectRoot, projectHost }: { path: string; projectRoot?: string; projectHost?: string }) {
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  // Bumped after docx-preview finishes writing the DOM: `useDomFind` keys its
  // text index on it, so the first find after a load sees the rendered body.
  const [docVersion, setDocVersion] = useState(0);
  const bodyRef = useRef<HTMLDivElement | null>(null);
  const report = useViewerReport(path);
  const { ref, sel, clear } = usePreviewSelection<HTMLDivElement>(() => "doc", (text) => report({ selection: text }));
  const find = useDomFind({ containerRef: ref, textRootRef: bodyRef, version: docVersion, enabled: !loading && !error });
  useViewerController(path, domViewerController({ scroller: ref, textRoot: bodyRef, find, report }));
  useFindStateReport(report, { active: find.active, query: find.query, count: find.count, current: find.current, searching: false });

  // What the reader sees. Reported after each render of the document (the body
  // is empty while loading, so the version bump re-reports the real content)
  // and throttled while scrolling.
  const snapshot = useCallback(() => {
    const el = ref.current;
    if (!el) return;
    const view = collectVisibleText(el, VIEWER_VISIBLE_MAX);
    report({ location: { heading: view.heading ?? undefined }, visibleText: view.text });
  }, [ref, report]);
  const throttledSnapshot = useMemo(() => throttleTrailing(snapshot, 250), [snapshot]);
  useEffect(() => () => throttledSnapshot.cancel(), [throttledSnapshot]);
  useEffect(() => {
    snapshot();
  }, [docVersion, snapshot]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    api
      .fetchFileRaw(path, projectRoot, projectHost)
      .then((buf) => {
        if (cancelled || !bodyRef.current) return;
        bodyRef.current.innerHTML = "";
        return renderAsync(buf, bodyRef.current, undefined, { inWrapper: true }).catch((e) => {
          throw e instanceof Error ? e : new Error(String(e));
        });
      })
      .then(() => {
        if (!cancelled) {
          setLoading(false);
          setDocVersion((v) => v + 1);
        }
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
  }, [path, projectRoot, projectHost]);
  // Same shortcuts as the other preview surfaces, claimed only while the
  // reader is inside this preview (focused/hovered/selected).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      if (mod && !e.altKey && e.key.toLowerCase() === "f" && !isEditableTarget(e.target)) {
        if (!previewWantsFindShortcut(ref.current)) return;
        e.preventDefault();
        find.show();
      } else if (e.key === "F3" && find.active) {
        e.preventDefault();
        find.go(e.shiftKey ? -1 : 1);
      } else if (e.key === "Escape" && find.active && !isEditableTarget(e.target)) {
        find.hide();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [find.active, find.show, find.go, find.hide, ref]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center justify-end border-b border-border px-2 py-0.5 text-xs">
        <button type="button" onClick={find.show} className="rounded px-1.5 py-0.5 hover:bg-muted" aria-label="Find in document" title="Find (Ctrl/⌘ F)">
          Find
        </button>
      </div>
      {find.active && (
        <FindBar
          query={find.query}
          onQueryChange={find.setQuery}
          current={find.current}
          count={find.count}
          opts={find.opts}
          onToggleOption={find.toggleOption}
          onNext={() => find.go(1)}
          onPrev={() => find.go(-1)}
          onClose={find.hide}
        />
      )}
      <div ref={ref} tabIndex={0} onScroll={() => throttledSnapshot()} className="relative h-full min-h-0 flex-1 overflow-auto bg-muted/20 p-2 outline-none">
        {loading && <div className="p-4 text-xs text-muted-foreground">Loading Word document…</div>}
        {error && <div className="p-4 text-xs text-red-400">Word preview failed: {error}</div>}
        <div ref={bodyRef} className="docx-body select-text" />
        <FindHighlights rects={find.rects} current={find.current} />
        <style>{`.docx-body .docx-wrapper{background:transparent !important;padding:0 !important;}.docx-body section.docx{box-shadow:none !important;margin:0 auto 12px !important;}`}</style>
      </div>
      {sel && <SelectionToolbar sel={sel} path={path} label="doc" projectRoot={projectRoot} projectHost={projectHost} onDone={clear} />}
    </div>
  );
}
