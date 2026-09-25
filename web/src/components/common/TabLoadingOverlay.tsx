import { AlertCircle, Loader2 } from "lucide-react";

interface TabLoadingOverlayProps {
  active: boolean;
  label?: string;
  error?: string;
  onRetry?: () => void;
}

/** Blocking initial-load surface with an accessible retry state. */
export function TabLoadingOverlay({
  active,
  label = "Loading",
  error,
  onRetry,
}: TabLoadingOverlayProps) {
  if (!active && !error) return null;
  const isError = Boolean(error);
  return (
    <div
      role="status"
      aria-live="polite"
      aria-busy={active ? "true" : "false"}
      className="absolute inset-0 z-20 flex flex-col items-center justify-center gap-2 bg-background/80 px-6 text-center backdrop-blur-[1px]"
    >
      {isError ? (
        <>
          <AlertCircle aria-hidden className="h-5 w-5 shrink-0 text-red-400" />
          <p className="text-sm text-red-300">{error}</p>
          {onRetry && (
            <button
              type="button"
              onClick={onRetry}
              className="rounded-md border border-border px-3 py-1.5 text-sm font-medium hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              Retry
            </button>
          )}
        </>
      ) : (
        <>
          <Loader2
            aria-hidden
            className="h-5 w-5 shrink-0 animate-spin motion-reduce:animate-none text-muted-foreground"
          />
          <p className="text-sm text-muted-foreground">{label}</p>
        </>
      )}
    </div>
  );
}
