import { Loader2 } from "lucide-react";

interface TabLoadingIndicatorProps {
  active: boolean;
  label?: string;
  error?: boolean;
}

/** Compact inline loading/error affordance for a tab trigger. */
export function TabLoadingIndicator({
  active,
  label = "Loading",
  error = false,
}: TabLoadingIndicatorProps) {
  if (!active && !error) return null;
  return (
    <span
      role="status"
      aria-live="polite"
      data-error={error ? "true" : undefined}
      className="inline-flex shrink-0 items-center"
    >
      {error ? (
        <span aria-hidden className="h-1.5 w-1.5 rounded-full bg-red-400" />
      ) : (
        <Loader2
          aria-hidden
          className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none text-muted-foreground"
        />
      )}
      <span className="sr-only">{label}</span>
    </span>
  );
}
