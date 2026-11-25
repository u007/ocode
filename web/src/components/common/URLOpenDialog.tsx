import { ExternalLink, Globe2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import { Button } from "../ui/button";

export default function URLOpenDialog({
  open,
  url,
  canOpenInTab,
  onOpenInTab,
  onOpenExternal,
  onCancel,
}: {
  open: boolean;
  url: string;
  canOpenInTab: boolean;
  onOpenInTab: () => void;
  onOpenExternal: () => void;
  onCancel: () => void;
}) {
  if (!open) return null;

  return (
    <Dialog open onOpenChange={(next) => !next && onCancel()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="text-sm">Open link</DialogTitle>
          <DialogDescription>Choose where to open this URL.</DialogDescription>
        </DialogHeader>
        <div className="rounded-md border border-border bg-muted/40 px-3 py-2 font-mono text-xs break-all">
          {url}
        </div>
        <DialogFooter className="gap-2">
          <Button variant="ghost" onClick={onCancel} data-dialog-default-action>
            Cancel
          </Button>
          <Button variant="outline" onClick={onOpenExternal}>
            <ExternalLink className="h-4 w-4" aria-hidden="true" />
            Open in external browser
          </Button>
          <Button
            onClick={onOpenInTab}
            disabled={!canOpenInTab}
            title={
              canOpenInTab
                ? undefined
                : "Select a project to open an embedded browser tab"
            }
          >
            <Globe2 className="h-4 w-4" aria-hidden="true" />
            Open in browser tab
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
