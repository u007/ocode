import { lazy, Suspense, useState } from "react";
import type { FileEditorProps } from "./FileEditor";
import PreviewSurface from "../Preview/PreviewSurface";
import LegacyOfficePane from "../Preview/LegacyOfficePane";
import { isLegacyOfficePath, previewOnlyKindForPath } from "../../lib/previewKind";

// Monaco (and its workers) is the single largest dependency in the app. It is
// only ever needed once a text/code tab actually renders, so load it on demand
// instead of shipping it in the entry chunk. `FileEditorProps` above is a
// type-only import and erases at build time, so it does not re-introduce the
// module into the initial graph.
const FileEditor = lazy(() => import("./FileEditor"));

/**
 * Body of a Files-tab editor tab. Binary preview-only formats (PDF, Office
 * documents, images) render the shared PreviewSurface instead of Monaco — the
 * editor only ever showed a "Binary File — Edit anyway" dead end for them.
 * Legacy .doc/.ppt (no in-browser renderer) land on the OS-open fallback pane.
 * Text, code, markdown, and mermaid still open in the Monaco editor.
 *
 * Routing lives here rather than in `FileEditor` so `FileEditor` stays a pure
 * Monaco surface (it is reused by TextViewer / MarkdownViewer for real text).
 */
export default function FileTabContent(props: FileEditorProps) {
  const previewKind = previewOnlyKindForPath(props.path);
  // Page/slide for the paginated viewers (PDF, PPTX). PreviewSurface's viewers
  // are controlled by their host, so the Files tab owns the state; it survives
  // tab switches because every editor tab stays mounted (hidden, not unmounted).
  const [page, setPage] = useState(1);
  const [slide, setSlide] = useState(1);

  if (previewKind) {
    return (
      // PreviewSurface's root is `min-h-0 flex-1` and the viewers size
      // themselves with `h-full`, so they need a definite-height flex column
      // (the tab body is `absolute inset-0`). This is the same host shape
      // PreviewHost / PreviewTabPage provide.
      <div className="flex h-full min-h-0 flex-col">
        <PreviewSurface
          path={props.path}
          kind={previewKind}
          projectRoot={props.projectRoot}
          projectHost={props.projectHost}
          page={page}
          onPageChange={setPage}
          slide={slide}
          onSlideChange={setSlide}
        />
      </div>
    );
  }

  if (isLegacyOfficePath(props.path)) {
    return <LegacyOfficePane path={props.path} projectRoot={props.projectRoot} projectHost={props.projectHost} />;
  }

  return (
    // Suspense placeholder for the Monaco chunk. Kept chrome-free so it does
    // not flash a frame the editor then replaces.
    <Suspense
      fallback={
        <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
          Loading editor…
        </div>
      }
    >
      <FileEditor {...props} />
    </Suspense>
  );
}
