"use client";

import { useEffect, useRef, useState } from "react";
import { Printer, TriangleAlert } from "lucide-react";
import type { PreviewKind } from "../../lib/previewKind";
import { cn } from "../../lib/utils";
import { isPrintableKind, buildPrintDocument } from "./printPreview";
import { PrintPreviewDialog } from "./PrintPreviewDialog";

const FAILED_RESET_MS = 2500;

/**
 * Opens an in-app print preview dialog instead of triggering the native OS
 * print dialog. The document is rendered inside the popup; closing it dismisses
 * without printing anything. Renders nothing for kinds that cannot be printed.
 * A failed request shows a warning icon (also in iconOnly mode) and its reason
 * in the tooltip, then resets after a short delay.
 */
export function PrintPreviewButton({ kind, title, load, className, iconOnly }: {
  kind: PreviewKind;
  title: string;
  /** Resolves the document source when the user clicks (live editor text, or a fetch from disk). */
  load: () => string | Promise<string>;
  className?: string;
  iconOnly?: boolean;
}) {
  const [state, setState] = useState<"idle" | "busy" | "failed">("idle");
  const [failureReason, setFailureReason] = useState("");
  const [open, setOpen] = useState(false);
  const [html, setHtml] = useState<string | null>(null);
  const resetTimer = useRef<number | undefined>(undefined);

  useEffect(() => () => window.clearTimeout(resetTimer.current), []);

  if (!isPrintableKind(kind)) return null;

  const onClick = async () => {
    window.clearTimeout(resetTimer.current);
    setState("busy");
    try {
      const documentHtml = await buildPrintDocument(kind, await load(), title);
      setHtml(documentHtml);
      setState("idle");
      setOpen(true);
    } catch (err) {
      console.error("print preview failed", { kind, title }, err);
      setFailureReason(err instanceof Error ? err.message : String(err));
      setState("failed");
      resetTimer.current = window.setTimeout(() => setState("idle"), FAILED_RESET_MS);
    }
  };

  const handleClose = () => {
    setOpen(false);
    setHtml(null);
  };

  const failed = state === "failed";
  const label = state === "busy" ? "Printing…" : failed ? "Print failed" : "Print";
  const Icon = failed ? TriangleAlert : Printer;

  return (
    <>
      <button
        type="button"
        aria-label={failed ? "Print failed" : "Print"}
        title={failed ? `Print failed: ${failureReason}` : "Open print preview"}
        disabled={state === "busy"}
        onClick={onClick}
        className={cn(className, failed && "text-destructive")}
      >
        <Icon className="h-3.5 w-3.5" />
        {!iconOnly && <span>{label}</span>}
      </button>
      <PrintPreviewDialog
        open={open}
        onOpenChange={handleClose}
        html={html}
        title={title}
      />
    </>
  );
}
