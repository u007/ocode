import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "../../api/client";
import { FindBar } from "./FindBar";
import { isEditableTarget, previewWantsFindShortcut } from "./findDom";
import { buildHtmlFindSrcDoc } from "./htmlFindAgent";
import { useIframeFind } from "./useIframeFind";
import { runFindCommand, useFindStateReport } from "./viewerControlDom";
import { useViewerController, useViewerReport } from "../../lib/viewerContext";

/**
 * HTML preview renderer.
 *
 * Renders the document in a sandboxed iframe via `srcDoc`. The
 * `sandbox="allow-scripts"` attribute lets the HTML run its own scripts but
 * does NOT include `allow-same-origin`, so the iframe gets an opaque origin and
 * cannot reach the parent document's cookies, localStorage, or the ocode API.
 * This is the safe choice for rendering untrusted HTML.
 *
 * That opacity is also why find cannot inspect the frame directly: a
 * self-contained agent is appended to the srcDoc and driven over postMessage
 * (`htmlFindAgent.ts`). A page whose own CSP forbids inline scripts simply
 * reports 0 matches.
 *
 * Like MarkdownViewer, it has two modes:
 * - CONTROLLED (`content` supplied): the Files tab's Split view, which passes
 *   the live editor text so the preview follows unsaved edits.
 * - UNCONTROLLED (`content` omitted): the sidebar PreviewHost and
 *   PreviewTabPage, which pass only the path — here the viewer fetches the file
 *   itself through /api/files/content and refetches when `revision` advances.
 *
 * Known limitation: relative asset paths (`<link>`, `<img>`, `<script src>`)
 * do not resolve because `srcDoc` has no base URL. Absolute URLs and
 * inline styles work correctly.
 */
export default function HtmlViewer({
  path,
  content,
  projectRoot,
  projectHost,
  revision,
}: {
  path: string;
  /** Live editor source. Omit to make the viewer fetch the file itself. */
  content?: string;
  projectRoot?: string;
  projectHost?: string;
  /** Sidebar live-refresh counter: bumped when the active session mutates this
   *  file. Ignored in controlled mode (the editor content already tracks it). */
  revision?: number;
}) {
  const controlled = content !== undefined;
  // Uncontrolled source, read from disk. Controlled mode derives its source
  // from `content` directly: mirroring it through state would add a render per
  // keystroke and an un-acted update on mount.
  const [loaded, setLoaded] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [isBinary, setIsBinary] = useState(false);

  useEffect(() => {
    if (controlled) return;
    let cancelled = false;
    setLoaded(null);
    setError(null);
    setIsBinary(false);
    api
      .getFileContent(path, projectRoot, projectHost)
      .then((c) => {
        if (cancelled) return;
        setIsBinary(c.is_binary);
        setLoaded(c.content);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [controlled, path, projectRoot, projectHost]);

  // Live refresh (sidebar only): refetch silently so a mid-turn edit does not
  // flash the loading placeholder. The mount effect above already covers the
  // revision present at mount, so skip it — otherwise a viewer mounted with a
  // non-zero revision issues the same GET twice.
  const fetchedRevision = useRef(revision);
  useEffect(() => {
    if (controlled || !revision || revision === fetchedRevision.current) return;
    fetchedRevision.current = revision;
    let cancelled = false;
    api
      .getFileContent(path, projectRoot, projectHost)
      .then((c) => {
        if (cancelled) return;
        setIsBinary(c.is_binary);
        setLoaded(c.content);
        setError(null);
      })
      .catch(() => {
        // Keep the last good document on a transient refresh failure.
      });
    return () => {
      cancelled = true;
    };
  }, [controlled, revision, path, projectRoot, projectHost]);

  // Debounce the srcDoc update to ~300ms to avoid reloading the iframe on
  // every keystroke. The parent (FileTabContent) already debounces at 200ms,
  // but this adds a second layer so the iframe reload is further coalesced.
  const source = controlled ? (content ?? "") : (loaded ?? "");
  const ready = controlled || loaded !== null;
  const srcDoc = useMemo(() => buildHtmlFindSrcDoc(source), [source]);
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
      {!ready ? (
        <div className={error ? "p-4 text-xs text-red-400" : "p-4 text-xs text-muted-foreground"}>
          {error ? `Load failed: ${error}` : "Loading file…"}
        </div>
      ) : isBinary ? (
        <div className="p-4 text-xs text-muted-foreground">Binary file — not previewable.</div>
      ) : (
        <iframe
          ref={iframeRef}
          title="HTML preview"
          sandbox="allow-scripts"
          srcDoc={srcDoc}
          onLoad={find.onFrameLoad}
          className="min-h-0 w-full flex-1 border-0"
        />
      )}
    </div>
  );
}
