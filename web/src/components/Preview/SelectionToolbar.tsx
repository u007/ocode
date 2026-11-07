import { useCallback, useEffect, useRef, useState } from "react";
import { dispatchPreviewContext } from "../../lib/previewKind";

export interface TextSelection {
  text: string;
  rect: { top: number; left: number };
}

/**
 * Reads the live window selection when it is a non-collapsed highlight
 * anchored inside `el`. Returns the full trimmed text (callers clamp it for
 * the toolbar) and the rect's top/center-left, or null when there is none.
 */
function readContainerSelection(el: HTMLElement | null): TextSelection | null {
  const s = window.getSelection();
  if (!s || s.isCollapsed || !el || !s.toString().trim()) return null;
  const anchor = s.anchorNode ? (s.anchorNode instanceof Element ? s.anchorNode : s.anchorNode.parentElement) : null;
  if (!anchor || !el.contains(anchor)) return null;
  const text = s.toString().trim();
  const r = s.getRangeAt(0).getBoundingClientRect();
  return { text, rect: { top: r.top, left: r.left + r.width / 2 } };
}

/**
 * Shared highlight → toolbar flow for every PreviewHost renderer.
 * Listens for mouseup inside the container, keeps the selection only when
 * it is non-collapsed and anchored in the container, and exposes a floating
 * toolbar anchor. Copy writes the clipboard; Ask LLM pushes a
 * `ocode:preview-context` event (file + label + excerpt) that App funnels
 * into the chat composer's context chip.
 *
 * `onSelection` receives the trimmed text of a highlight found in the
 * container, and `null` when a mouseup in the container left no usable
 * highlight (a deselect made inside the viewer).
 */
export function usePreviewSelection<T extends HTMLElement>(label: () => string, onSelection?: (text: string | null) => void) {
  const ref = useRef<T | null>(null);
  const [sel, setSel] = useState<TextSelection | null>(null);
  const labelRef = useRef(label);
  labelRef.current = label;
  const onSelectionRef = useRef(onSelection);
  onSelectionRef.current = onSelection;

  const onMouseUp = useCallback(() => {
    // Let the browser finish the selection before reading it.
    requestAnimationFrame(() => {
      const found = readContainerSelection(ref.current);
      if (!found) {
        setSel(null);
        onSelectionRef.current?.(null);
        return;
      }
      setSel({ text: found.text.slice(0, 2000), rect: found.rect });
      onSelectionRef.current?.(found.text);
    });
  }, []);

  // No dependency array: the viewers render their container conditionally
  // (after a "Loading…" branch), so a mount-only effect bound while
  // `ref.current` was still null and the listener never attached. Re-binding
  // after every render is one add/remove pair and always sees the live node.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.addEventListener("mouseup", onMouseUp);
    return () => el.removeEventListener("mouseup", onMouseUp);
  });

  // The toolbar position is a snapshot taken on mouseup. Re-read the live
  // selection when the layout under it moves: a viewport resize, a container
  // relayout (reader mode), or any scroll. One frame is pending at a time.
  const frameRef = useRef<number | null>(null);
  const reanchor = useCallback(() => {
    if (frameRef.current !== null) return;
    frameRef.current = requestAnimationFrame(() => {
      frameRef.current = null;
      const found = readContainerSelection(ref.current);
      if (!found) {
        setSel(null);
        return;
      }
      setSel({ text: found.text.slice(0, 2000), rect: found.rect });
    });
  }, []);

  // Bound only while a highlight is showing, so an idle viewer pays nothing.
  const hasSel = sel !== null;
  useEffect(() => {
    if (!hasSel) return;
    const el = ref.current;
    window.addEventListener("resize", reanchor);
    window.addEventListener("scroll", reanchor, true);
    const ro = el && typeof ResizeObserver !== "undefined" ? new ResizeObserver(reanchor) : null;
    if (ro && el) ro.observe(el);
    return () => {
      window.removeEventListener("resize", reanchor);
      window.removeEventListener("scroll", reanchor, true);
      ro?.disconnect();
      if (frameRef.current !== null) {
        cancelAnimationFrame(frameRef.current);
        frameRef.current = null;
      }
    };
  }, [hasSel, reanchor]);

  const clear = useCallback(() => {
    setSel(null);
    window.getSelection()?.removeAllRanges();
  }, []);

  return { ref, sel, clear, currentLabel: labelRef };
}

export function SelectionToolbar({
  sel,
  path,
  label,
  projectRoot,
  projectHost,
  onDone,
}: {
  sel: TextSelection;
  path: string;
  label: string;
  projectRoot?: string;
  projectHost?: string;
  onDone: () => void;
}) {
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(sel.text);
      setCopied(true);
      setTimeout(onDone, 600);
    } catch {
      onDone();
    }
  };

  const ask = () => {
    dispatchPreviewContext({ path, label, excerpt: sel.text, projectRoot, projectHost });
    onDone();
  };

  return (
    <div
      className="fixed z-50 flex items-center gap-1 rounded-md border border-border bg-popover px-1.5 py-1 shadow-lg"
      style={{ top: Math.max(8, sel.rect.top - 44), left: Math.min(Math.max(8, sel.rect.left - 80), window.innerWidth - 180) }}
      role="toolbar"
      aria-label="Selection actions"
    >
      <button
        type="button"
        onClick={copy}
        className="rounded px-2 py-0.5 text-xs hover:bg-muted"
        title={`Copy selection from ${path} ${label}`}
      >
        {copied ? "Copied" : "Copy"}
      </button>
      <button
        type="button"
        onClick={ask}
        className="rounded bg-primary px-2 py-0.5 text-xs text-primary-foreground hover:opacity-90"
        title={`Attach this highlight (${path} ${label}) as LLM context`}
      >
        Ask LLM
      </button>
    </div>
  );
}
