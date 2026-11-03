import type { FindRect } from "./useDomFind";

/**
 * Highlight overlay for DOM-rendered previews. Rendered as the last child of
 * the scroller (which must be `relative`); rects are content-relative, so the
 * layer scrolls with the document and needs no scroll handler.
 *
 * Colours match the PDF viewer's overlay (all matches yellow, current orange)
 * so find looks identical across surfaces.
 */
export function FindHighlights({ rects, current }: { rects: FindRect[]; current: number }) {
  if (!rects.length) return null;
  return (
    <div className="pointer-events-none absolute inset-0 z-10" aria-hidden="true">
      {rects.map((r, i) => (
        <div
          key={`${r.matchIdx}-${i}`}
          aria-hidden="true"
          className={`pointer-events-none absolute rounded-sm ${
            r.matchIdx === current ? "bg-orange-500/60" : "bg-yellow-300/45"
          }`}
          style={{ left: r.left, top: r.top, width: r.width, height: r.height }}
        />
      ))}
    </div>
  );
}
