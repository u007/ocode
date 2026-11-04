import { useCallback, useEffect, useMemo, useState } from "react";
import { api } from "../../api/client";
import { SelectionToolbar, usePreviewSelection } from "./SelectionToolbar";
import { parsePptx, type DeckBlock } from "./pptxDeck";

/** A slide ready to draw: its images are URLs the page can load. */
export interface SlideView {
  blocks: DeckBlock[];
  images: string[];
  aspect: number;
}

interface Slide extends SlideView {
  index: number;
}

/**
 * One slide as a card. The viewer and the print document both render it, so
 * the printed slide is the one on screen. `forPresent` sizes it for the
 * fullscreen view; `revealed` steps through its blocks and images there.
 */
export function SlideCard({
  slide,
  number,
  forPresent,
  revealAll,
  revealed,
}: {
  slide: SlideView;
  number: number;
  forPresent: boolean;
  revealAll: boolean;
  revealed: number;
}) {
  return (
    <div
      className={`w-full overflow-hidden rounded border border-border bg-card ${forPresent ? "max-h-full" : ""}`}
      style={{ aspectRatio: `${slide.aspect}` }}
    >
      <div className={`flex h-full flex-col gap-2 overflow-auto ${forPresent ? "p-8 text-lg" : "p-4 text-sm"}`}>
        {slide.blocks.map((b, i) => (
          <div
            key={i}
            className={`pptx-block rounded px-1 ${!revealAll && forPresent && i >= revealed ? "pptx-hidden" : ""}`}
          >
            {b.text}
          </div>
        ))}
        {slide.images.map((u, i) => (
          <img
            key={`img-${i}`}
            src={u}
            alt={`Slide ${number} image ${i + 1}`}
            className={`max-h-48 rounded object-contain ${!revealAll && forPresent && slide.blocks.length + i >= revealed ? "pptx-hidden" : ""}`}
          />
        ))}
        {slide.blocks.length === 0 && slide.images.length === 0 && (
          <div className="text-xs text-muted-foreground">(blank slide)</div>
        )}
      </div>
    </div>
  );
}

/**
 * PowerPoint viewer (jszip — stable/maintained — unzips the .pptx package via
 * parsePptx; slides render as structured cards from the slide XML).
 *
 * - Filmstrip + slide view with pager, multi-slide decks fully navigable.
 * - Present mode: fullscreen click-through (click/→ advances, ← back,
 *   Esc exits). Each click first steps through the slide's blocks with a
 *   fade/slide entrance transition — an approximation of PowerPoint
 *   entrance animations. Pixel-perfect native playback is one click away
 *   via "Open with OS app" (Keynote/PowerPoint), which the PreviewHost
 *   toolbar provides.
 */
export default function PptxViewer({
  path,
  projectRoot,
  projectHost,
  slide,
  onSlideChange,
}: {
  path: string;
  projectRoot?: string;
  projectHost?: string;
  slide: number;
  onSlideChange: (slide: number) => void;
}) {
  const [slides, setSlides] = useState<Slide[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [presenting, setPresenting] = useState(false);
  const [revealed, setRevealed] = useState(0);
  const urlsRef = useMemo(() => new Set<string>(), []);
  const { ref, sel, clear } = usePreviewSelection<HTMLDivElement>(() => `slide ${slide}`);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setSlides([]);
    // Revoke previous blob URLs before loading a new deck.
    urlsRef.forEach((u) => URL.revokeObjectURL(u));
    urlsRef.clear();

    (async () => {
      const parsed = await parsePptx(await api.fetchFileRaw(path, projectRoot, projectHost));
      if (cancelled) return;
      setSlides(
        parsed.map((slide) => ({
          ...slide,
          images: slide.images.map((blob) => {
            const url = URL.createObjectURL(blob);
            urlsRef.add(url);
            return url;
          }),
        })),
      );
      setLoading(false);
    })().catch((e) => {
      if (!cancelled) {
        setError(e instanceof Error ? e.message : String(e));
        setLoading(false);
      }
    });

    return () => {
      cancelled = true;
    };
  }, [path, projectRoot, projectHost, urlsRef]);

  useEffect(() => {
    setRevealed(0);
    clear();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [slide]);

  const total = slides.length;
  const safe = Math.min(Math.max(1, slide), Math.max(1, total));
  const current = slides[safe - 1];

  const stepForward = useCallback(() => {
    if (!current) return;
    const steps = current.blocks.length + current.images.length;
    if (revealed < steps) setRevealed(revealed + 1);
    else if (safe < total) {
      onSlideChange(safe + 1);
      setRevealed(0);
    }
  }, [current, revealed, safe, total, onSlideChange]);

  const stepBack = useCallback(() => {
    if (revealed > 0) setRevealed(revealed - 1);
    else if (safe > 1) onSlideChange(safe - 1);
  }, [revealed, safe, onSlideChange]);

  if (loading) return <div className="p-4 text-xs text-muted-foreground">Loading deck…</div>;
  if (error) return <div className="p-4 text-xs text-red-400">Deck failed: {error}</div>;
  if (!current) return <div className="p-4 text-xs text-muted-foreground">No slides.</div>;

  const steps = current.blocks.length + current.images.length;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b border-border px-2 py-1 text-xs">
        <button type="button" disabled={safe <= 1} onClick={() => onSlideChange(safe - 1)} className="rounded px-1.5 py-0.5 hover:bg-muted disabled:opacity-40" aria-label="Previous slide">←</button>
        <span className="text-muted-foreground">Slide {safe} / {total}</span>
        <button type="button" disabled={safe >= total} onClick={() => onSlideChange(safe + 1)} className="rounded px-1.5 py-0.5 hover:bg-muted disabled:opacity-40" aria-label="Next slide">→</button>
        <button
          type="button"
          onClick={() => {
            setRevealed(0);
            setPresenting(true);
          }}
          className="ml-auto rounded bg-primary px-2 py-0.5 text-primary-foreground hover:opacity-90"
          title="Fullscreen click-through presentation"
        >
          Present
        </button>
      </div>
      <div className="flex min-h-0 flex-1">
        <div className="flex w-20 shrink-0 flex-col gap-1 overflow-auto border-r border-border p-1.5" aria-label="Slides filmstrip">
          {slides.map((s) => (
            <button
              key={s.index}
              type="button"
              onClick={() => onSlideChange(s.index)}
              className={`rounded border p-1 text-left text-[10px] leading-tight hover:bg-muted ${s.index === safe ? "border-primary bg-muted" : "border-border"}`}
              title={`Slide ${s.index}`}
            >
              <span className="font-mono text-muted-foreground">{s.index}</span>
              <span className="block truncate">{s.blocks[0]?.text || "(blank)"}</span>
            </button>
          ))}
        </div>
        <div ref={ref} className="min-w-0 flex-1 overflow-auto p-2">
          <SlideCard slide={current} number={safe} forPresent={false} revealAll revealed={revealed} />
        </div>
      </div>
      <style>{`.pptx-block{transition:opacity .35s ease,transform .35s ease;}.pptx-hidden{opacity:0;transform:translateY(8px);}`}</style>
      {sel && <SelectionToolbar sel={sel} path={path} label={`slide ${safe}`} projectRoot={projectRoot} projectHost={projectHost} onDone={clear} />}
      {presenting && (
        <div
          className="fixed inset-0 z-50 flex flex-col bg-black/95 p-6"
          onClick={stepForward}
          onKeyDown={(e) => {
            if (e.key === "Escape") setPresenting(false);
            else if (e.key === "ArrowRight" || e.key === " ") {
              e.preventDefault();
              stepForward();
            } else if (e.key === "ArrowLeft") {
              e.preventDefault();
              stepBack();
            }
          }}
          tabIndex={0}
          role="dialog"
          aria-label={`Presenting slide ${safe} of ${total}`}
        >
          <div className="mb-3 flex items-center gap-2 text-xs text-white/60">
            <span>Slide {safe} / {total}{steps > 0 ? ` · step ${Math.min(revealed, steps)}/${steps}` : ""}</span>
            <span className="ml-auto">Click or → to advance · ← back · Esc exits</span>
            <button type="button" onClick={() => setPresenting(false)} className="rounded bg-white/10 px-2 py-0.5 text-white hover:bg-white/20">Exit</button>
          </div>
          <div className="flex min-h-0 flex-1 items-center justify-center" onClick={(e) => e.stopPropagation()}>
            <div className="w-full max-w-4xl text-white" onClick={stepForward}>
              <SlideCard slide={current} number={safe} forPresent revealAll={false} revealed={revealed} />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
