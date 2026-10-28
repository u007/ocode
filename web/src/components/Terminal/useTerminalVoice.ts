import { useEffect, useState } from "react";
import { api, STTInstallRequiredError } from "@/api/client";
import { reportActionErrorMessage } from "@/lib/actionErrors";
import { useVoiceRecorder, VOICE_UNAVAILABLE_MESSAGE, type VoiceState } from "../Chat/useVoiceRecorder";

/**
 * Voice input for the terminal panel. Records with the shared recorder and
 * hands the transcript to `onTranscript`, which pastes it into the pty. The
 * recorder, the transcribe endpoint and the install confirm flow are the same
 * ones the chat composer uses (ChatInput), so the server contract and the
 * install prompt stay in one shape. This hook only adds the terminal's tap
 * model (tap to start, tap to stop) and its error surface (the app-wide
 * ActionErrorToast, since the terminal has no composer to show an inline banner).
 */

export const VOICE_INSECURE_CONTEXT_MESSAGE =
  "Voice input needs a secure connection: open ocode over HTTPS or localhost to record microphone audio.";

/** Why the browser cannot record here. A plain-HTTP page on a phone is the common
 *  case: getUserMedia does not exist outside a secure context. */
export function voiceUnavailableMessage(): string {
  return window.isSecureContext === false ? VOICE_INSECURE_CONTEXT_MESSAGE : VOICE_UNAVAILABLE_MESSAGE;
}

export interface InstallAsk {
  message: string;
  resolve: (ok: boolean) => void;
}

export interface TerminalVoice {
  /** False when getUserMedia or MediaRecorder is missing (e.g. insecure context). */
  supported: boolean;
  state: VoiceState;
  /** True while the one-time voice dependency install runs. */
  installing: boolean;
  /** Set while the install confirm is open; the caller renders the dialog. */
  installAsk: InstallAsk | null;
  /** Tap handler: starts a recording when idle, stops and transcribes when recording. */
  toggle: () => void;
}

export function useTerminalVoice(onTranscript: (text: string) => void): TerminalVoice & {
  answerInstall: (ok: boolean) => void;
} {
  const [installAsk, setInstallAsk] = useState<InstallAsk | null>(null);
  const [installing, setInstalling] = useState(false);

  // Not window.confirm: native dialogs silently return false in the desktop webview.
  const askInstall = (message: string) =>
    new Promise<boolean>((resolve) => setInstallAsk({ message, resolve }));
  const answerInstall = (ok: boolean) => {
    installAsk?.resolve(ok);
    setInstallAsk(null);
  };

  const voice = useVoiceRecorder({
    transcribe: async (audio, filename) => {
      try {
        return await api.transcribeSpeech(audio, filename);
      } catch (err) {
        // The local engine needs a one-time pip install: ask, then retry once.
        if (!(err instanceof STTInstallRequiredError)) throw err;
        if (!(await askInstall(err.message))) {
          throw new Error("Voice input needs the dependency install; it was declined.");
        }
        setInstalling(true);
        try {
          return await api.transcribeSpeech(audio, filename, { installApproved: true });
        } catch (installErr) {
          // Keep the server's message (the tail of pip's output) verbatim.
          const detail = installErr instanceof Error ? installErr.message : String(installErr);
          throw new Error(`Voice dependency install failed: ${detail}`);
        } finally {
          setInstalling(false);
        }
      }
    },
    onTranscript,
    // Load the model while the user is talking so the final transcription does not wait for it.
    // `?.` keeps a partial test double (an api object without the method) from
    // throwing: these callbacks run from an unmount effect.
    prewarm: () => Promise.resolve(api.prewarmSpeech?.()),
    // Abandoning a recording must not leave the ~2 GB load resident. The
    // cancel is process-wide (stt.CancelPrewarm), so it only ever aborts a
    // LOAD in progress; a worker already transcribing is untouched.
    cancelPrewarm: () => Promise.resolve(api.cancelPrewarmSpeech?.()),
  });

  const { error, dismissError } = voice;
  // Every recorder failure (mic denied, nothing heard, transcription or install
  // failure) lands here once: logged, shown in the app toast, and cleared from
  // the recorder so the same message can surface again later.
  useEffect(() => {
    if (!error) return;
    console.error("terminal: voice input failed:", error);
    reportActionErrorMessage(error);
    dismissError();
  }, [error, dismissError]);

  const toggle = () => {
    if (!voice.supported) {
      console.warn("terminal: voice input unavailable in this browser");
      reportActionErrorMessage(voiceUnavailableMessage());
      return;
    }
    if (voice.state === "recording") {
      voice.stop();
    } else {
      void voice.start();
    }
  };

  return {
    supported: voice.supported,
    state: voice.state,
    installing,
    installAsk,
    toggle,
    answerInstall,
  };
}
