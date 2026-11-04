import { useEffect, useRef, useState } from "react";
import { Printer, TriangleAlert } from "lucide-react";
import type { PreviewKind } from "../../lib/previewKind";
import { cn } from "../../lib/utils";
import { isPrintableKind, printPreview, type PrintSource } from "./printPreview";

const FAILED_RESET_MS = 2500;

/**
 * Prints a preview's rendered document, or saves it as PDF from the print
 * dialog. The document is read at click time from `source`, so the button
 * always prints what the user sees now. Renders nothing for kinds that cannot
 * be printed. A failed request shows a warning icon (also in iconOnly mode) and
 * its reason in the tooltip, then resets after a short delay.
 */
export function PrintPreviewButton({ kind, title, source, className, iconOnly }: {
  kind: PreviewKind;
  title: string;
  /** Where the document comes from: live editor text, or the saved file. */
  source: PrintSource;
  className?: string;
  iconOnly?: boolean;
}) {
  const [state, setState] = useState<"idle" | "busy" | "failed">("idle");
  const [failureReason, setFailureReason] = useState("");
  const resetTimer = useRef<number | undefined>(undefined);

  useEffect(() => () => window.clearTimeout(resetTimer.current), []);

  if (!isPrintableKind(kind)) return null;

  const onClick = async () => {
    window.clearTimeout(resetTimer.current);
    setState("busy");
    try {
      await printPreview({ kind, title, source });
      setState("idle");
    } catch (err) {
      console.error("print preview failed", { kind, title }, err);
      setFailureReason(err instanceof Error ? err.message : String(err));
      setState("failed");
      resetTimer.current = window.setTimeout(() => setState("idle"), FAILED_RESET_MS);
    }
  };

  const failed = state === "failed";
  const label = state === "busy" ? "Printing…" : failed ? "Print failed" : "Print";
  const Icon = failed ? TriangleAlert : Printer;

  return (
    <button
      type="button"
      aria-label={failed ? "Print failed" : "Print"}
      title={failed ? `Print failed: ${failureReason}` : "Print or save as PDF"}
      disabled={state === "busy"}
      onClick={onClick}
      className={cn(className, failed && "text-destructive")}
    >
      <Icon className="h-3.5 w-3.5" />
      {!iconOnly && <span>{label}</span>}
    </button>
  );
}
