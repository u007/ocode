import { Loader2 } from "lucide-react";

/**
 * Small theme-aware loading indicator for browser states (address bar, CDP
 * viewport, browser tabs). Uses a chrome token color so it adapts to both
 * themes, and honors prefers-reduced-motion by freezing the spin.
 * Size is passed as Tailwind classes (default ~14px, browsers' compact scale).
 */
export function LoadingSpinner({ className = "w-3.5 h-3.5" }: { className?: string }) {
  return (
    <Loader2
      aria-hidden
      className={`animate-spin motion-reduce:animate-none text-muted-foreground shrink-0 ${className}`}
    />
  );
}