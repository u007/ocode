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
  transcribe: (audio: Blob, filename: string) => Promise<STTTranscribeResult>;
  /** Receives a non-empty transcript. Never called for an empty one. */
  onTranscript: (text: string) => void;
}

export interface VoiceRecorder {
  /** False when getUserMedia or MediaRecorder is missing. */
  supported: boolean;
  state: VoiceState;
  error: string | null;
  start: () => Promise<void>;
  /** Stops the recording and transcribes it. No-op unless recording. */
  stop: () => void;
  /** Starts when idle, stops when recording, does nothing while transcribing. */
  toggle: () => void;
}

export function useVoiceRecorder({ transcribe, onTranscript }: UseVoiceRecorderOptions): VoiceRecorder {
  const supported = isVoiceRecordingSupported();
  const [state, setState] = useState<VoiceState>("idle");
  const [error, setError] = useState<string | null>(null);

  // stateRef is the synchronous source of truth for guards (a double click
  // must not start two recorders); `state` only drives rendering.
  const stateRef = useRef<VoiceState>("idle");
  const recorderRef = useRef<MediaRecorder | null>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const mountedRef = useRef(false);
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
    cancelStartRef.current = false;
    setPhase("recording");

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
      recorder.start();
    } catch (err) {
      recorderRef.current = null;
      releaseStream();
      setPhase("idle");
      report(describeMicError(err));
    }
  }, [releaseStream, report, setPhase, transcribeBlob]);

  const stop = useCallback(() => {
    if (stateRef.current !== "recording") return;
    const recorder = recorderRef.current;
    if (!recorder) {
      // Still waiting on the permission prompt: cancel the start.
      cancelStartRef.current = true;
      setPhase("idle");
      return;
    }
    setPhase("transcribing");
    if (recorder.state !== "inactive") recorder.stop();
  }, [setPhase]);

  const toggle = useCallback(() => {
    if (stateRef.current === "idle") void start();
    else if (stateRef.current === "recording") stop();
  }, [start, stop]);

  useEffect(() => {
    mountedRef.current = true;
    discardRef.current = false;
    return () => {
      mountedRef.current = false;
      discardRef.current = true;
      cancelStartRef.current = true;
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
    };
  }, [releaseStream]);

  return { supported, state, error, start, stop, toggle };
}
