import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { api } from "../../api/client";
import type { TTSConfig, TTSEngine, TTSPlayback, TTSStatus } from "../../api/types";
import { chunkSpeechText, sanitizeSpeechText } from "./speechUtils";

interface SpeechContextValue {
  engines: TTSEngine[];
  status: TTSStatus | null;
  config: TTSConfig;
  isSpeaking: boolean;
  paused: boolean;
  error: string | null;
  currentText: string;
  speak: (text: string) => Promise<void>;
  stop: () => void;
  pause: () => void;
  resume: () => void;
  skip: (seconds: number) => void;
  selectEngine: (engine: TTSConfig["engine"]) => Promise<void>;
  setMode: (mode: TTSConfig["mode"]) => Promise<void>;
  retry: () => Promise<void>;
  position: number;
  duration: number;
  seek: (position: number) => void;
  toolbarVisible: boolean;
  setToolbarVisible: (visible: boolean) => void;
  toggleToolbar: () => void;
}

const defaultConfig: TTSConfig = { engine: "browser-native", voice: "", mode: "manual" };
const SpeechContext = createContext<SpeechContextValue | null>(null);

function browserSpeechAvailable() {
  return typeof window !== "undefined" && "speechSynthesis" in window && "SpeechSynthesisUtterance" in window;
}

export function SpeechProvider({ children }: { children: ReactNode }) {
  const [engines, setEngines] = useState<TTSEngine[]>([]);
  const [status, setStatus] = useState<TTSStatus | null>(null);
  const [config, setConfig] = useState<TTSConfig>(defaultConfig);
  const [isSpeaking, setIsSpeaking] = useState(false);
  const [paused, setPaused] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [currentText, setCurrentText] = useState("");
  const [position, setPosition] = useState(0);
  const [duration, setDuration] = useState(0);
  const [toolbarVisible, setToolbarVisible] = useState(true);
  const toggleToolbar = useCallback(() => setToolbarVisible((v) => !v), [setToolbarVisible]);
  const generation = useRef(0);
  const localRequestGeneration = useRef(0);
  const selectionRequestGeneration = useRef(0);
  const localMutationTail = useRef(Promise.resolve());
  const chunks = useRef<string[]>([]);
  const chunkIndex = useRef(0);
  const lastText = useRef("");

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
    chunks.current = [];
    chunkIndex.current = 0;
    setPosition(0);
    setDuration(0);
  }, []);

  const stop = useCallback(() => {
    generation.current++;
    localRequestGeneration.current++;
    chunks.current = [];
    chunkIndex.current = 0;
    if (browserSpeechAvailable()) window.speechSynthesis.cancel();
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
  }, [config.engine, enqueueLocalMutation]);

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

  const speak = useCallback(async (text: string) => {
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
    try {
      const playback = await enqueueLocalMutation(() => api.ttsSpeak(normalized));
      if (requestGeneration === localRequestGeneration.current) {
        setStatus((previous) => previous ? { ...previous, playback } : previous);
      }
    } catch (err) {
      // Deliberately do not switch to Browser Native on local-engine failure.
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [config.engine, enqueueLocalMutation, speakBrowser, stop]);

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
    if (!isSpeaking || config.engine !== "browser-native" || !lastText.current) return;
    const parts = chunkSpeechText(lastText.current, 240);
    const delta = Math.max(1, Math.round(Math.abs(seconds) / 10)) * (seconds < 0 ? -1 : 1);
    beginBrowserPlayback(parts, Math.max(0, Math.min(parts.length, chunkIndex.current + delta)));
  }, [beginBrowserPlayback, config.engine, isSpeaking]);

  const seek = useCallback((nextPosition: number) => {
    if (config.engine !== "browser-native" || !lastText.current) return;
    const parts = chunkSpeechText(lastText.current, 240);
    const target = Math.max(0, Math.min(parts.length, Math.round(nextPosition)));
    beginBrowserPlayback(parts, target);
  }, [beginBrowserPlayback, config.engine]);

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
    engines, status, config, isSpeaking, paused, error, currentText, speak, stop,
    pause: () => { if (browserSpeechAvailable()) { window.speechSynthesis.pause(); setPaused(true); } },
    resume: () => { if (browserSpeechAvailable()) { window.speechSynthesis.resume(); setPaused(false); } },
    skip, position, duration, seek,
    selectEngine, setMode, retry,
    toolbarVisible, setToolbarVisible, toggleToolbar,
  }), [config, currentText, duration, engines, error, isSpeaking, paused, position, retry, seek, selectEngine, setMode, skip, speak, status, stop, toolbarVisible, setToolbarVisible, toggleToolbar]);

  return <SpeechContext.Provider value={value}>{children}</SpeechContext.Provider>;
}

export function useSpeech() {
  const context = useContext(SpeechContext);
  if (!context) throw new Error("useSpeech must be used within SpeechProvider");
  return context;
}

export function playbackLabel(playback: TTSPlayback | undefined) {
  if (!playback || playback.status === "stopped") return "Idle";
  return playback.status === "playing" ? "Playing" : playback.status;
}

export function requestSpeech(text: string) {
  window.dispatchEvent(new CustomEvent("ocode:speak", { detail: { text } }));
}
