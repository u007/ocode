import { useCallback, useEffect, useRef, useState } from "react";
import { Loader2, Volume2 } from "lucide-react";
import type { SpeechOutcome } from "./SpeechProvider";

/** A speak action: requestSpeech (or the provider's speak) returns an outcome
 *  promise, but isolated callers/tests may return nothing. */
export type SpeakAction = (text: string) => Promise<SpeechOutcome> | void;

/**
 * Drives one Speak button: disabled + spinner while the request is being
 * summarised/synthesised, then re-enabled with an inline error when it fails.
 *
 * The outcome resolves at playback START, not at the end of a long read, so the
 * button is only blocked for the "processing" window. `pendingRef` guards
 * double-fire in the handler itself (keyboard repeat / a second click before
 * React re-renders), not just via the `disabled` attribute.
 */
export function useSpeakAction(onSpeak: SpeakAction) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const mounted = useRef(true);
  const pendingRef = useRef(false);
  useEffect(() => () => {
    mounted.current = false;
  }, []);

  const run = useCallback(
    async (text: string) => {
      if (pendingRef.current) return;
      pendingRef.current = true;
      setPending(true);
      setError(null);
      try {
        const outcome = await onSpeak(text);
        if (outcome && !outcome.ok) {
          const message = outcome.error || "Speech failed";
          console.warn("speak failed", message);
          if (mounted.current) setError(message);
        }
      } catch (err) {
        // A throwing caller must still re-enable the button; a stuck spinner
        // is worse than the error itself, and the error must not be silent.
        const message = err instanceof Error ? err.message : String(err);
        console.warn("speak failed", err);
        if (mounted.current) setError(message);
      } finally {
        pendingRef.current = false;
        if (mounted.current) setPending(false);
      }
    },
    [onSpeak],
  );

  return { pending, error, run };
}

/**
 * The shared Speak button. `getText` is read on click so the caller can extract
 * the RENDERED text from the DOM (see speechUtils) instead of the raw markdown.
 */
export function SpeakButton({
  getText,
  onSpeak,
  ariaLabel,
  title,
  idleLabel = "Speak",
  pendingLabel = "Speaking…",
  className,
  disabled = false,
}: {
  getText: () => string | undefined | null;
  onSpeak: SpeakAction;
  ariaLabel: string;
  title: string;
  idleLabel?: string;
  pendingLabel?: string;
  className?: string;
  /** Additional external disable condition (e.g. nothing selected). */
  disabled?: boolean;
}) {
  const { pending, error, run } = useSpeakAction(onSpeak);
  return (
    <>
      <button
        type="button"
        aria-label={ariaLabel}
        aria-busy={pending}
        title={error ? `${title} — ${error}` : title}
        data-speech-exclude=""
        disabled={pending || disabled}
        onClick={() => {
          const text = getText();
          if (text && text.trim()) void run(text);
        }}
        className={className}
      >
        {pending ? (
          <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
        ) : (
          <Volume2 className="h-3.5 w-3.5" aria-hidden="true" />
        )}
        {pending ? pendingLabel : idleLabel}
      </button>
      {error ? (
        <span role="status" aria-live="polite" className="ml-2 text-xs text-destructive">
          {error}
        </span>
      ) : null}
    </>
  );
}
