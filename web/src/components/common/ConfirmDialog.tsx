import { useEffect, useState, type ReactNode } from "react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import { Button } from "../ui/button";

/**
 * ConfirmDialog — the one shared "are you sure?" for destructive actions.
 *
 * Why a rendered dialog and not `window.confirm`: native JS dialogs are not
 * supported in the Wails/WKWebView desktop webview, where `confirm()`
 * SILENTLY RETURNS FALSE. An action guarded by it is therefore unreachable in
 * the desktop app while looking fine in a browser (see the same note in
 * `Files/FileTree.tsx`'s ConfirmDeleteDialog and `Git/GitPanel.tsx`).
 *
 * Contract, depended on by every call site:
 *  - Cancel / Escape / overlay never run the action.
 *  - Confirm runs `onConfirm` exactly once.
 *  - A REJECTED `onConfirm` renders the error inline and KEEPS the dialog
 *    open, so a failed write is never reported as a completed one. This is why
 *    the error is dialog-local rather than the app-wide `ActionErrorToast`:
 *    the user is looking at the dialog, and the dialog can stay put.
 *  - The safe action (Cancel) is the default-focused one
 *    (`data-dialog-default-action`), so Enter on a freshly opened confirm
 *    cancels rather than destroys.
 *
 * This component owns STRUCTURE only — wording stays at each call site.
 * The bespoke confirms already in the app (FileTree's multi-path list,
 * UnifiedTabBar's close-tab copy, GitPanel's discard/revert wording) are
 * intentionally left as they are.
 */
export default function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  pendingLabel,
  confirmVariant = "destructive",
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  /** May be a sentence or a short list of the exact things being dropped. */
  description?: ReactNode;
  confirmLabel: string;
  /** Shown on the confirm button while `onConfirm` is in flight. */
  pendingLabel?: string;
  /**
   * `destructive` (red) is the default and is right for deletes. A dialog
   * that CONFIRMS rather than destroys — the profile rename — passes
   * "default", because a red confirm button mislabels the action.
   */
  confirmVariant?: "destructive" | "default";
  /** Must REJECT on failure so the dialog can stay open with the error. */
  onConfirm: () => Promise<void>;
  onCancel: () => void;
}) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // A fresh open starts clean: a stale error from a previous attempt would
  // read as this one's.
  useEffect(() => {
    if (open) {
      setError(null);
      setPending(false);
    }
  }, [open]);

  if (!open) return null;

  return (
    <Dialog open onOpenChange={(o) => !o && !pending && onCancel()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle className="text-sm">{title}</DialogTitle>
        </DialogHeader>
        {description != null && (
          <div className="text-sm text-muted-foreground break-words">{description}</div>
        )}
        {error && (
          <div role="alert" className="text-xs text-destructive break-words">
            {error}
          </div>
        )}
        <DialogFooter className="gap-2">
          <Button
            variant="ghost"
            onClick={onCancel}
            disabled={pending}
            data-dialog-default-action
          >
            Cancel
          </Button>
          <Button
            variant={confirmVariant}
            disabled={pending}
            onClick={async () => {
              setError(null);
              setPending(true);
              try {
                await onConfirm();
              } catch (err) {
                // Handled by rendering it inline: the dialog stays open so the
                // user can read the reason and retry or cancel.
                // intentionally not logged: the message is shown in the dialog
                // itself, and a console line here would duplicate the one
                // failure the user can already see. Callers that also keep an
                // audit trail (the projectStore mutations) log before re-throwing.
                setError(err instanceof Error && err.message ? err.message : "The action failed");
              } finally {
                setPending(false);
              }
            }}
          >
            {pending ? (pendingLabel ?? "Working…") : confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
