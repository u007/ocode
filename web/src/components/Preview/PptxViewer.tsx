import { useCallback, useEffect, useMemo, useState } from "react";
import JSZip from "jszip";
import { api } from "../../api/client";
import { SelectionToolbar, usePreviewSelection } from "./SelectionToolbar";

interface SlideBlock {
  text: string;
}

interface Slide {
  index: number;
  blocks: SlideBlock[];
  images: string[];
  aspect: number;
}

function xmlDoc(text: string): Document {
  return new DOMParser().parseFromString(text, "application/xml");
}

/** Resolve a rels Target (e.g. "../media/image1.png") against its base dir. */
function resolveTarget(baseDir: string, target: string): string {
  const parts = `${baseDir}/${target}`.split("/");
  const out: string[] = [];
  for (const p of parts) {
    if (p === "..") out.pop();
    else if (p !== ".") out.push(p);
  }
  return out.join("/");
}

/**
 * PowerPoint viewer (jszip — stable/maintained — unzips the .pptx package;
 * slides render as structured cards from the slide XML).
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
  slide,
  onSlideChange,
}: {
  path: string;
  projectRoot?: string;
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
      const buf = await api.fetchFileRaw(path, projectRoot);
      const zip = await JSZip.loadAsync(buf);

      const presXml = await zip.file("ppt/presentation.xml")?.async("string");
      if (!presXml) throw new Error("not a readable .pptx (missing presentation.xml)");
      const pres = xmlDoc(presXml);

      let aspect = 16 / 9;
      const sz = pres.getElementsByTagName("p:sldSz")[0];
      if (sz) {
        const cx = parseFloat(sz.getAttribute("cx") || "0");
        const cy = parseFloat(sz.getAttribute("cy") || "0");
        if (cx > 0 && cy > 0) aspect = cx / cy;
      }

      const relsXml = await zip.file("ppt/_rels/presentation.xml.rels")?.async("string");
      const rels = relsXml ? xmlDoc(relsXml) : null;
      const idToTarget = new Map<string, string>();
      if (rels) {
        const r = rels.getElementsByTagName("Relationship");
        for (let i = 0; i < r.length; i++) {
          const id = r[i].getAttribute("Id") || "";
          const target = r[i].getAttribute("Target") || "";
          if (id && target) idToTarget.set(id, target.startsWith("ppt/") ? target : `ppt/${target}`);
        }
      }

      const order: string[] = [];
      const sldIds = pres.getElementsByTagName("p:sldId");
      for (let i = 0; i < sldIds.length; i++) {
        const rid = sldIds[i].getAttribute("r:id") || "";
        const target = idToTarget.get(rid);
        if (target) order.push(target);
      }
      if (order.length === 0) throw new Error("no slides found in deck");

      const parsed: Slide[] = [];
      for (let si = 0; si < order.length; si++) {
        const slidePath = order[si];
        const slideXml = await zip.file(slidePath)?.async("string");
        if (!slideXml) continue;
        const doc = xmlDoc(slideXml);
        const blocks: SlideBlock[] = [];
        const paras = doc.getElementsByTagName("a:p");
        for (let pi = 0; pi < paras.length; pi++) {
          const runs = paras[pi].getElementsByTagName("a:t");
          let text = "";
          for (let ti = 0; ti < runs.length; ti++) text += runs[ti].textContent ?? "";
          text = text.trim();
          if (text) blocks.push({ text });
        }

        const images: string[] = [];
        const baseDir = slidePath.slice(0, slidePath.lastIndexOf("/"));
        const relName = slidePath.slice(slidePath.lastIndexOf("/") + 1);
        const slideRels = await zip.file(`${baseDir}/_rels/${relName}.rels`)?.async("string");
        if (slideRels) {
          const rd = xmlDoc(slideRels);
          const rr = rd.getElementsByTagName("Relationship");
          for (let i = 0; i < rr.length; i++) {
            const type = rr[i].getAttribute("Type") || "";
            const target = rr[i].getAttribute("Target") || "";
            if (!type.endsWith("/image") || !target) continue;
            const resolved = target.startsWith("..") ? resolveTarget(baseDir, target) : `${baseDir}/${target}`;
            const file = zip.file(resolved);
            if (!file) continue;
            const blob = await file.async("blob");
            const url = URL.createObjectURL(blob);
            urlsRef.add(url);
            images.push(url);
          }
        }
        parsed.push({ index: si + 1, blocks, images, aspect });
      }
      if (cancelled) return;
      setSlides(parsed);
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
  }, [path, projectRoot, urlsRef]);

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

  const slideCard = (forPresent: boolean, revealAll: boolean) => (
    <div
      className={`w-full overflow-hidden rounded border border-border bg-card ${forPresent ? "max-h-full" : ""}`}
      style={{ aspectRatio: `${current.aspect}` }}
    >
      <div className={`flex h-full flex-col gap-2 overflow-auto ${forPresent ? "p-8 text-lg" : "p-4 text-sm"}`}>
        {current.blocks.map((b, i) => (
          <div
            key={i}
            className={`pptx-block rounded px-1 ${!revealAll && forPresent && i >= revealed ? "pptx-hidden" : ""}`}
          >
            {b.text}
          </div>
        ))}
        {current.images.map((u, i) => (
          <img
            key={`img-${i}`}
            src={u}
            alt={`Slide ${safe} image ${i + 1}`}
            className={`max-h-48 rounded object-contain ${!revealAll && forPresent && current.blocks.length + i >= revealed ? "pptx-hidden" : ""}`}
          />
        ))}
        {current.blocks.length === 0 && current.images.length === 0 && (
          <div className="text-xs text-muted-foreground">(blank slide)</div>
        )}
      </div>
    </div>
  );

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
          {slideCard(false, true)}
        </div>
      </div>
      <style>{`.pptx-block{transition:opacity .35s ease,transform .35s ease;}.pptx-hidden{opacity:0;transform:translateY(8px);}`}</style>
      {sel && <SelectionToolbar sel={sel} path={path} label={`slide ${safe}`} projectRoot={projectRoot} onDone={clear} />}
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
              {slideCard(true, false)}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
