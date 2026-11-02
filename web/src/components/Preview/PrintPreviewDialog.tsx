"use client";

import * as React from "react";
import { Dialog, DialogContent, DialogClose, DialogTitle } from "@/components/ui/dialog";
import { X } from "lucide-react";

export interface PrintPreviewDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Standalone HTML document to render (markdown/text/html/json previews). */
  html?: string | null;
  /** Document URL to render inline (PDF blob URL). */
  src?: string | null;
  title?: string;
  /** Extra content rendered below the preview frame (e.g. an error message). */
  children?: React.ReactNode;
}

/**
 * In-app print preview dialog. Replaces the previous behaviour of opening the
 * native OS print dialog immediately. The document is rendered in an iframe so
 * the user can review it before deciding what to do; there is no automatic
 * native print sheet. Close dismisses without printing.
 */
export function PrintPreviewDialog({
  open,
  onOpenChange,
  html,
  src,
  title = "Print preview",
  children,
}: PrintPreviewDialogProps) {
  const body = React.useMemo(() => {
    if (html) {
      return (
        <iframe className="w-full h-full border-0" srcDoc={html} sandbox="" title="Print preview" />
      );
    }
    if (src) {
      return <iframe className="w-full h-full border-0" src={src} sandbox="" title="Print preview" />;
    }
    return null;
  }, [html, src]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-full max-w-[1000px] max-h-[85vh] sm:max-w-[1200px] flex flex-col">
        <div className="flex shrink-0 items-center justify-between gap-2 border-b border-border px-4 py-2">
          <DialogTitle className="text-sm font-medium">{title}</DialogTitle>
          <DialogClose asChild>
            <button
              className="rounded-sm p-1 text-muted-foreground hover:bg-muted"
              aria-label="Close preview"
              data-dialog-default-action
            >
              <X className="h-4 w-4" />
            </button>
          </DialogClose>
        </div>
        <div className="flex-1 min-h-0 bg-background overflow-auto p-4">
          {body}
          {children}
        </div>
      </DialogContent>
    </Dialog>
  );
}
