import { useEffect } from "react";
import { X } from "lucide-react";
import { dismissActionError, useActionError } from "../../lib/actionErrors";

/**
 * ActionErrorToast — surfaces a failed settings action (sidebar toggle, model
 * pick) that would otherwise only be logged.
 *
 * Mounted once at the app root so it also works for the ModelDialog, which
 * closes immediately on pick and would unmount a dialog-local error before the
 * user could read it.
 *
 * Deliberately sticky (no auto-dismiss): these failures are the only feedback
 * the user gets that a control silently did nothing, so the message must not
 * vanish while they are reading it. It clears on dismiss or when a newer error
 * replaces it — mirroring the Git panel's "errors stick, successes auto-clear"
 * contract.
 */
export default function ActionErrorToast() {
  const error = useActionError();

  // Escape dismisses, matching the app's dialog/cancel convention.
  useEffect(() => {
    if (!error) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") dismissActionError();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [error]);

  if (!error) return null;

  return (
    <div
      role="alert"
      data-testid="action-error-toast"
      className="fixed bottom-2 inset-x-2 z-50 flex items-start gap-2 rounded-lg border border-red-500/40 bg-card/95 px-3 py-2 text-xs text-red-400 shadow-lg backdrop-blur sm:inset-x-auto sm:left-1/2 sm:max-w-[calc(100vw-1rem)] sm:-translate-x-1/2"
    >
      <span className="flex-1 whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
        {error.message}
      </span>
      <button
        type="button"
        onClick={dismissActionError}
        aria-label="Dismiss error"
        className="flex-shrink-0 rounded p-0.5 text-muted-foreground hover:text-foreground"
      >
        <X className="w-3.5 h-3.5" />
      </button>
    </div>
  );
}
