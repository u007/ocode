import { useEffect, useMemo, useRef } from "react";
import { FindBar } from "./FindBar";
import { isEditableTarget, previewWantsFindShortcut } from "./findDom";
import { buildHtmlFindSrcDoc } from "./htmlFindAgent";
import { useIframeFind } from "./useIframeFind";
import { runFindCommand, useFindStateReport } from "./viewerControlDom";
import { useViewerController, useViewerReport } from "../../lib/viewerContext";

/**
 * HTML preview renderer for the Files-tab Split view.
 *
 * Renders the live editor content in a sandboxed iframe via `srcDoc`.
 * The `sandbox="allow-scripts"` attribute lets the HTML run its own scripts
 * but does NOT include `allow-same-origin`, so the iframe gets an opaque
 * origin and cannot reach the parent document's cookies, localStorage, or the
 * ocode API. This is the safe choice for rendering untrusted HTML.
 *
 * That opacity is also why find cannot inspect the frame directly: a
 * self-contained agent is appended to the srcDoc and driven over postMessage
 * (`htmlFindAgent.ts`). A page whose own CSP forbids inline scripts simply
 * reports 0 matches.
 *
 * Known limitation: relative asset paths (`<link>`, `<img>`, `<script src>`)
 * do not resolve because `srcDoc` has no base URL. Absolute URLs and
 * inline styles work correctly.
 */
export default function HtmlViewer({ path, content }: { path: string; content: string }) {
  // Debounce the srcDoc update to ~300ms to avoid reloading the iframe on
  // every keystroke. The parent (FileTabContent) already debounces at 200ms,
  // but this adds a second layer so the iframe reload is further coalesced.
  const srcDoc = useMemo(() => buildHtmlFindSrcDoc(content), [content]);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const find = useIframeFind({ iframeRef });
  const report = useViewerReport(path);
  // The sandboxed frame is opaque to the parent: find runs inside it, while
  // scroll and select have no reachable DOM and stay unsupported.
  useViewerController(path, { find: (cmd) => runFindCommand(find, cmd) });
  useFindStateReport(report, { active: find.active, query: find.query, count: find.count, current: find.current, searching: find.searching });

  // Same shortcuts as the other preview surfaces, claimed only while the
  // reader is in this preview (focused iframe / hovered / selected).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      if (mod && !e.altKey && e.key.toLowerCase() === "f" && !isEditableTarget(e.target)) {
        if (!previewWantsFindShortcut(rootRef.current)) return;
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
  }, [find.active, find.show, find.go, find.hide]);

  return (
    <div ref={rootRef} className="flex h-full min-h-0 flex-col">
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
          searching={find.searching}
          opts={find.opts}
          onToggleOption={find.toggleOption}
          onNext={() => find.go(1)}
          onPrev={() => find.go(-1)}
          onClose={find.hide}
        />
      )}
      <iframe
        ref={iframeRef}
        title="HTML preview"
        sandbox="allow-scripts"
        srcDoc={srcDoc}
        onLoad={find.onFrameLoad}
        className="min-h-0 w-full flex-1 border-0"
      />
    </div>
  );
}
