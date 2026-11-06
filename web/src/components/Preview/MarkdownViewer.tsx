import { isValidElement, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { api } from "../../api/client";
import MermaidViewer from "./MermaidViewer";
import { SelectionToolbar, usePreviewSelection } from "./SelectionToolbar";
import FileEditor from "../Files/FileEditor";
import MarkdownLink from "../common/MarkdownLink";
import { FindBar } from "./FindBar";
import { FindHighlights } from "./FindHighlights";
import { isEditableTarget, previewWantsFindShortcut } from "./findDom";
import { useDomFind } from "./useDomFind";
import { cn } from "../../lib/utils";
import type { EditorAppearance } from "../../lib/editorAppearance";
import {
  previewColumnClass,
  resolvePreviewWidth,
  usePreviewWidthPreset,
} from "../../lib/previewWidth";

/**
 * The mermaid source a fenced block holds, or null for any other code block.
 * react-markdown renders a fence as `<pre><code class="language-mermaid">`, so
 * the `pre` override reaches the source through its code child.
 */
function mermaidFenceSource(children: ReactNode): string | null {
  if (!isValidElement(children)) return null;
  const props = children.props as { className?: string; children?: ReactNode };
  if (!/\blanguage-mermaid\b/.test(props.className ?? "")) return null;
  return String(props.children ?? "").replace(/\n$/, "");
}

/**
 * Markdown viewer: rendered prose (react-markdown + GFM — already a project
 * dep) with selectable text. A ```mermaid fence renders as an interactive
 * diagram IN PLACE (clickable nodes, zoom, branch Ask-AI), where the author
 * wrote it — never as a strip above the document and never as a code block.
 */
export default function MarkdownViewer({
  path,
  projectRoot,
  projectHost,
  onOpenFile,
  content,
  revision,
  followTail,
  appearance,
}: {
  path: string;
  projectRoot?: string;
  projectHost?: string;
  onOpenFile: (path: string) => void;
  /** Live editor source. When provided, the viewer renders this instead of
   *  fetching from disk — the Files tab's split view feeds it the current
   *  (possibly unsaved) editor content so the preview tracks typing without a
   *  save round-trip. Omit it for the normal read-from-disk behaviour. */
  content?: string;
  /** Live-refresh revision from the sidebar PreviewHost. Bumped while the AI
   *  edits this file mid-turn; the viewer refetches WITHOUT flashing the
   *  loading placeholder (the old content stays until the new one arrives).
   *  Undefined in hosts without live refresh (Files tab) — no extra fetches. */
  revision?: number;
  /** True while the driving chat turn is running. While set (or while a
   *  revision just landed) the viewer follows the document tail when the
   *  reader is pinned to the bottom — the sidebar preview's "auto scroll". */
  followTail?: boolean;
  /** Light/dark override forwarded from PreviewSurface (Files-tab preview
   *  toggle). `"light"` drops `prose-invert`; unset keeps the dark default. */
  appearance?: EditorAppearance;
}) {
  // `content !== undefined` selects controlled mode. It is stable per mounted
  // viewer instance (the Files tab mounts a separate viewer per mode), so the
  // two effects below never fight over the same instance.
  const controlled = content !== undefined;
  const [md, setMd] = useState<string | null>(controlled ? (content ?? "") : null);
  const [error, setError] = useState<string | null>(null);
  const [isBinary, setIsBinary] = useState(false);
  const [forceEdit, setForceEdit] = useState(false);
  const { ref, sel, clear } = usePreviewSelection<HTMLDivElement>(() => "doc");
  // Rendered prose root: the searchable text lives here, while `ref` is the
  // scroll container whose geometry the highlight layer uses.
  const columnRef = useRef<HTMLDivElement | null>(null);
  const find = useDomFind({ containerRef: ref, textRootRef: columnRef, version: md, enabled: !forceEdit && !isBinary });
  // Reading column. `auto` resolves per locale (Letter for US customary-paper
  // locales, A4 elsewhere); the resolved width is applied to an inner wrapper
  // so `.prose`/`max-w-none` on the scroller still cannot cap the measure, and
  // so a narrow sidebar pane degrades to "pane width minus the padding margin".
  const widthPreset = usePreviewWidthPreset();
  const columnWidth = resolvePreviewWidth(widthPreset);

  useEffect(() => {
    if (controlled) return;
    let cancelled = false;
    setMd(null);
    setError(null);
    setIsBinary(false);
    setForceEdit(false);
    api
      .getFileContent(path, projectRoot, projectHost)
      .then((c) => {
        if (!cancelled) {
          setIsBinary(c.is_binary);
          setMd(c.content);
        }
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [path, projectRoot, projectHost, controlled]);

  // Controlled mode: mirror the caller's live source on every edit.
  useEffect(() => {
    if (!controlled) return;
    setMd(content ?? "");
    setError(null);
    setIsBinary(false);
  }, [controlled, content]);

  // Live refresh: when the sidebar PreviewHost bumps `revision` (the active
  // session's tool stream mutated this file), refetch SILENTLY — the previous
  // content stays rendered until the new text lands, so mid-turn edits don't
  // flash the loading placeholder or reset the reader's scroll position.
  // The initial revision (0) is handled by the mount fetch above.
  useEffect(() => {
    if (controlled || revision === undefined || revision === 0) return;
    let cancelled = false;
    api
      .getFileContent(path, projectRoot, projectHost)
      .then((c) => {
        if (!cancelled) {
          setIsBinary(c.is_binary);
          setMd(c.content);
          setError(null);
        }
      })
      .catch(() => {
        // Keep the last good content on a transient refresh failure.
      });
    return () => {
      cancelled = true;
    };
  }, [revision, controlled, path, projectRoot, projectHost]);

  // A ```mermaid fence renders its diagram where it sits in the document.
  // Overriding `pre` (not `code`) lets the diagram replace the whole <pre>
  // instead of nesting a block element inside it.
  const components = useMemo<Components>(
    () => ({
      a: MarkdownLink,
      pre: ({ children }) => {
        const code = mermaidFenceSource(children);
        if (code === null) return <pre>{children}</pre>;
        return (
          <div className="not-prose my-4 min-h-[220px] overflow-hidden rounded border border-border">
            <MermaidViewer
              path={path}
              code={code}
              projectRoot={projectRoot}
              projectHost={projectHost}
              onOpenFile={onOpenFile}
              appearance={appearance}
            />
          </div>
        );
      },
    }),
    [path, projectRoot, projectHost, onOpenFile, appearance],
  );

  // ── Tail-following (sidebar auto-scroll) ──
  // Mirrors ChatPanel's proven pattern: an atBottom lock maintained by the
  // scroll listener, reset when the reader scrolls up, re-armed when the
  // content no longer overflows or was replaced, and consulted by the pin
  // effect so growth (a streaming AI edit) follows the tail only while the
  // reader is pinned. The PreviewHost feeds `followTail` (turn running) so
  // a reader who never scrolled keeps following through a whole turn.
  const atBottomRef = useRef(true);
  // Only react to tail-follow signals on revision changes, not on every md
  // keystroke in controlled mode (Files-tab split renders while typing and
  // must NOT yank the preview around).
  // `lastRevRef` is seeded with the mount revision so an already-bumped
  // counter (a viewer opened mid-turn) is a baseline, not a pending follow.
  const lastRevRef = useRef<number | undefined>(revision);
  // A revision bump and the refetched `md` land in SEPARATE renders (the
  // refetch is async). The bump only arms this flag; the pin below is driven
  // by the actual content landing, so it can never scroll against the stale
  // document the refetch is about to replace.
  const pendingFollowRef = useRef(false);
  const lastMdRef = useRef<string | null | undefined>(md);

  const handleScroll = () => {
    const el = ref.current;
    if (!el) return;
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
    atBottomRef.current = distance < 24;
  };

  // The scroller is rendered conditionally (the "Loading…"/binary branches
  // return first), so the bottom-lock reads its position via React's onScroll
  // prop — a mount-only addEventListener effect would bind to `ref.current`
  // while it is still null and never recover. Do not also add a manual
  // listener or every scroll runs handleScroll twice.

  // A live-refresh revision bump is only a signal that content is coming: arm
  // a pending follow, honoured once the refetched markdown actually lands.
  useEffect(() => {
    if (controlled || revision === undefined) return;
    if (revision === lastRevRef.current) return;
    lastRevRef.current = revision;
    pendingFollowRef.current = true;
  }, [revision, controlled]);

  // Follow the tail when new content lands while the reader is pinned (or a
  // turn is streaming). Runs only on an actual `md` change — never on a bare
  // revision bump — so the pin always targets the document on screen.
  useEffect(() => {
    if (controlled) return;
    const mdChanged = md !== lastMdRef.current;
    lastMdRef.current = md;
    if (!mdChanged) return;
    const pending = pendingFollowRef.current;
    pendingFollowRef.current = false;
    if (!pending && !followTail) return;
    const el = ref.current;
    if (!el || el.clientHeight === 0) return;
    // No scrollbar: nothing to be locked away from; re-arm and clear any
    // stale pin state (same contract as ChatPanel).
    if (el.scrollHeight - el.clientHeight <= 1) {
      atBottomRef.current = true;
      return;
    }
    if (atBottomRef.current || followTail) {
      el.scrollTop = el.scrollHeight;
      atBottomRef.current = true;
    }
  }, [md, followTail, controlled]);

  // Same window-level shortcuts as the PDF viewer: Ctrl/⌘F opens the bar when
  // the reader is in this preview, F3/Shift+F3 walk matches, Esc closes. The
  // preview only claims Ctrl+F while focused/hovered/selected so the chat's
  // own find bar is not opened at the same time.
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

  if (error) return <div className="p-4 text-xs text-red-400">Load failed: {error}</div>;
  if (isBinary && !forceEdit) return (
    <div className="flex h-full flex-col items-center justify-center gap-4 p-8 text-center">
      <div className="text-2xl font-bold text-amber-500">Binary File</div>
      <p className="text-sm text-muted-foreground">This file contains binary data and cannot be previewed as markdown.</p>
      <button type="button" onClick={() => setForceEdit(true)} className="rounded border border-border px-3 py-1.5 text-sm hover:bg-accent">Edit as text</button>
    </div>
  );
  if (md === null && !isBinary) return <div className="p-4 text-xs text-muted-foreground">Loading…</div>;

  if (forceEdit) {
    return (
      <div className="min-h-0 flex-1 overflow-hidden">
        <FileEditor path={path} projectRoot={projectRoot} projectHost={projectHost} content={md || ""} language="plaintext" />
      </div>
    );
  }

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
      <div
        ref={ref}
        onScroll={handleScroll}
        tabIndex={0}
        className={cn("relative prose prose-sm max-w-none min-h-0 flex-1 overflow-auto select-text outline-none", appearance !== "light" && "prose-invert")}
      >
        <div ref={columnRef} className={previewColumnClass(columnWidth)} data-preview-column={columnWidth}>
          <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>{md}</ReactMarkdown>
        </div>
        <FindHighlights rects={find.rects} current={find.current} />
      </div>
      {sel && <SelectionToolbar sel={sel} path={path} label="doc" projectRoot={projectRoot} projectHost={projectHost} onDone={clear} />}
    </div>
  );
}
