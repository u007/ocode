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
      {kind === "pdf" && <PdfViewer path={path} projectRoot={projectRoot} page={page ?? 1} onPageChange={handlePageChange} />}
      {kind === "docx" && <DocxViewer path={path} projectRoot={projectRoot} />}
      {kind === "pptx" && <PptxViewer path={path} projectRoot={projectRoot} slide={slide ?? 1} onSlideChange={handleSlideChange} />}
      {kind === "excel" && <ExcelViewer path={path} projectRoot={projectRoot} />}
      {kind === "mermaid" && <MmdViewer path={path} projectRoot={projectRoot} onOpenFile={handleOpenFile} />}
      {kind === "markdown" && <MarkdownViewer path={path} projectRoot={projectRoot} onOpenFile={handleOpenFile} />}
      {kind === "text" && <TextViewer path={path} projectRoot={projectRoot} />}
      {kind === "image" && <ImageViewer path={path} projectRoot={projectRoot} />}
    </div>
  );
}
