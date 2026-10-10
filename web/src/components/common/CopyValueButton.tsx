import { useEffect, useRef, useState } from "react";
import { Check, Copy, X } from "lucide-react";
import { copyTextToClipboard } from "../../lib/clipboard";
import { cn } from "@/lib/utils";

type CopyState = "idle" | "copied" | "failed";

const flashMs: Record<Exclude<CopyState, "idle">, number> = { copied: 1500, failed: 3000 };

/**
 * A copy button that appears on hover/focus over its parent `group`, used to
 * copy opaque identifiers (today: session IDs) that the UI already shows as
 * plain text but that users regularly need to paste into a bug report, an
 * `/api/sessions/…` call, or a support thread.
 *
 * Layout contract — the button is ALWAYS in the DOM at a fixed size and only its
 * opacity changes. A conditionally-rendered button reflows the surrounding row
 * the instant the pointer arrives, which in the tab strip reshuffles every pill
 * and in the status bar shifts the segments that follow. The tab bar already
 * depends on this rule for its spinner, pending-dot and turn-state slots; this
 * button obeys the same one.
 *
 * Reveal contract — the hidden control stays `pointer-events-none` and opts back
 * in only on group hover or keyboard focus, so the invisible slot can never be
 * clicked by accident. (The tab strip's close button does not do this, which is
 * tolerable for a destructive action behind a confirm dialog but not for a
 * target the user cannot see.)
 *
 * Propagation — the surrounding row is itself clickable (a tab pill switches
 * sessions; a sidebar chat row opens the chat). Pointer-down, pointer-up and
 * click are all stopped: a row that opens on `onPointerUp` rather than `onClick`
 * (the sidebar's chat rows do) would otherwise still navigate after a copy.
 */
export function CopyValueButton({
  value,
  label,
  className,
  testId,
}: {
  /** Text written to the clipboard. An empty value renders nothing at all, so
   *  callers can pass an unresolved identifier without reserving a dead slot. */
  value: string;
  /** Accessible name + tooltip, e.g. "Copy session ID". */
  label: string;
  className?: string;
  testId?: string;
}) {
  const [state, setState] = useState<CopyState>("idle");
  const resetRef = useRef<number | null>(null);

  useEffect(
    () => () => {
      if (resetRef.current !== null) window.clearTimeout(resetRef.current);
    },
    [],
  );

  if (!value) return null;

  const flash = (next: Exclude<CopyState, "idle">) => {
    setState(next);
    if (resetRef.current !== null) window.clearTimeout(resetRef.current);
    resetRef.current = window.setTimeout(() => setState("idle"), flashMs[next]);
  };

  return (
    <button
      type="button"
      data-testid={testId}
      data-state={state}
      aria-label={label}
      title={label}
      onPointerDown={(e) => e.stopPropagation()}
      onPointerUp={(e) => e.stopPropagation()}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        void copyTextToClipboard(value).then((ok) => flash(ok ? "copied" : "failed"));
      }}
      onKeyDown={(e) => {
        if (e.key !== "Enter" && e.key !== " ") return;
        e.preventDefault();
        e.stopPropagation();
      }}
      className={cn(
        "flex h-4 w-4 shrink-0 items-center justify-center rounded p-0.5 transition-opacity",
        "opacity-0 pointer-events-none",
        "group-hover:opacity-100 group-hover:pointer-events-auto",
        "focus-visible:opacity-100 focus-visible:pointer-events-auto",
        state === "failed"
          ? "text-destructive hover:bg-destructive/10"
          : "text-muted-foreground hover:bg-accent hover:text-foreground",
        className,
      )}
    >
      {state === "copied" ? (
        <Check className="w-3 h-3 text-emerald-500" />
      ) : state === "failed" ? (
        <X className="w-3 h-3" />
      ) : (
        <Copy className="w-3 h-3" />
      )}
    </button>
  );
}

/**
 * Convenience wrapper: a `group` span so {@link CopyValueButton} reveals when the
 * pointer is anywhere over the value it belongs to, laid out inline with it.
 */
export function CopyableValue({
  value,
  label,
  valueClassName,
  buttonClassName,
  testId,
  children,
}: {
  value: string;
  label: string;
  /** Classes for the value text itself. */
  valueClassName?: string;
  buttonClassName?: string;
  testId?: string;
  /** Rendered beside the button; defaults to `value`. */
  children?: React.ReactNode;
}) {
  return (
    <span className="group inline-flex min-w-0 items-center gap-1" title={label}>
      <span className={cn("truncate", valueClassName)}>{children ?? value}</span>
      <CopyValueButton value={value} label={label} className={buttonClassName} testId={testId} />
    </span>
  );
}
