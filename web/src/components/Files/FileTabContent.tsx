import { lazy, Suspense, useCallback, useEffect, useState } from "react";
import { Columns2, Eye, Pencil } from "lucide-react";
import type { FileEditorProps } from "./FileEditor";
import PreviewSurface from "../Preview/PreviewSurface";
import LegacyOfficePane from "../Preview/LegacyOfficePane";
import { isLegacyOfficePath, isMarkdownPath, previewOnlyKindForPath } from "../../lib/previewKind";
import { useResizableSplit } from "../../hooks/useResizableSplit";
import { loadPreviewViewState, previewViewKey, savePreviewViewState } from "../../lib/previewViewState";
import { cn } from "../../lib/utils";

// Monaco (and its workers) is the single largest dependency in the app. It is
// only ever needed once a text/code tab actually renders, so load it on demand
// instead of shipping it in the entry chunk. `FileEditorProps` above is a
// type-only import and erases at build time, so it does not re-introduce the
// module into the initial graph.
const FileEditor = lazy(() => import("./FileEditor"));

/** Chrome-free placeholder for the Monaco chunk so it does not flash a frame
 *  the editor then replaces. */
function EditorLoading() {
  return (
    <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
      Loading editor…
    </div>
  );
}

type MarkdownMode = "edit" | "preview" | "split";

const MARKDOWN_MODES: { mode: MarkdownMode; label: string; title: string; Icon: typeof Pencil }[] = [
  { mode: "edit", label: "Edit", title: "Edit the Markdown source", Icon: Pencil },
  { mode: "preview", label: "Preview", title: "Rendered preview", Icon: Eye },
  { mode: "split", label: "Split", title: "Editor and preview side by side", Icon: Columns2 },
];

/** Trailing debounce for the live preview source. React-markdown (plus the
 *  optional Mermaid pane) re-renders on every change, so feeding it the raw
 *  keystroke stream makes a long document janky to type in. 200ms is short
 *  enough to feel live and long enough to coalesce a typing burst. */
const PREVIEW_DEBOUNCE_MS = 200;

function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(t);
  }, [value, delayMs]);
  return debounced;
}

/**
 * Body of a Files-tab editor tab. Binary preview-only formats (PDF, Office
 * documents, images) render the shared PreviewSurface instead of Monaco — the
 * editor only ever showed a "Binary File — Edit anyway" dead end for them.
 * Legacy .doc/.ppt (no in-browser renderer) land on the OS-open fallback pane.
 * Text, code, and mermaid still open in the Monaco editor.
 *
 * **Markdown** additionally gets an Edit / Preview / Split mode switch, with
 * Edit as the default. The Monaco instance is never unmounted when the mode
 * changes (it is hidden with CSS instead), so cursor, scroll, and undo history
 * survive a trip through the preview — and because every editor tab stays
 * mounted (hidden, not unmounted) the chosen mode also survives tab switches.
 * Split feeds PreviewSurface the live editor content so the rendered pane
 * tracks unsaved edits.
 *
 * Routing lives here rather than in `FileEditor` so `FileEditor` stays a pure
 * Monaco surface (it is reused by TextViewer / MarkdownViewer for real text).
 */
export default function FileTabContent(props: FileEditorProps & { active?: boolean }) {
  const previewKind = previewOnlyKindForPath(props.path);
  // Page/slide for the paginated viewers (PDF, PPTX). PreviewSurface's viewers
  // are controlled by their host, so the Files tab owns the state; it survives
  // tab switches AND project switches because every editor tab stays mounted
  // (hidden, not unmounted). It is also persisted (under the file's canonical
  // identity, shared with PdfViewer's own zoom/scroll entry) so an app reload —
  // the one case that does unmount — resumes where the reader left off.
  const fileViewKey = previewViewKey(props.path, props.projectRoot, props.projectHost);
  const [page, setPage] = useState(() => Math.max(1, loadPreviewViewState(fileViewKey)?.page ?? 1));
  const [slide, setSlide] = useState(() => Math.max(1, loadPreviewViewState(fileViewKey)?.page ?? 1));
  const [mode, setMode] = useState<MarkdownMode>("edit");
  const split = useResizableSplit();
  const previewContent = useDebouncedValue(props.content, PREVIEW_DEBOUNCE_MS);

  // Persist the paginated viewer's position for this file. Only one of the two
  // viewers is ever mounted for a given file, so pick the index that belongs to
  // this format (a PPTX drives `slide`, a PDF drives `page`) — `page || slide`
  // would be wrong because the unused one stays at its initial 1.
  useEffect(() => {
    if (!previewKind) return;
    savePreviewViewState(fileViewKey, { page: previewKind === "pptx" ? slide : page });
  }, [fileViewKey, previewKind, page, slide]);

  const { onOpenFile, projectRoot: propsProjectRoot } = props;
  const handleOpenFile = useCallback(
    (path: string) => onOpenFile?.(path, propsProjectRoot),
    [onOpenFile, propsProjectRoot],
  );

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
          active={props.active}
        />
      </div>
    );
  }

  if (isLegacyOfficePath(props.path)) {
    return <LegacyOfficePane path={props.path} projectRoot={props.projectRoot} projectHost={props.projectHost} />;
  }

  // A `.md` the server flagged as binary falls through to the plain editor
  // path, exactly as before this mode switch existed.
  if (isMarkdownPath(props.path) && !props.isBinary) {
    return (
      <div className="flex h-full min-h-0 flex-col">
        <div className="flex shrink-0 items-center gap-1 border-b border-border bg-muted/30 px-2 py-1">
          <div
            role="group"
            aria-label="Markdown view mode"
            className="flex shrink-0 items-center overflow-hidden rounded-md border border-border"
          >
            {MARKDOWN_MODES.map(({ mode: m, label, title, Icon }) => (
              <button
                key={m}
                type="button"
                title={title}
                aria-pressed={mode === m}
                onClick={() => setMode(m)}
                className={cn(
                  "flex items-center gap-1 px-2 py-1 text-xs transition-colors",
                  mode === m
                    ? "bg-accent text-accent-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground",
                )}
              >
                <Icon className="h-3.5 w-3.5" />
                <span>{label}</span>
              </button>
            ))}
          </div>
        </div>

        <div ref={split.containerRef} className="relative flex min-h-0 flex-1 overflow-hidden">
          {/* The Monaco pane stays mounted in every mode; `hidden` (not
              unmount) is what preserves cursor/scroll/undo across a preview. */}
          <div
            className={cn(
              "h-full min-w-0",
              mode === "preview" ? "hidden" : mode === "split" ? "shrink-0" : "flex-1",
            )}
            style={mode === "split" ? { width: `${split.ratio * 100}%` } : undefined}
          >
            <Suspense fallback={<EditorLoading />}>
              <FileEditor {...props} />
            </Suspense>
          </div>

          {mode === "split" && (
            <div
              role="separator"
              aria-orientation="vertical"
              aria-label="Resize editor and preview"
              title="Drag to resize · double-click to reset"
              onPointerDown={split.onPointerDown}
              onDoubleClick={split.resetToDefault}
              className="w-1 shrink-0 cursor-col-resize bg-border hover:bg-accent active:bg-accent"
            />
          )}

          {mode !== "edit" && (
            // Flex column so PreviewSurface's `min-h-0 flex-1` root (and the
            // `h-full` viewer inside it) get a definite height — the same host
            // shape the binary preview-only branch above provides. An
            // auto-height block parent would make `h-full` resolve to auto and
            // the rendered document would overflow instead of scrolling.
            <div className="flex h-full min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
              <PreviewSurface
                path={props.path}
                kind="markdown"
                projectRoot={props.projectRoot}
                projectHost={props.projectHost}
                content={previewContent}
                onOpenFile={handleOpenFile}
              />
            </div>
          )}
        </div>
      </div>
    );
  }

  return (
    <Suspense fallback={<EditorLoading />}>
      <FileEditor {...props} />
    </Suspense>
  );
}
