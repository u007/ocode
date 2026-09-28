import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { api, type SpeechSummaryConfig } from "../../api/client";
import type { TTSConfig, TTSEngine, TTSPlayback, TTSStatus } from "../../api/types";
import { useChatSelector } from "../../stores/chatStore";
import { chunkSpeechText, sanitizeSpeechText } from "./speechUtils";
import {
  loadSpeechSpeakMode,
  loadSpeechToolbarVisible,
  saveSpeechSpeakMode,
  saveSpeechToolbarVisible,
  type SpeechSpeakMode,
} from "./speechToolbarPersistence";
import { DEFAULT_SPEECH_SUMMARY_CONFIG } from "../../lib/speechSummaryConfig";

interface SpeechContextValue {
  engines: TTSEngine[];
  status: TTSStatus | null;
  config: TTSConfig;
  isSpeaking: boolean;
  paused: boolean;
  error: string | null;
  currentText: string;
  speak: (text: string) => Promise<void>;
  /** Re-speak text that was ALREADY prepared for speech (the toolbar's Replay).
   *  Bypasses the summary model: re-summarising a summary wastes an LLM call
   *  and reads a summary of a summary. */
  replay: (text: string) => Promise<void>;
  stop: () => void;
  pause: () => void;
  resume: () => void;
  skip: (seconds: number) => void;
  selectEngine: (engine: TTSConfig["engine"]) => Promise<void>;
  setMode: (mode: TTSConfig["mode"]) => Promise<void>;
  retry: () => Promise<void>;
  refresh: () => Promise<void>;
  position: number;
  duration: number;
  seek: (position: number) => void;
  toolbarVisible: boolean;
  setToolbarVisible: (visible: boolean) => void;
  toggleToolbar: () => void;
  /** Whether assistant text is shortened by the summary model before it is
   *  spoken. Server-backed: the same flag the sidebar and settings toggle. */
  summaryEnabled: boolean;
  summaryModel: string;
  setSummaryEnabled: (enabled: boolean) => Promise<SpeechSummaryConfig>;
  /** Set the speech-summary model. Kept beside setSummaryEnabled so the
   *  Settings form, the sidebar row and the model picker all mutate the ONE
   *  owner of this config instead of writing the endpoint behind its back. */
  setSummaryModel: (model: string) => Promise<SpeechSummaryConfig>;
  /** Apply a partial update and keep this provider's copy in sync. The Settings
   *  form edits both keys at once, so it needs a single-call merge rather than
   *  two sequential setter calls. */
  updateSummaryConfig: (patch: Partial<SpeechSummaryConfig>) => Promise<SpeechSummaryConfig>;
  /** SSH/WSL host this provider is bound to (undefined = local). Exposed so the
   *  Settings form reads and writes the SAME host's block the speak path uses,
   *  rather than the local one. */
  host?: string;
  /** Per-user "speak full text instead" override. Client-side so reading one
   *  message verbatim does not disable the feature everywhere. */
  speakMode: SpeechSpeakMode;
  setSpeakMode: (mode: SpeechSpeakMode) => void;
}

const defaultConfig: TTSConfig = { engine: "browser-native", voice: "", mode: "manual" };
const SpeechContext = createContext<SpeechContextValue | null>(null);

function browserSpeechAvailable() {
  return typeof window !== "undefined" && "speechSynthesis" in window && "SpeechSynthesisUtterance" in window;
}

export function SpeechProvider({
  children,
  sessionId,
  host,
}: {
  children: ReactNode;
  /** Active chat session. The summary endpoint is session-scoped so it reuses
   *  that session's agent, credentials and profile; without it the funnel has
   *  nothing to summarise against and speaks the full text. */
  sessionId?: string;
  /** SSH/WSL host of the session's project: both the summary config and the
   *  summary request must go to the host that runs the session. */
  host?: string;
}) {
	const chatModel = useChatSelector((state) => state.model);
  const [engines, setEngines] = useState<TTSEngine[]>([]);
  const [status, setStatus] = useState<TTSStatus | null>(null);
  const [config, setConfig] = useState<TTSConfig>(defaultConfig);
  const [isSpeaking, setIsSpeaking] = useState(false);
  const [paused, setPaused] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [currentText, setCurrentText] = useState("");
  const [position, setPosition] = useState(0);
  const [duration, setDuration] = useState(0);
  const [toolbarVisible, setToolbarVisibleState] = useState<boolean>(() => loadSpeechToolbarVisible());
  const setToolbarVisible = useCallback((visible: boolean) => {
    setToolbarVisibleState(visible);
    saveSpeechToolbarVisible(visible);
  }, []);
  const toggleToolbar = useCallback(() => {
    const next = !toolbarVisible;
    setToolbarVisibleState(next);
    saveSpeechToolbarVisible(next);
  }, [toolbarVisible]);
  const [speakMode, setSpeakModeState] = useState<SpeechSpeakMode>(() => loadSpeechSpeakMode());
  const setSpeakMode = useCallback((mode: SpeechSpeakMode) => {
    setSpeakModeState(mode);
    saveSpeechSpeakMode(mode);
  }, []);
  const [summaryConfig, setSummaryConfig] = useState(DEFAULT_SPEECH_SUMMARY_CONFIG);
  const summaryEnabled = summaryConfig.enabled;
  const summaryModel = summaryConfig.model;
  const generation = useRef(0);
  const localRequestGeneration = useRef(0);
  // Invalidates a speak() that is still awaiting its summary. Bumped by
  // stop() and by every new speak/replay, so a Stop during the (up to 60s)
  // summary wait cancels the pending playback, and two overlapping speaks
  // cannot finish out of order (the older one's guard fails).
  const speakRequestGeneration = useRef(0);
  const selectionRequestGeneration = useRef(0);
  const localMutationTail = useRef(Promise.resolve());
  const chunks = useRef<string[]>([]);
  const chunkIndex = useRef(0);
  const lastText = useRef("");
  // Local-engine playback: one <audio> element fed by a blob fetched from
  // /api/tts/audio once the server reports the rendering ready.
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const audioUrlRef = useRef<string | null>(null);
  const isLocal = config.engine !== "browser-native";

  const releaseAudio = useCallback(() => {
    const audio = audioRef.current;
    if (audio) {
      audio.pause();
      audio.onended = null;
      audio.onerror = null;
      audio.ontimeupdate = null;
      audio.onloadedmetadata = null;
      audio.removeAttribute("src");
      audioRef.current = null;
    }
    if (audioUrlRef.current) {
      URL.revokeObjectURL(audioUrlRef.current);
      audioUrlRef.current = null;
    }
  }, []);

  const refresh = useCallback(async () => {
    try {
      const [catalog, selected, current] = await Promise.all([api.getTTSEngines(), api.getTTSConfig(), api.getTTSStatus()]);
      setEngines(catalog.engines);
      setConfig(selected);
      setStatus(current);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  // Server-side playback mutations must be observed in submission order. A
  // newer speak request otherwise can reach the server before an older stop
  // request and get stopped by it. Failed mutations do not poison the queue;
  // the next operation still gets a chance to run.
  const enqueueLocalMutation = useCallback(<T,>(operation: () => Promise<T>) => {
    const next = localMutationTail.current.catch(() => undefined).then(operation);
    localMutationTail.current = next.then(() => undefined, () => undefined);
    return next;
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => () => {
    generation.current++;
    window.speechSynthesis?.cancel();
    releaseAudio();
    chunks.current = [];
    chunkIndex.current = 0;
    setPosition(0);
    setDuration(0);
  }, []);

  const stop = useCallback(() => {
    generation.current++;
    localRequestGeneration.current++;
    // Cancel a speak that is still awaiting its summary: without this, a Speak
    // click followed by Stop still starts playback once the summary resolves.
    speakRequestGeneration.current++;
    chunks.current = [];
    chunkIndex.current = 0;
    if (browserSpeechAvailable()) window.speechSynthesis.cancel();
    releaseAudio();
    if (config.engine !== "browser-native") {
      const requestGeneration = localRequestGeneration.current;
      void enqueueLocalMutation(() => api.ttsStop()).then((playback) => {
        if (requestGeneration === localRequestGeneration.current) {
          setStatus((previous) => previous ? { ...previous, playback } : previous);
        }
      }).catch(() => undefined);
    }
    setIsSpeaking(false);
    setPaused(false);
  }, [config.engine, enqueueLocalMutation, releaseAudio]);

  const beginBrowserPlayback = useCallback((parts: string[], startAt: number) => {
    const currentGeneration = ++generation.current;
    window.speechSynthesis.cancel();
    chunks.current = parts;
    chunkIndex.current = Math.max(0, Math.min(startAt, parts.length));
    setDuration(parts.length);
    setPosition(chunkIndex.current);
    const speakNext = () => {
      if (currentGeneration !== generation.current || chunkIndex.current >= chunks.current.length) {
        if (currentGeneration === generation.current) {
          setPosition(chunks.current.length);
          setIsSpeaking(false);
        }
        return;
      }
      const index = chunkIndex.current++;
      setPosition(index);
      const utterance = new SpeechSynthesisUtterance(chunks.current[index]);
      utterance.onend = speakNext;
      utterance.onerror = (event) => {
        if (currentGeneration !== generation.current || event.error === "canceled") return;
        setError(`Browser Native speech failed: ${event.error || "unknown error"}`);
        setIsSpeaking(false);
      };
      window.speechSynthesis.speak(utterance);
    };
    setError(null);
    setPaused(false);
    setIsSpeaking(true);
    speakNext();
  }, []);

  const speakBrowser = useCallback((text: string) => {
    if (!browserSpeechAvailable()) throw new Error("Browser Native speech is unavailable in this browser");
    beginBrowserPlayback(chunkSpeechText(text, 240), 0);
  }, [beginBrowserPlayback]);

  // Load the server-side speech-summary block. Host-threaded, and re-read
  // whenever the host changes so switching from a local project to a remote one
  // cannot leave the local block gating the remote session's speech.
  useEffect(() => {
    let cancelled = false;
    void api
      .getSpeechSummaryConfig(host)
      .then((next) => {
        if (!cancelled) setSummaryConfig(next);
      })
      .catch((err) => {
        // A failed read must not disable summarising: the server default is ON,
        // so keep the optimistic default and let the next speak try anyway.
        console.warn("speech summary config load failed", err);
      });
    return () => {
      cancelled = true;
    };
  }, [host]);

  const updateSummaryConfig = useCallback(
    async (patch: Partial<SpeechSummaryConfig>): Promise<SpeechSummaryConfig> => {
      // Optimistic so a toggle/pick feels instant, then reconciled from the
      // server's merged block so a rejected write cannot leave the UI lying.
      const previous = summaryConfig;
      setSummaryConfig({ ...summaryConfig, ...patch });
      try {
        const saved = await api.setSpeechSummaryConfig(patch, host);
        setSummaryConfig(saved);
        return saved;
      } catch (err) {
        setSummaryConfig(previous);
        console.warn("speech summary config update failed", err);
        throw err;
      }
    },
    [summaryConfig, host],
  );

  const setSummaryEnabled = useCallback(
    (enabled: boolean) => updateSummaryConfig({ enabled }),
    [updateSummaryConfig],
  );

  const setSummaryModel = useCallback(
    (model: string) => updateSummaryConfig({ model }),
    [updateSummaryConfig],
  );

  /**
   * Decide what to actually read: the summarised prose, or the original text.
   *
   * Every failure path returns the ORIGINAL text rather than an empty string,
   * because a side task failing must never silence speech -- reading a code
   * block aloud is strictly better than reading nothing.
   */
  const resolveSpeechText = useCallback(
    async (text: string): Promise<string> => {
      if (!summaryEnabled || speakMode === "full" || !sessionId) return text;
      try {
        const { summary } = await api.summarizeSpeech(sessionId, text, host);
        const trimmed = typeof summary === "string" ? summary.trim() : "";
        return trimmed || text;
      } catch (err) {
        // Non-fatal by design: the summary is an optimisation, not a
        // prerequisite. Logged so the reason is visible in the console rather
        // than silently reading the full message.
        console.warn("speech summary failed; speaking full text", err);
        return text;
      }
    },
    [summaryEnabled, speakMode, sessionId, host],
  );

  const speakNow = useCallback(async (text: string) => {
    const normalized = sanitizeSpeechText(text);
    if (!normalized) {
      setError("Nothing to speak");
      return;
    }
    setError(null);
    setCurrentText(normalized);
    lastText.current = normalized;
    if (config.engine === "browser-native") {
      try {
        speakBrowser(normalized);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
        setIsSpeaking(false);
        setPaused(false);
      }
      return;
    }
    stop();
    const requestGeneration = ++localRequestGeneration.current;
    const current = () => requestGeneration === localRequestGeneration.current;
    try {
		  let playback = await enqueueLocalMutation(() => api.ttsSpeak(normalized, chatModel ?? undefined));
      if (!current()) return;
      setStatus((previous) => previous ? { ...previous, playback } : previous);
      setIsSpeaking(true);
      setPaused(false);
      setPosition(0);
      setDuration(0);
      // Synthesis runs server-side; poll until this generation is ready.
      while (current() && playback.status === "synthesizing") {
        await new Promise((resolve) => setTimeout(resolve, 400));
        if (!current()) return;
        const next = await api.getTTSStatus();
        if (next.playback.generation !== playback.generation) {
          throw new Error("speech request was replaced");
        }
        playback = next.playback;
        setStatus(next);
      }
      if (!current()) return;
      if (playback.status !== "ready" || !playback.audio_id) {
        throw new Error(playback.error || `speech synthesis ${playback.status}`);
      }
      const blob = await api.ttsAudioBlob(playback.audio_id);
      if (!current()) return;
      releaseAudio();
      const url = URL.createObjectURL(blob);
      audioUrlRef.current = url;
      const audio = new Audio(url);
      audioRef.current = audio;
      audio.onloadedmetadata = () => { if (current()) setDuration(Math.round(audio.duration)); };
      audio.ontimeupdate = () => { if (current()) setPosition(Math.round(audio.currentTime)); };
      audio.onended = () => {
        if (!current()) return;
        setIsSpeaking(false);
        setPaused(false);
        setPosition(Math.round(audio.duration));
      };
      audio.onerror = () => {
        if (!current()) return;
        setError("Audio playback failed");
        setIsSpeaking(false);
      };
      await audio.play();
    } catch (err) {
      if (!current()) return;
      // Deliberately do not switch to Browser Native on local-engine failure.
      setError(err instanceof Error ? err.message : String(err));
      setIsSpeaking(false);
      setPaused(false);
    }
  }, [config.engine, enqueueLocalMutation, releaseAudio, speakBrowser, stop]);

  // The single funnel every speak path goes through: the per-message speak
  // button, the toolbar replay, and at-bottom auto-speak. Summarising lives
  // HERE, not in the callers, so a new caller cannot accidentally read a code
  // block aloud.
  //
  // The request generation is captured BEFORE the summary await and re-checked
  // after: the summary call can take up to 60s, and without the check a Stop
  // pressed during it would not stop the pending playback, and two overlapping
  // speaks could finish out of order (the older message playing last).
  const speak = useCallback(
    async (text: string) => {
      const requestGeneration = ++speakRequestGeneration.current;
      const spoken = await resolveSpeechText(text);
      if (requestGeneration !== speakRequestGeneration.current) return;
      if (!spoken) return;
      await speakNow(spoken);
    },
    [resolveSpeechText, speakNow],
  );

  // Replay the already-prepared text verbatim, WITHOUT another summary pass.
  // The toolbar's Replay hands back `currentText`, which is the summarised
  // prose (or the full text when that mode is on); summarising it again would
  // cost an extra LLM call and read a summary of a summary.
  const replay = useCallback(
    async (text: string) => {
      // Supersede any speak still awaiting its summary, so a replay cannot be
      // interrupted by an older pending request landing after it.
      speakRequestGeneration.current++;
      await speakNow(text);
    },
    [speakNow],
  );

  useEffect(() => {
    const onRequest = (event: Event) => {
      const text = (event as CustomEvent<{ text?: string }>).detail?.text;
      if (text) void speak(text);
    };
    window.addEventListener("ocode:speak", onRequest as EventListener);
    return () => window.removeEventListener("ocode:speak", onRequest as EventListener);
  }, [speak]);

  useEffect(() => {
    const onAssistantComplete = (event: Event) => {
      const detail = (event as CustomEvent<{ text?: string; atBottom?: boolean }>).detail;
      if (config.mode === "at-bottom" && detail?.atBottom && detail.text) void speak(detail.text);
    };
    window.addEventListener("ocode:assistant-complete", onAssistantComplete as EventListener);
    return () => window.removeEventListener("ocode:assistant-complete", onAssistantComplete as EventListener);
  }, [config.mode, speak]);

  const selectEngine = useCallback(async (engine: TTSConfig["engine"]) => {
    const requestGeneration = ++selectionRequestGeneration.current;
    stop();
    localRequestGeneration.current++;
    try {
      const next = { ...config, engine };
      const nextStatus = await enqueueLocalMutation(() => api.setTTSConfig(next));
      if (requestGeneration !== selectionRequestGeneration.current) return;
      setConfig(next);
      setStatus(nextStatus);
      setError(nextStatus.error ?? null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [config, enqueueLocalMutation, stop]);

  const setMode = useCallback(async (mode: TTSConfig["mode"]) => {
    const requestGeneration = ++selectionRequestGeneration.current;
    try {
      const next = { ...config, mode };
      const nextStatus = await enqueueLocalMutation(() => api.setTTSConfig(next));
      if (requestGeneration !== selectionRequestGeneration.current) return;
      setConfig(next);
      setStatus(nextStatus);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [config, enqueueLocalMutation]);

  const skip = useCallback((seconds: number) => {
    const audio = audioRef.current;
    if (isLocal) {
      if (audio && isSpeaking) audio.currentTime = Math.max(0, Math.min(audio.duration || 0, audio.currentTime + seconds));
      return;
    }
    if (!isSpeaking || !lastText.current) return;
    const parts = chunkSpeechText(lastText.current, 240);
    const delta = Math.max(1, Math.round(Math.abs(seconds) / 10)) * (seconds < 0 ? -1 : 1);
    beginBrowserPlayback(parts, Math.max(0, Math.min(parts.length, chunkIndex.current + delta)));
  }, [beginBrowserPlayback, isLocal, isSpeaking]);

  const seek = useCallback((nextPosition: number) => {
    const audio = audioRef.current;
    if (isLocal) {
      if (audio) audio.currentTime = Math.max(0, Math.min(audio.duration || 0, nextPosition));
      return;
    }
    if (!lastText.current) return;
    const parts = chunkSpeechText(lastText.current, 240);
    const target = Math.max(0, Math.min(parts.length, Math.round(nextPosition)));
    beginBrowserPlayback(parts, target);
  }, [beginBrowserPlayback, isLocal]);

  const retry = useCallback(async () => {
    setError(null);
    if (config.engine === "browser-native" && lastText.current) {
      try {
        speakBrowser(lastText.current);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      }
      return;
    }
    try {
      const nextStatus = await enqueueLocalMutation(() => api.setTTSConfig(config));
      setStatus(nextStatus);
      setError(nextStatus.error ?? null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [config, enqueueLocalMutation, speakBrowser]);

  const value = useMemo<SpeechContextValue>(() => ({
    engines, status, config, isSpeaking, paused, error, currentText, speak, replay, stop, host,
    pause: () => {
      if (isLocal) { audioRef.current?.pause(); setPaused(true); return; }
      if (browserSpeechAvailable()) { window.speechSynthesis.pause(); setPaused(true); }
    },
    resume: () => {
      if (isLocal) { void audioRef.current?.play(); setPaused(false); return; }
      if (browserSpeechAvailable()) { window.speechSynthesis.resume(); setPaused(false); }
    },
    skip, position, duration, seek,
    selectEngine, setMode, retry, refresh,
    toolbarVisible, setToolbarVisible, toggleToolbar,
    summaryEnabled, summaryModel, setSummaryEnabled, setSummaryModel, updateSummaryConfig, speakMode, setSpeakMode,
  }), [config, currentText, duration, engines, error, isLocal, isSpeaking, paused, position, refresh, retry, seek, selectEngine, setMode, skip, speak, replay, status, stop, toolbarVisible, setToolbarVisible, toggleToolbar,
    summaryEnabled, summaryModel, setSummaryEnabled, setSummaryModel, updateSummaryConfig, speakMode, setSpeakMode, host]);

  return <SpeechContext.Provider value={value}>{children}</SpeechContext.Provider>;
}

export function useSpeech() {
  const context = useContext(SpeechContext);
  if (!context) throw new Error("useSpeech must be used within SpeechProvider");
  return context;
}

/**
 * Like useSpeech, but returns null instead of throwing when there is no
 * provider. Components that persist speech-summary settings (CoworkSidebar,
 * ModelDialog) are also rendered without a provider in isolated tests and
 * unusual trees; they route the write through the provider — the single owner
 * of `summaryConfig` — when it exists, and fall back to the direct endpoint
 * otherwise. This keeps the provider's copy in sync so a toggle in one screen
 * takes effect in the speak path without a reload.
 */
export function useSpeechOptional() {
  return useContext(SpeechContext);
}

export function playbackLabel(playback: TTSPlayback | undefined) {
  // "ready" is the server's rendered-audio state; once the client finished
  // playing it the toolbar is idle again.
  if (!playback || playback.status === "stopped" || playback.status === "ready") return "Idle";
  if (playback.status === "synthesizing") return "Synthesizing";
  return playback.status === "playing" ? "Playing" : playback.status;
}

export function requestSpeech(text: string) {
  window.dispatchEvent(new CustomEvent("ocode:speak", { detail: { text } }));
}
