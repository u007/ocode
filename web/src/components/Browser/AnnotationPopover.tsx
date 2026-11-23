import { useEffect, useRef, useState } from "react";
import { Camera, Loader2, X } from "lucide-react";

export interface AnnotationPopoverProps {
  /** Viewport coordinates of the click that opened it. */
  x: number;
  y: number;
  /** Short element label for the heading, e.g. `button#save.primary`. */
  label: string;
  /** False on surfaces without a faithful screenshot (the local iframe still
   *  supplies one, so this is only for a failed/unsupported capture). */
  canScreenshot: boolean;
  /** Screenshot availability detail, shown when it is not available. */
  screenshotHint?: string;
  busy?: boolean;
  error?: string | null;
  onSubmit: (note: string, includeScreenshot: boolean) => void;
  onCancel: () => void;
}

const WIDTH = 320;
const HEIGHT = 208;

/** Small note composer anchored at the annotated element. It never sends:
 *  submitting hands the note (and an optional screenshot) to the chat
 *  composer, where the user can edit and send it. */
export function AnnotationPopover({
  x,
  y,
  label,
  canScreenshot,
  screenshotHint,
  busy = false,
  error = null,
  onSubmit,
  onCancel,
}: AnnotationPopoverProps) {
  const ref = useRef<HTMLDivElement | null>(null);
  const [note, setNote] = useState("");
  const [shot, setShot] = useState(canScreenshot);

  useEffect(() => {
    const onDown = (ev: MouseEvent) => {
      if (ref.current && !ref.current.contains(ev.target as Node)) onCancel();
    };
    document.addEventListener("mousedown", onDown, true);
    return () => document.removeEventListener("mousedown", onDown, true);
  }, [onCancel]);

  const left = Math.max(8, Math.min(x, (window.innerWidth || WIDTH) - WIDTH - 8));
  const top = Math.max(8, Math.min(y, (window.innerHeight || HEIGHT) - HEIGHT - 8));

  return (
    <div
      ref={ref}
      role="dialog"
      aria-label="Add annotation"
      data-testid="annotation-popover"
      className="fixed z-[70] w-80 rounded-lg border border-neutral-300 bg-white p-3 text-xs shadow-xl dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-200"
      style={{ left, top }}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          onCancel();
        }
      }}
    >
      <div className="mb-2 flex items-center justify-between gap-2">
        <div className="min-w-0">
          <div className="font-semibold text-neutral-800 dark:text-neutral-100">Add annotation</div>
          <div className="truncate font-mono text-[10px] text-neutral-500" title={label}>
            {label}
          </div>
        </div>
        <button
          type="button"
          aria-label="Cancel annotation"
          onClick={onCancel}
          className="rounded p-0.5 text-neutral-500 hover:bg-neutral-100 hover:text-neutral-800 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>
      <textarea
        autoFocus
        aria-label="Note"
        value={note}
        onChange={(e) => setNote(e.target.value)}
        placeholder="What should change here?"
        rows={3}
        className="w-full resize-none rounded border border-neutral-300 bg-white px-2 py-1 text-xs text-neutral-900 placeholder:text-neutral-400 focus:border-sky-500 focus:outline-none dark:border-neutral-700 dark:bg-neutral-950 dark:text-neutral-100"
      />
      <label className="mt-2 flex items-center gap-1.5 text-[11px] text-neutral-600 dark:text-neutral-400">
        <input
          type="checkbox"
          checked={shot && canScreenshot}
          disabled={!canScreenshot || busy}
          onChange={(e) => setShot(e.target.checked)}
        />
        <Camera className="h-3 w-3" />
        Attach screenshot
        {!canScreenshot && screenshotHint ? <span className="text-neutral-500">— {screenshotHint}</span> : null}
      </label>
      {error ? (
        <div role="alert" className="mt-2 rounded bg-red-50 px-2 py-1 text-[11px] text-red-700 dark:bg-red-950/40 dark:text-red-300">
          {error}
        </div>
      ) : null}
      <div className="mt-2 flex items-center justify-end gap-2">
        <button
          type="button"
          onClick={onCancel}
          className="rounded px-2 py-1 text-[11px] text-neutral-600 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800"
        >
          Cancel
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={() => onSubmit(note, shot && canScreenshot)}
          className="flex items-center gap-1 rounded bg-sky-600 px-2.5 py-1 text-[11px] font-medium text-white hover:bg-sky-700 disabled:opacity-60"
        >
          {busy ? <Loader2 className="h-3 w-3 animate-spin" /> : null}
          Add
        </button>
      </div>
    </div>
  );
}
