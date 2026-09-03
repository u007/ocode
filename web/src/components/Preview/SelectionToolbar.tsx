import { useCallback, useEffect, useRef, useState } from "react";
import { dispatchPreviewContext } from "../../lib/previewKind";

export interface TextSelection {
  text: string;
  rect: { top: number; left: number };
}

/**
 * Shared highlight → toolbar flow for every PreviewHost renderer.
 * Listens for mouseup inside the container, keeps the selection only when
 * it is non-collapsed and anchored in the container, and exposes a floating
 * toolbar anchor. Copy writes the clipboard; Ask LLM pushes a
 * `ocode:preview-context` event (file + label + excerpt) that App funnels
 * into the chat composer's context chip.
 */
export function usePreviewSelection<T extends HTMLElement>(label: () => string) {
  const ref = useRef<T | null>(null);
  const [sel, setSel] = useState<TextSelection | null>(null);
  const labelRef = useRef(label);
  labelRef.current = label;

  const onMouseUp = useCallback(() => {
    // Let the browser finish the selection before reading it.
    requestAnimationFrame(() => {
      const s = window.getSelection();
      const el = ref.current;
      if (!s || s.isCollapsed || !el || !s.toString().trim()) {
        setSel(null);
        return;
      }
      const anchor = s.anchorNode ? (s.anchorNode instanceof Element ? s.anchorNode : s.anchorNode.parentElement) : null;
      if (!anchor || !el.contains(anchor)) {
        setSel(null);
        return;
      }
      const r = s.getRangeAt(0).getBoundingClientRect();
      setSel({ text: s.toString().trim().slice(0, 2000), rect: { top: r.top, left: r.left + r.width / 2 } });
    });
  }, []);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.addEventListener("mouseup", onMouseUp);
    return () => el.removeEventListener("mouseup", onMouseUp);
  }, [onMouseUp]);

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
  onDone,
}: {
  sel: TextSelection;
  path: string;
  label: string;
  projectRoot?: string;
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
    dispatchPreviewContext({ path, label, excerpt: sel.text, projectRoot });
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
