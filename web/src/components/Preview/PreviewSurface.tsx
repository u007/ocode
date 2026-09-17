import { lazy, Suspense } from "react";
import ImageViewer from "./ImageViewer";
import MediaViewer from "./MediaViewer";
import type { PreviewKind } from "../../lib/previewKind";

// Heavy viewers are code-split. pdf.js, xlsx, docx-preview and mermaid (and,
// transitively through TextViewer/MarkdownViewer → FileEditor, all of Monaco)
// used to sit in the initial bundle purely because this file imported them
// statically — that is what made the entry chunk ~6.7 MB and its parse/compile
// cost part of every cold start. Splitting them behind `lazy` moves them into
// hashed chunks that load on first use and are then immutable-cached.
//
// ImageViewer/MediaViewer stay static: they pull nothing heavy, and the common
// image path should never pay a Suspense round-trip.
const PdfViewer = lazy(() => import("./PdfViewer"));
const DocxViewer = lazy(() => import("./DocxViewer"));
const PptxViewer = lazy(() => import("./PptxViewer"));
const ExcelViewer = lazy(() => import("./ExcelViewer"));
const MmdViewer = lazy(() => import("./MmdViewer"));
const MarkdownViewer = lazy(() => import("./MarkdownViewer"));
const TextViewer = lazy(() => import("./TextViewer"));

/** Minimal placeholder while a heavy viewer's chunk downloads. Intentionally
 *  chrome-free so it does not flash a layout the viewer then replaces. */
function ViewerLoading() {
  return (
    <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
      Loading viewer…
    </div>
  );
}

export interface PreviewSurfaceProps {
  path: string;
  kind: PreviewKind;
  projectRoot?: string;
  /** Registered remote target (SSH/WSL) for this path's project. When set,
   *  every surface reads through the remote pipeline (same ?host= routing
   *  the editor tabs use): text/markdown via /api/files/content, binary
   *  viewers (pdf/docx/pptx/excel/image/mmd) via /api/files/raw. Without
   *  it a remote path would silently read the server-local filesystem
   *  (or 400 when the remote root isn't a local root). Derived from the
   *  active project at the PreviewHost boundary. */
  projectHost?: string;
  page?: number;
  onPageChange?: (p: number) => void;
  slide?: number;
  onSlideChange?: (s: number) => void;
  onOpenFile?: (p: string) => void;
  /** Live editor source for a markdown preview: the Files tab's Split view
   *  passes the current editor content so the rendered pane tracks unsaved
   *  edits. Only `markdown` consumes it; every other kind reads from disk. */
  content?: string;
}

export default function PreviewSurface({
  path,
  kind,
  projectRoot,
  projectHost,
  page,
  onPageChange,
  slide,
  onSlideChange,
  onOpenFile,
  content,
}: PreviewSurfaceProps) {
  const handlePageChange = onPageChange ?? (() => {});
  const handleSlideChange = onSlideChange ?? (() => {});
  const handleOpenFile = onOpenFile ?? (() => {});
  return (
    <div className="min-h-0 flex-1">
      <Suspense fallback={<ViewerLoading />}>
        {kind === "pdf" && <PdfViewer path={path} projectRoot={projectRoot} projectHost={projectHost} page={page ?? 1} onPageChange={handlePageChange} />}
        {kind === "docx" && <DocxViewer path={path} projectRoot={projectRoot} projectHost={projectHost} />}
        {kind === "pptx" && <PptxViewer path={path} projectRoot={projectRoot} projectHost={projectHost} slide={slide ?? 1} onSlideChange={handleSlideChange} />}
        {kind === "excel" && <ExcelViewer path={path} projectRoot={projectRoot} projectHost={projectHost} />}
        {kind === "mermaid" && <MmdViewer path={path} projectRoot={projectRoot} projectHost={projectHost} onOpenFile={handleOpenFile} />}
        {kind === "markdown" && <MarkdownViewer path={path} projectRoot={projectRoot} projectHost={projectHost} onOpenFile={handleOpenFile} content={content} />}
        {kind === "text" && <TextViewer path={path} projectRoot={projectRoot} projectHost={projectHost} />}
        {kind === "image" && <ImageViewer path={path} projectRoot={projectRoot} projectHost={projectHost} />}
        {kind === "audio" && <MediaViewer path={path} projectRoot={projectRoot} projectHost={projectHost} kind="audio" />}
        {kind === "video" && <MediaViewer path={path} projectRoot={projectRoot} projectHost={projectHost} kind="video" />}
      </Suspense>
    </div>
  );
}
