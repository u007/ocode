import { useEffect, useRef, useState } from "react";
import { BrowserPanel } from "../Browser/BrowserPanel";
import type { StateKey } from "../../lib/browserStore";
import { previewKindForPath, type PreviewOpenRequest } from "../../lib/previewKind";
import { api } from "../../api/client";
import PdfViewer from "./PdfViewer";
import DocxViewer from "./DocxViewer";
import PptxViewer from "./PptxViewer";
import MmdViewer from "./MmdViewer";
import MarkdownViewer from "./MarkdownViewer";
import TextViewer from "./TextViewer";
import ImageViewer from "./ImageViewer";
import { dispatchOpenPreview } from "../../lib/previewKind";

type Surface = "browser" | "preview";

interface Doc {
  path: string;
  kind: Exclude<ReturnType<typeof previewKindForPath>, null>;
  projectRoot?: string;
}

/**
 * Sidebar PreviewHost: the side panel's two-tab shell.
 * - Browser tab: the existing BrowserPanel (side mode) untouched — web
 *   browsing keeps working exactly like before.
 * - Preview tab: file preview + editor. Text/code is editable (Monaco),
 *   markdown renders with interactive mermaid diagrams, PDFs paginate via
 *   pdf.js, Word via docx-preview, PowerPoint via the slide parser with
 *   filmstrip + Present click-through, images inline. Every text surface
 *   supports highlight → Copy / Ask-LLM; .mmd and mermaid fences support
 *   clickable nodes with branch Ask-AI, zoom, and linked-file opening.
 *
 * The AI drives this panel through the `preview_open` tool (scanned from
 * the chat transcript by App) or `ocode:open-preview` window events (file
 * tree, diagram links) — both arrive here as `request` + `nonce`.
 */
export default function PreviewHost({
  stateKey,
  projectRoot,
  request,
  nonce,
}: {
  stateKey: StateKey;
  projectRoot?: string;
  request: PreviewOpenRequest | null;
  nonce: number;
}) {
  const [surface, setSurface] = useState<Surface>("browser");
  const [doc, setDoc] = useState<Doc | null>(null);
  const [page, setPage] = useState(1);
  const [osOpenState, setOsOpenState] = useState<string | null>(null);
  const lastNonceRef = useRef(0);

  // New activation → show the file in the Preview tab, starting at the
  // requested page/slide.
  useEffect(() => {
    if (!request || nonce === lastNonceRef.current) return;
    lastNonceRef.current = nonce;
    const kind = previewKindForPath(request.path) ?? request.kind;
    const anchor = request.projectRoot ?? projectRoot;
    setDoc({ path: request.path, kind, projectRoot: anchor });
    setPage(Math.max(1, request.page));
    setSurface("preview");
  }, [request, nonce, projectRoot]);

  // Follow project switches for the open doc's anchor.
  useEffect(() => {
    setDoc((d) => (d && !d.projectRoot && projectRoot ? { ...d, projectRoot } : d));
  }, [projectRoot]);

  const openWithOS = async () => {
    if (!doc) return;
    setOsOpenState("Opening…");
    try {
      await api.openFileWithOS(doc.path, doc.projectRoot ?? projectRoot);
      setOsOpenState("Opened in OS app");
    } catch (e) {
      setOsOpenState(e instanceof Error ? e.message : String(e));
    } finally {
      setTimeout(() => setOsOpenState(null), 2500);
    }
  };

  const openLinked = (p: string) => dispatchOpenPreview(p, 1, doc?.projectRoot ?? projectRoot);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-1 border-b border-border px-2 py-1" role="tablist" aria-label="Sidebar surface">
        <button
          type="button"
          role="tab"
          aria-selected={surface === "browser"}
          onClick={() => setSurface("browser")}
          className={`rounded px-2 py-0.5 text-xs ${surface === "browser" ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground"}`}
        >
          Browser
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={surface === "preview"}
          onClick={() => setSurface("preview")}
          className={`rounded px-2 py-0.5 text-xs ${surface === "preview" ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground"}`}
        >
          Preview{doc ? ` · ${doc.path.split("/").pop()}` : ""}
        </button>
      </div>

      {surface === "browser" ? (
        <div className="min-h-0 flex-1">
          <BrowserPanel key={stateKey} stateKey={stateKey} mode="side" />
        </div>
      ) : doc ? (
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="flex shrink-0 items-center gap-1 border-b border-border px-2 py-1 text-xs">
            <span className="min-w-0 flex-1 truncate font-mono text-muted-foreground" title={doc.path}>
              {doc.path}
            </span>
            {osOpenState && <span className="shrink-0 text-muted-foreground">{osOpenState}</span>}
            <button type="button" onClick={openWithOS} className="shrink-0 rounded px-1.5 py-0.5 hover:bg-muted" title="Open with the OS default app (native PowerPoint/Keynote/Word playback)">
              Open in app
            </button>
            <button
              type="button"
              onClick={() => navigator.clipboard?.writeText(doc.path)}
              className="shrink-0 rounded px-1.5 py-0.5 hover:bg-muted"
              title="Copy file path"
            >
              Copy path
            </button>
          </div>
          <div className="min-h-0 flex-1">
            {doc.kind === "pdf" && <PdfViewer path={doc.path} projectRoot={doc.projectRoot} page={page} onPageChange={setPage} />}
            {doc.kind === "docx" && <DocxViewer path={doc.path} projectRoot={doc.projectRoot} />}
            {doc.kind === "pptx" && <PptxViewer path={doc.path} projectRoot={doc.projectRoot} slide={page} onSlideChange={setPage} />}
            {doc.kind === "mermaid" && <MmdViewer path={doc.path} projectRoot={doc.projectRoot} onOpenFile={openLinked} />}
            {doc.kind === "markdown" && <MarkdownViewer path={doc.path} projectRoot={doc.projectRoot} onOpenFile={openLinked} />}
            {doc.kind === "text" && <TextViewer path={doc.path} projectRoot={doc.projectRoot} />}
            {doc.kind === "image" && <ImageViewer path={doc.path} projectRoot={doc.projectRoot} />}
          </div>
        </div>
      ) : (
        <div className="flex flex-1 flex-col items-center justify-center gap-2 p-4 text-center">
          <div className="text-xs text-muted-foreground">No file previewed yet.</div>
          <div className="max-w-[220px] text-[11px] leading-relaxed text-muted-foreground/70">
            Right-click a file → Preview in sidebar, click a diagram node link, or ask the AI to preview a PDF, deck, doc, or diagram.
          </div>
        </div>
      )}
    </div>
  );
}
