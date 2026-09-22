import { useEffect, useMemo, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { api } from "../../api/client";
import MermaidViewer from "./MermaidViewer";
import { SelectionToolbar, usePreviewSelection } from "./SelectionToolbar";
import FileEditor from "../Files/FileEditor";

function extractMermaid(md: string): string | null {
  const m = md.match(/```mermaid\s+([\s\S]*?)```/);
  return m ? m[1].trim() : null;
}

/**
 * Markdown viewer: rendered prose (react-markdown + GFM — already a project
 * dep) with selectable text, plus an interactive diagram on top when the
 * file contains a ```mermaid fence (clickable nodes, zoom, branch Ask-AI).
 */
export default function MarkdownViewer({
  path,
  projectRoot,
  projectHost,
  onOpenFile,
  content,
  revision,
  followTail,
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

  const diagram = useMemo(() => (md ? extractMermaid(md) : null), [md]);

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
      {diagram && (
        <div className="min-h-[220px] shrink-0 border-b border-border">
          <MermaidViewer path={path} code={diagram} projectRoot={projectRoot} projectHost={projectHost} onOpenFile={onOpenFile} />
        </div>
      )}
      <div ref={ref} onScroll={handleScroll} className="prose prose-sm prose-invert max-w-none min-h-0 flex-1 overflow-auto p-3 select-text">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{md}</ReactMarkdown>
      </div>
      {sel && <SelectionToolbar sel={sel} path={path} label="doc" projectRoot={projectRoot} projectHost={projectHost} onDone={clear} />}
    </div>
  );
}
