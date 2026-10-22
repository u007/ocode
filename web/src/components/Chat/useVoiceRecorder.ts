import { useCallback, useEffect, useRef, useState } from "react";
import type { STTTranscribeResult } from "@/api/types";

/**
 * Voice input for the chat composer: record with MediaRecorder, upload the
 * clip for transcription, hand the text back. The hook owns only the recording
 * lifecycle; the caller supplies the transcribe call and what to do with the
 * text, which keeps this testable without a server or a real microphone.
 */

export type VoiceState = "idle" | "recording" | "transcribing";

export const VOICE_UNAVAILABLE_MESSAGE =
  "Voice input is unavailable: this browser cannot record microphone audio.";
export const VOICE_NOTHING_HEARD_MESSAGE = "Nothing was heard. Try again.";
/** MediaRecorder timeslice: one chunk per second, so the accumulated chunks are always a playable clip. */
export const VOICE_TIMESLICE_MS = 1000;
/** How often a live partial transcript is requested while recording. */
export const VOICE_PARTIAL_INTERVAL_MS = 3000;
/** How long a press must be held before recording locks into continuous mode:
 *  release won't stop it; the user clicks the stop button to stop & send. */
export const VOICE_HOLD_THRESHOLD_MS = 2000;

/** Preferred recording formats, first supported one wins. Anything else falls
 *  back to the browser default (no mimeType passed to MediaRecorder). */
const MIME_PREFERENCE = ["audio/webm;codecs=opus", "audio/mp4"] as const;

export function isVoiceRecordingSupported(): boolean {
  if (typeof navigator === "undefined") return false;
  const hasGetUserMedia = typeof navigator.mediaDevices?.getUserMedia === "function";
  return hasGetUserMedia && typeof globalThis.MediaRecorder === "function";
}

/** Returns the first preferred MIME type the browser can record, or undefined
 *  to let MediaRecorder choose its default. */
export function pickRecordingMimeType(): string | undefined {
  const Recorder = globalThis.MediaRecorder;
  if (typeof Recorder?.isTypeSupported !== "function") return undefined;
  return MIME_PREFERENCE.find((type) => Recorder.isTypeSupported(type));
}

/** File extension matching the container of a recorded blob. The server sniffs
 *  the bytes, but the extension keeps the multipart upload self-describing. */
export function audioExtensionFor(mimeType: string): string {
  const type = mimeType.toLowerCase();
  if (type.includes("mp4") || type.includes("m4a")) return ".mp4";
  if (type.includes("ogg")) return ".ogg";
  if (type.includes("wav")) return ".wav";
  return ".webm";
}

/** Turns a getUserMedia / MediaRecorder failure into a sentence the user can act on. */
export function describeMicError(err: unknown): string {
  const name = (err as { name?: unknown } | null)?.name;
  switch (name) {
    case "NotAllowedError":
    case "SecurityError":
      return "Microphone permission was denied. Allow microphone access for this site and try again.";
    case "NotFoundError":
    case "OverconstrainedError":
      return "No microphone was found.";
    case "NotReadableError":
      return "The microphone is in use by another application.";
    default:
      return err instanceof Error && err.message ? err.message : "Could not start recording.";
  }
}

export interface UseVoiceRecorderOptions {
  /** Uploads the clip. Resolves with the transcript; an empty text means nothing was heard. */
  /** `preview` marks a live partial: it must never prompt or install, and its
   *  failure is ignored. */
  transcribe: (audio: Blob, filename: string, preview?: boolean) => Promise<STTTranscribeResult>;
  /** Receives a non-empty transcript. Never called for an empty one. Partials never reach it. */
  onTranscript: (text: string) => void;
  /**
   * Resolves true when live previews may run. Each preview re-uploads the whole
   * clip, so only a local engine should get them. Absent or failing means off.
   */
  canPreview?: () => Promise<boolean>;
  /**
   * Starts loading the local model in the background so the first
   * transcription does not wait for the model load. Best-effort: a rejection
   * is ignored, because the transcription starts the worker itself.
   */
  prewarm?: () => Promise<unknown>;
  /** Aborts an in-flight prewarm when the recording is discarded, so ~2 GB
   *  does not stay resident for a clip nobody will transcribe. Best-effort. */
  cancelPrewarm?: () => Promise<unknown>;
}

export interface VoiceRecorder {
  /** False when getUserMedia or MediaRecorder is missing. */
  supported: boolean;
  state: VoiceState;
  /** Live preview of the recording so far. Display only: never sent, never put in the draft. */
  partialText: string;
  /** True while the user is holding the button but the hold threshold hasn't
   *  fired yet (visual "press to lock" cue). */
  holding: boolean;
  error: string | null;
  /** Starts when idle, stops when recording, does nothing while transcribing. */
  start: () => Promise<void>;
  /** Stops the recording and transcribes it. No-op unless recording. */
  stop: () => void;
  /** Stops the recording and discards the clip: no transcription, back to idle. */
  cancel: () => void;
  /** Call on press (mousedown/keydown): arms the hold threshold when idle,
   *  or records a stop-intent press when already recording. */
  press: () => void;
  /** Call on release (mouseup/keyup): a quick press starts recording; a held
   *  press locks continuous mode (release does nothing); a press while
   *  recording stops & sends. */
  release: () => void;
  /** Clears an armed hold timer without starting anything (pointer left the
   *  button while holding). */
  cancelPress: () => void;
  /** Clears the last error notice. The composer renders it as a dismissible
   *  banner, so a stale failure (mic denied, transcription failed, nothing
   *  heard) can be closed without starting another recording. */
  dismissError: () => void;
}

export function useVoiceRecorder(options: UseVoiceRecorderOptions): VoiceRecorder {
  const { transcribe, onTranscript } = options;
  const supported = isVoiceRecordingSupported();
  const [state, setState] = useState<VoiceState>("idle");
  const [error, setError] = useState<string | null>(null);
  const [partialText, setPartialTextState] = useState("");

  // stateRef is the synchronous source of truth for guards (a double click
  // must not start two recorders); `state` only drives rendering.
  const stateRef = useRef<VoiceState>("idle");
  const recorderRef = useRef<MediaRecorder | null>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const mountedRef = useRef(false);
  // No live audio monitoring: the mic is never played back to the
  // speakers. Recording streams the transcript preview instead.
  // Interval that requests live partials while recording.
  const partialTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  // At most one partial request in flight across the hook.
  const partialInFlightRef = useRef(false);
  // Bumped whenever a recording ends or restarts; a partial result that
  // captured an older epoch is dropped.
  const partialEpochRef = useRef(0);
  // True while the user is holding the mic button but the hold threshold has
  // not fired yet: used for a subtle visual "press to lock" cue.
  const [holding, setHolding] = useState(false);
  // Armed on press; fires start() after VOICE_HOLD_THRESHOLD_MS. Cleared on
  // release (quick click) or if the pointer leaves the button.
  const holdTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // True when the user has pressed the button again since the current
  // recording started. release() only stops when this is set, so the immediate
  // release that ends a hold-press (which starts continuous mode) does not
  // stop the recording — only a deliberate subsequent click does.
  const pressSinceStartRef = useRef(false);
  // Set once canPreview confirms a local engine for the current recording.
  const partialAllowedRef = useRef(false);
  const canPreviewRef = useRef(options.canPreview);
  canPreviewRef.current = options.canPreview;
  const prewarmRef = useRef(options.prewarm);
  prewarmRef.current = options.prewarm;
  const cancelPrewarmRef = useRef(options.cancelPrewarm);
  cancelPrewarmRef.current = options.cancelPrewarm;
  // Set on unmount: an in-flight recording is discarded, never transcribed.
  const discardRef = useRef(false);
  // Set by stop() while getUserMedia is still waiting on the permission prompt.
  const cancelStartRef = useRef(false);
  // Latest callbacks, so a transcript that arrives later uses the current composer state.
  const callbacksRef = useRef({ transcribe, onTranscript });
  useEffect(() => {
    callbacksRef.current = { transcribe, onTranscript };
  }, [transcribe, onTranscript]);

  const setPhase = useCallback((next: VoiceState) => {
    stateRef.current = next;
    if (mountedRef.current) setState(next);
  }, []);

  const report = useCallback((message: string | null) => {
    if (mountedRef.current) setError(message);
  }, []);

  /** The composer's dismiss affordance. The hook owns the state, so the
   *  component never needs a local copy that could outlive a newer failure. */
  const dismissError = useCallback(() => report(null), [report]);

  const setPartial = useCallback((text: string) => {
    if (mountedRef.current) setPartialTextState(text);
  }, []);

  /** Ends the live preview: stops the timer, invalidates in-flight partials, clears the text. */
  const stopPartials = useCallback(() => {
    if (partialTimerRef.current !== null) {
      clearInterval(partialTimerRef.current);
      partialTimerRef.current = null;
    }
    partialEpochRef.current += 1;
    partialAllowedRef.current = false;
    setPartial("");
  }, [setPartial]);

  /** Requests a transcript of everything recorded so far. Errors are silent: the final transcription still runs on stop. */
  const sendPartial = useCallback(
    async (type: string) => {
      if (!partialAllowedRef.current || partialInFlightRef.current || stateRef.current !== "recording") return;
      const chunks = chunksRef.current;
      if (chunks.length === 0) return;
      const epoch = partialEpochRef.current;
      partialInFlightRef.current = true;
      try {
        // new Blob copies the chunk list now, so later chunks do not leak into this request.
        const blob = new Blob(chunks, { type });
        const result = await callbacksRef.current.transcribe(blob, `recording${audioExtensionFor(type)}`, true);
        if (epoch !== partialEpochRef.current) return;
        setPartial((result?.text ?? "").trim());
      } catch {
        // Ignored on purpose: partials are a preview, not a result.
      } finally {
        partialInFlightRef.current = false;
      }
    },
    [setPartial],
  );

  const releaseStream = useCallback(() => {
    streamRef.current?.getTracks().forEach((track) => track.stop());
    streamRef.current = null;
  }, []);

  const transcribeBlob = useCallback(
    async (blob: Blob, filename: string) => {
      try {
        const result = await callbacksRef.current.transcribe(blob, filename);
        if (discardRef.current) return;
        const text = (result?.text ?? "").trim();
        if (!text) {
          report(VOICE_NOTHING_HEARD_MESSAGE);
          return;
        }
        callbacksRef.current.onTranscript(text);
      } catch (err) {
        if (!discardRef.current) report(err instanceof Error ? err.message : String(err));
      } finally {
        if (!discardRef.current) setPhase("idle");
      }
    },
    [report, setPhase],
  );

  const start = useCallback(async () => {
    if (stateRef.current !== "idle") return;
    if (!isVoiceRecordingSupported()) {
      report(VOICE_UNAVAILABLE_MESSAGE);
      return;
    }
    report(null);
    stopPartials();
    cancelStartRef.current = false;
    discardRef.current = false;
    setPhase("recording");

    // Load the model in the background so the final transcription does not
    // wait for it. Fire-and-forget: a rejection is ignored.
    void prewarmRef.current?.().catch(() => undefined);

    let stream: MediaStream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch (err) {
      setPhase("idle");
      report(describeMicError(err));
      return;
    }
    // Unmounted, or stop() was clicked while the permission prompt was open.
    if (discardRef.current || cancelStartRef.current) {
      stream.getTracks().forEach((track) => track.stop());
      return;
    }
    streamRef.current = stream;
    setHolding(false);
    pressSinceStartRef.current = false; // fresh recording: no stop-intent press yet

    const mimeType = pickRecordingMimeType();
    let recorder: MediaRecorder;
    let failed = false;
    try {
      recorder = mimeType ? new MediaRecorder(stream, { mimeType }) : new MediaRecorder(stream);
    } catch (err) {
      releaseStream();
      setPhase("idle");
      report(describeMicError(err));
      return;
    }

    chunksRef.current = [];
    recorder.ondataavailable = (event: BlobEvent) => {
      if (event.data && event.data.size > 0) chunksRef.current.push(event.data);
    };
    recorder.onerror = () => {
      failed = true;
      if (recorder.state !== "inactive") recorder.stop();
    };
    recorder.onstop = () => {
      stopPartials();
      releaseStream();
      recorderRef.current = null;
      const chunks = chunksRef.current;
      chunksRef.current = [];
      if (discardRef.current) return;
      if (failed) {
        setPhase("idle");
        report("Recording failed. Try again.");
        return;
      }
      if (chunks.length === 0) {
        setPhase("idle");
        report("No audio was captured. Try again.");
        return;
      }
      const type = recorder.mimeType || mimeType || "audio/webm";
      const blob = new Blob(chunks, { type });
      setPhase("transcribing");
      void transcribeBlob(blob, `recording${audioExtensionFor(type)}`);
    };

    recorderRef.current = recorder;
    try {
      recorder.start(VOICE_TIMESLICE_MS);
    } catch (err) {
      recorderRef.current = null;
      releaseStream();
      setPhase("idle");
      report(describeMicError(err));
      return;
    }
    const partialType = recorder.mimeType || mimeType || "audio/webm";
    const epochAtStart = partialEpochRef.current;
    const check = canPreviewRef.current;
    if (check) {
      void check()
        .then((ok) => {
          if (ok && partialEpochRef.current === epochAtStart && stateRef.current === "recording") {
            partialAllowedRef.current = true;
          }
        })
        .catch(() => {
          // No preview when the engine cannot be confirmed; the final still runs.
        });
    }
    partialTimerRef.current = setInterval(() => {
      void sendPartial(partialType);
    }, VOICE_PARTIAL_INTERVAL_MS);
  }, [releaseStream, report, setPhase, sendPartial, stopPartials, transcribeBlob]);

  const stop = useCallback(() => {
    if (stateRef.current !== "recording") return;
    pressSinceStartRef.current = false; // stopped: no press pending for the next release
    stopPartials();
    const recorder = recorderRef.current;
    if (!recorder) {
      // Still waiting on the permission prompt: cancel the start.
      cancelStartRef.current = true;
      setPhase("idle");
      return;
    }
    setPhase("transcribing");
    if (recorder.state !== "inactive") recorder.stop();
  }, [setPhase, stopPartials]);

  /** Stops the recording and discards the clip: no transcription, back to idle. */
  const cancel = useCallback(() => {
    if (stateRef.current === "idle") return;
    stopPartials();
    discardRef.current = true; // onstop skips transcription
    const recorder = recorderRef.current;
    if (recorder) {
      if (recorder.state !== "inactive") recorder.stop();
    } else {
      // Still waiting on the permission prompt: cancel the start.
      cancelStartRef.current = true;
    }
    // The prewarm was only for this recording: stop a load still in progress
    // rather than leaving ~2 GB resident for a clip nobody will transcribe.
    void cancelPrewarmRef.current?.().catch(() => undefined);
    pressSinceStartRef.current = false;
    setPartial("");
    setPhase("idle");
  }, [setPhase, stopPartials]);

  /** Arms the hold timer: call on press (pointerdown/keydown) when idle. */
  const press = useCallback(() => {
    // A press while already recording is the user's stop gesture: remember it
    // so the following release stops & sends (rather than being read as the
    // release that ends a hold-press).
    if (stateRef.current === "recording") {
      pressSinceStartRef.current = true;
      return;
    }
    if (stateRef.current !== "idle") return;
    // CLEAR any previously armed timer before arming a new one.
    //
    // A browser repeats keydown while a key is held (keydown → keydown with
    // repeat=true → …), and the keydown handler calls press() on EVERY
    // keydown. Each call armed a NEW timer and overwrote the ref, so only the
    // LAST timer could be cleared by release() — the earlier ones were
    // orphaned and fired later, starting a SECOND recording with no user
    // action. The orphan also reset pressSinceStartRef, wiping a legitimate
    // stop-intent so the recording was never transcribed.
    if (holdTimerRef.current) {
      clearTimeout(holdTimerRef.current);
      holdTimerRef.current = null;
    }
    holdTimerRef.current = setTimeout(() => {
      holdTimerRef.current = null;
      // NOTE: the stop-intent is a USER signal. It is cleared by stop()/cancel()
      // and by the start of a fresh recording, never by this timer — clearing
      // it here would cancel a deliberate stop-press that arrived while the
      // hold timer was running.
      setHolding(false);
      void start();
    }, VOICE_HOLD_THRESHOLD_MS);
    setHolding(true);
  }, [start]);

  /** Call on release (pointerup/keyup). A quick release starts a normal
   *  recording; a held press already started continuous mode, so release does
   *  nothing. A release while recording (no timer armed) stops & sends. */
  const release = useCallback(() => {
    const timer = holdTimerRef.current;
    if (timer) {
      clearTimeout(timer);
      holdTimerRef.current = null;
      setHolding(false);
      if (stateRef.current === "idle") {
        void start();
      }
      return;
    }
    setHolding(false);
    if (stateRef.current === "recording" && pressSinceStartRef.current) {
      void stop();
    }
  }, [start, stop]);

  /** Clears an armed hold timer without starting anything (pointer left the
   *  button while holding). */
  const cancelPress = useCallback(() => {
    if (holdTimerRef.current) {
      clearTimeout(holdTimerRef.current);
      holdTimerRef.current = null;
    }
    setHolding(false);
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    discardRef.current = false;
    return () => {
      mountedRef.current = false;
      discardRef.current = true;
      cancelStartRef.current = true;
      // CLEAR the hold timer on unmount. Without this, a component that
      // unmounted while the hold timer was armed (the user closed the tab,
      // switched sessions, or the composer was unmounted mid-hold) still had
      // the timer pending: it fired after the unmount and called start(),
      // which asked for the microphone and started a recording with no UI and
      // no way to stop it. The mic indicator stayed lit because nothing
      // released the stream.
      if (holdTimerRef.current) {
        clearTimeout(holdTimerRef.current);
        holdTimerRef.current = null;
      }
      stopPartials();
      const recorder = recorderRef.current;
      recorderRef.current = null;
      if (recorder && recorder.state !== "inactive") {
        try {
          recorder.stop();
        } catch {
          // already stopping
        }
      }
      releaseStream();
      void cancelPrewarmRef.current?.().catch(() => undefined);
    };
  }, [releaseStream, stopPartials]);

  return { supported, state, partialText, error, holding, start, stop, cancel, press, release, cancelPress, dismissError };
}
