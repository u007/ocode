import PdfViewer from "./PdfViewer";
import DocxViewer from "./DocxViewer";
import PptxViewer from "./PptxViewer";
import ExcelViewer from "./ExcelViewer";
import MmdViewer from "./MmdViewer";
import MarkdownViewer from "./MarkdownViewer";
import TextViewer from "./TextViewer";
import ImageViewer from "./ImageViewer";
import type { PreviewKind } from "../../lib/previewKind";

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
}: PreviewSurfaceProps) {
  const handlePageChange = onPageChange ?? (() => {});
  const handleSlideChange = onSlideChange ?? (() => {});
  const handleOpenFile = onOpenFile ?? (() => {});
  return (
    <div className="min-h-0 flex-1">
      {kind === "pdf" && <PdfViewer path={path} projectRoot={projectRoot} projectHost={projectHost} page={page ?? 1} onPageChange={handlePageChange} />}
      {kind === "docx" && <DocxViewer path={path} projectRoot={projectRoot} projectHost={projectHost} />}
      {kind === "pptx" && <PptxViewer path={path} projectRoot={projectRoot} projectHost={projectHost} slide={slide ?? 1} onSlideChange={handleSlideChange} />}
      {kind === "excel" && <ExcelViewer path={path} projectRoot={projectRoot} projectHost={projectHost} />}
      {kind === "mermaid" && <MmdViewer path={path} projectRoot={projectRoot} projectHost={projectHost} onOpenFile={handleOpenFile} />}
      {kind === "markdown" && <MarkdownViewer path={path} projectRoot={projectRoot} projectHost={projectHost} onOpenFile={handleOpenFile} />}
      {kind === "text" && <TextViewer path={path} projectRoot={projectRoot} projectHost={projectHost} />}
      {kind === "image" && <ImageViewer path={path} projectRoot={projectRoot} projectHost={projectHost} />}
    </div>
  );
}
